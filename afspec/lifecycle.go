package afspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// deepCopyJSON returns a deep copy of v by round-tripping through JSON.
// It returns nil if v is nil. Panics only if T is not JSON-serialisable,
// which cannot happen for the generated artifact types.
func deepCopyJSON[T any](v *T) *T {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("deepCopyJSON: marshal failed: %v", err))
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		panic(fmt.Sprintf("deepCopyJSON: unmarshal failed: %v", err))
	}
	return &out
}

// validTransitions maps each spec status to the set of statuses it can transition to.
// Active specs must be sealed before they can be archived or superseded; use
// Supersede() on a sealed spec to add the required deprecation banner.
var validTransitions = map[string][]string{
	"draft":      {"active", "archived"},
	"active":     {"sealed"},
	"sealed":     {"superseded", "archived"},
	"superseded": {"archived"},
}

// ValidTransition checks the transition table and returns true if the
// transition from current to target state is allowed, false otherwise.
// This is a pure function with no side effects.
//
// Allowed transitions:
//   - draft      -> active
//   - draft      -> archived
//   - active     -> sealed
//   - sealed     -> superseded
//   - sealed     -> archived
//   - superseded -> archived
func ValidTransition(current, target string) bool {
	allowed, ok := validTransitions[current]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// Transition updates the spec's status field to the target state,
// persists the updated spec to disk, and returns a new Spec copy with
// the updated status. The receiver is not modified.
//
// When transitioning from draft to active, ComputeIntentHash is called on
// the PRD body. If the body lacks a ## Intent section, an IntentError is
// returned and the spec is not persisted.
//
// Returns a LifecycleError if the transition is not allowed.
// Returns an IntentError if transitioning to active and the PRD body has no
// ## Intent section.
// Returns a SaveError if disk persistence fails.
func (s *Spec) Transition(target, dir string) (*Spec, error) {
	if !ValidTransition(s.Status, target) {
		return nil, &LifecycleError{
			Msg: fmt.Sprintf("invalid transition from %q to %q", s.Status, target),
			Err: &SpecError{Msg: fmt.Sprintf("invalid transition from %q to %q", s.Status, target)},
		}
	}

	// Create a deep copy with the new status.
	// The receiver is not modified — Transition returns a new Spec.
	// Slice fields and artifact pointers are deep-copied so that mutations
	// to the returned spec cannot alias back to the original.
	newSpec := *s
	newSpec.Supersedes = append([]string{}, s.Supersedes...)
	newSpec.Tags = append([]string{}, s.Tags...)
	newSpec.Requirements = deepCopyJSON(s.Requirements)
	newSpec.TestSpec = deepCopyJSON(s.TestSpec)
	newSpec.Tasks = deepCopyJSON(s.Tasks)
	newSpec.Status = target

	// When activating a draft spec, compute and store the intent hash.
	// Reject the transition if the PRD body lacks a ## Intent section.
	// Also capture the immutable snapshot so subsequent Save calls on the
	// returned Spec can detect mutations to spec_id, spec_name, and created_at.
	if s.Status == "draft" && target == "active" {
		hash, err := ComputeIntentHash(s.PRDBody)
		if err != nil {
			return nil, err
		}
		newSpec.IntentHash = &hash
		newSpec.loaded = &immutableSnapshot{
			SpecID:    newSpec.SpecID,
			SpecName:  newSpec.SpecName,
			CreatedAt: newSpec.CreatedAt,
		}
	}

	// Persist via saveToDisk which bypasses the public Save lifecycle guard,
	// since Transition legitimately writes sealed/superseded/archived specs.
	if err := newSpec.saveToDisk(dir); err != nil {
		return nil, err
	}

	return &newSpec, nil
}

// Supersede transitions a sealed spec to the superseded state, prepends
// a deprecation banner referencing the superseding spec ID to the PRD
// body, and persists the updated spec to disk. Returns a new Spec copy.
//
// Returns a LifecycleError if the spec is not in the sealed state.
func (s *Spec) Supersede(supersedingSpecID, dir string) (*Spec, error) {
	if s.Status != "sealed" {
		return nil, &LifecycleError{
			Msg: fmt.Sprintf("cannot supersede spec in %q state; must be sealed", s.Status),
			Err: &SpecError{Msg: fmt.Sprintf("cannot supersede spec in %q state; must be sealed", s.Status)},
		}
	}

	// Create a shallow copy with superseded status and deprecation banner.
	newSpec := *s
	newSpec.Status = "superseded"

	// Prepend deprecation banner to the PRD body.
	banner := fmt.Sprintf("> **DEPRECATED:** This specification has been superseded by spec %s. "+
		"Refer to the replacement specification for current requirements.\n\n", supersedingSpecID)
	newSpec.PRDBody = banner + s.PRDBody

	if err := newSpec.saveToDisk(dir); err != nil {
		return nil, err
	}

	return &newSpec, nil
}

// saveFailure is a *SaveError with a formatted message.
func saveFailure(format string, args ...any) *SaveError {
	msg := fmt.Sprintf(format, args...)
	return &SaveError{Msg: msg, Err: &SpecError{Msg: msg}}
}

// ArchiveDestination is where MoveUnderArchive puts the package at specDir:
// <root>/archive/<base name of specDir>. It writes nothing.
//
// Returns a SaveError when that path already exists (the message is "archive
// conflict: <path> already exists"; a dangling symbolic link counts, since a
// rename would replace it) or when whether it exists cannot be found out.
func ArchiveDestination(specDir, root string) (string, error) {
	dest := filepath.Join(root, "archive", filepath.Base(specDir))
	_, err := os.Lstat(dest)
	switch {
	case err == nil:
		return "", saveFailure("archive conflict: %s already exists", dest)
	case !errors.Is(err, os.ErrNotExist):
		return "", saveFailure("cannot check archive destination: %s", err)
	}
	return dest, nil
}

// MoveUnderArchive moves the package at specDir to ArchiveDestination(specDir,
// root) and returns the new directory. It rewrites no file, so the package's
// status, updated_at and content stay as they were: the caller writes the
// status first, with Transition or Supersede, and only an archived or a
// superseded package lives under archive/ (format v2 §9).
//
// It creates <root>/archive/ (mode 0755 before the umask) if it is missing.
//
// Returns the LoadError when the package does not load, a LifecycleError when
// its status is neither archived nor superseded, and a SaveError when the
// destination is taken or the directory cannot be created or renamed. In each
// case the package is where it was.
func MoveUnderArchive(specDir, root string) (string, error) {
	spec, err := LoadSpec(specDir)
	if err != nil {
		return "", err
	}
	if spec.Status != "archived" && spec.Status != "superseded" {
		msg := fmt.Sprintf("cannot move a %s spec under archive/; only archived and superseded specs live there", spec.Status)
		return "", &LifecycleError{Msg: msg, Err: &SpecError{Msg: msg}}
	}
	dest, err := ArchiveDestination(specDir, root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		return "", saveFailure("cannot create archive directory: %s", err)
	}
	if err := os.Rename(specDir, dest); err != nil {
		return "", saveFailure("cannot move spec to archive: %s", err)
	}
	return dest, nil
}

// MoveToArchive transitions a spec to the archived state and moves the
// spec directory to {root}/archive/ with MoveUnderArchive.
//
// A spec that is not already archived is transitioned first, a superseded one
// included (superseded to archived is a move the lifecycle allows). The
// destination is looked at before that, so a taken destination leaves the spec
// as it was. Creates the archive directory if it does not exist.
//
// Returns a SaveError if the archive directory already contains a directory
// with the same name, or the spec cannot be loaded, moved or written.
func MoveToArchive(specDir, root string) error {
	// Load the spec from the source directory.
	spec, err := LoadSpec(specDir)
	if err != nil {
		return saveFailure("cannot load spec for archiving: %s", err)
	}

	// Refuse before anything is written: the transition below would leave the
	// spec archived but still in place.
	if _, err := ArchiveDestination(specDir, root); err != nil {
		return err
	}

	// Transition to archived state if not already archived.
	if spec.Status != "archived" {
		if _, err := spec.Transition("archived", specDir); err != nil {
			return err
		}
	}

	_, err = MoveUnderArchive(specDir, root)
	return err
}
