package afspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

// immutableSnapshot captures the values of fields that must not change once a
// spec becomes active: spec_id, spec_name, and created_at (Section 9 of the
// spec format: "Immutable after creation").
type immutableSnapshot struct {
	SpecID    string
	SpecName  string
	CreatedAt string
}

// The files of a package, beside prd.md and architecture.md.
const (
	RequirementsFileName = "requirements.json"
	TasksFileName        = "tasks.json"
	// ProgressFileName is the file that holds the state of every task. The
	// tools write it; it is not part of the package's contract (format v3
	// §8.3).
	ProgressFileName = "progress.json"
)

// Spec represents a complete specification package with all artifacts.
// The PRD frontmatter fields are stored directly on the struct, while
// the PRD Markdown body is stored in PRDBody. The JSON artifacts
// are stored as pointers to the auto-generated types.
type Spec struct {
	// PRD frontmatter fields
	SpecID     string
	SpecName   string
	Title      string
	Status     string
	CreatedAt  string
	Source     string
	Supersedes []string
	IntentHash *string
	// ContractHash is the hash of each contract file recorded at activation,
	// keyed by its file name, or nil for a package activated without one
	// (contract.go).
	ContractHash  map[string]string
	SchemaVersion int
	// Profile is the package's profile, ProfileFull or ProfileLite, or ""
	// when the frontmatter names none, which is full (format v3 §4.1).
	Profile string
	// SteeringVersion is the version of the spec root's steering the package
	// was written under, or "" when none was recorded.
	SteeringVersion string

	// UpdatedAt is when a task last changed state, from progress.json, or ""
	// when none has. It is not in the PRD frontmatter: a task move writes the
	// progress file alone (format v3 §8.7).
	UpdatedAt string

	// PRD Markdown body (everything after the closing --- delimiter)
	PRDBody string

	// Dir is the directory path from which this spec was loaded. It is
	// set by LoadSpec and used by ValidateCrossFile to check that the
	// folder name's numeric prefix and snake_case suffix match SpecID
	// and SpecName. Empty when the spec was not loaded from disk.
	Dir string

	// JSON artifacts
	Requirements *RequirementsV3Json
	Tasks        *TasksV3Json
	// Progress is the state of every task, from progress.json, or nil when
	// the package has no progress file yet: every task is then pending.
	Progress *ProgressV3Json

	// Optional architecture document
	Architecture string

	// unknown holds, per artifact file, the keys LoadSpec found on disk that
	// the artifact's schema does not allow. The decode drops them, so they
	// are recorded here for ValidateSchema to report.
	unknown map[string][]string

	// extensions holds, per file ("prd.md" for the frontmatter), the
	// extension keys LoadSpec read, by the address of their object, for every
	// save to write back (extensions.go).
	extensions map[string]extensionSet

	// fromDisk is true for a package LoadSpec read, and for the copies a
	// lifecycle move makes of one. Only such a package is held to the
	// RenderedFileName in its directory: a generation run's dry run sets Dir
	// on a package it never wrote.
	fromDisk bool

	// loaded holds an immutable snapshot of identity fields captured at load
	// time (or at activation via Transition). Non-nil when the spec was loaded
	// in an active state or transitioned to active, enabling Save to reject
	// mutations to spec_id, spec_name, and created_at.
	loaded *immutableSnapshot
}

// LoadSpec reads all spec artifacts from a directory and returns a
// populated Spec struct. It reads prd.md, requirements.json, tasks.json,
// progress.json when it is there, and optionally architecture.md.
func LoadSpec(dir string) (*Spec, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, &LoadError{
			Msg:  fmt.Sprintf("cannot access spec directory: %s", err),
			File: dir,
			Err:  &SpecError{Msg: fmt.Sprintf("cannot access spec directory: %s", err)},
		}
	}
	if !info.IsDir() {
		return nil, &LoadError{
			Msg:  fmt.Sprintf("not a directory: %s", dir),
			File: dir,
			Err:  &SpecError{Msg: fmt.Sprintf("not a directory: %s", dir)},
		}
	}

	// Read and parse prd.md
	prdPath := filepath.Join(dir, "prd.md")
	prdData, err := os.ReadFile(prdPath)
	if err != nil {
		return nil, &LoadError{
			Msg:  fmt.Sprintf("cannot read prd.md: %s", err),
			File: prdPath,
			Err:  &SpecError{Msg: fmt.Sprintf("cannot read prd.md: %s", err)},
		}
	}

	fm, body, err := parsePRD(prdData, prdPath)
	if err != nil {
		return nil, err // parsePRD already returns LoadError
	}

	// Read and parse the JSON artifacts. Decoding checks JSON syntax and
	// Go types only: structural rules live in the compiled schemas and run in
	// Validate. That is what lets an empty scaffold load and be reported as
	// incomplete rather than failing to decode.
	unknown := map[string][]string{}
	extensions := map[string]extensionSet{}
	if len(fm.Extensions) > 0 {
		extensions["prd.md"] = extensionSet{"": fm.Extensions}
	}
	reqPath := filepath.Join(dir, RequirementsFileName)
	var req RequirementsV3Json
	if err := loadJSONArtifact(reqPath, &req, unknown, extensions); err != nil {
		return nil, err
	}

	tasksPath := filepath.Join(dir, TasksFileName)
	var tasks TasksV3Json
	if err := loadJSONArtifact(tasksPath, &tasks, unknown, extensions); err != nil {
		return nil, err
	}

	// progress.json is the one file a package may lack: a package written by
	// hand, or by a program that writes the contract alone, has every task
	// pending until the first save writes it.
	var progress *ProgressV3Json
	progressPath := filepath.Join(dir, ProgressFileName)
	if _, statErr := os.Stat(progressPath); statErr == nil {
		var p ProgressV3Json
		if err := loadJSONArtifact(progressPath, &p, unknown, extensions); err != nil {
			return nil, err
		}
		progress = &p
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return nil, &LoadError{
			Msg:  fmt.Sprintf("cannot read %s: %s", ProgressFileName, statErr),
			File: progressPath,
			Err:  &SpecError{Msg: fmt.Sprintf("cannot read %s: %s", ProgressFileName, statErr)},
		}
	}

	// Read architecture.md (optional)
	var architecture string
	archPath := filepath.Join(dir, "architecture.md")
	archData, err := os.ReadFile(archPath)
	if err == nil {
		architecture = string(archData)
	}
	// If architecture.md doesn't exist, that's fine — leave empty

	spec := &Spec{
		SpecID:          fm.SpecID,
		SpecName:        fm.SpecName,
		Title:           fm.Title,
		Status:          fm.Status,
		CreatedAt:       fm.CreatedAt,
		Source:          fm.Source,
		Supersedes:      fm.Supersedes,
		IntentHash:      fm.IntentHash,
		ContractHash:    fm.ContractHash,
		SchemaVersion:   fm.SchemaVersion,
		Profile:         fm.Profile,
		SteeringVersion: fm.SteeringVersion,
		PRDBody:         body,
		Requirements:    &req,
		Tasks:           &tasks,
		Progress:        progress,
		Architecture:    architecture,
		Dir:             dir,
		unknown:         unknown,
		extensions:      extensions,
		fromDisk:        true,
	}
	if progress != nil {
		spec.UpdatedAt = progress.UpdatedAt
	}

	// Capture immutable snapshot for active specs so Save can detect mutations
	// to spec_id, spec_name, and created_at (Section 9: immutable after creation).
	if fm.Status == "active" {
		spec.loaded = &immutableSnapshot{
			SpecID:    fm.SpecID,
			SpecName:  fm.SpecName,
			CreatedAt: fm.CreatedAt,
		}
	}

	return spec, nil
}

// loadJSONArtifact reads and unmarshals a JSON artifact file, and records in
// unknown the keys its schema does not allow and in extensions its extension
// keys. Returns a LoadError wrapping SpecError on failure.
func loadJSONArtifact(path string, target interface{}, unknown map[string][]string, extensions map[string]extensionSet) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return &LoadError{
			Msg:  fmt.Sprintf("cannot read %s: %s", filepath.Base(path), err),
			File: path,
			Err:  &SpecError{Msg: fmt.Sprintf("cannot read %s: %s", filepath.Base(path), err)},
		}
	}

	if err := json.Unmarshal(data, target); err != nil {
		return &LoadError{
			Msg:  fmt.Sprintf("cannot parse %s: %s", filepath.Base(path), err),
			File: path,
			Err:  &SpecError{Msg: fmt.Sprintf("cannot parse %s: %s", filepath.Base(path), err)},
		}
	}

	// The decode above drops a key the schema does not allow; it is kept
	// aside for ValidateSchema, so the package still loads and is reported
	// invalid rather than unreadable.
	var raw any
	if json.Unmarshal(data, &raw) == nil {
		if keys := UnknownFields(raw, target); len(keys) > 0 {
			unknown[filepath.Base(path)] = keys
		}
		set := extensionSet{}
		collectExtensions(raw, reflect.TypeOf(target), "", set)
		if len(set) > 0 {
			extensions[filepath.Base(path)] = set
		}
	}
	return nil
}

// Save atomically writes all spec artifacts to the given directory, the
// progress file, and the package's render beside them as RenderedFileName.
// Each file is written to a temporary file first, then renamed to its final
// name. Returns a LifecycleError if the spec is sealed, superseded, or
// archived.
//
// For active specs with a stored IntentHash, Save recomputes the hash from
// the current PRD body and returns an IntentError if the ## Intent section
// has changed since activation (intent drift detection).
func (s *Spec) Save(dir string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	return s.saveToDisk(dir)
}

// SaveProgress writes the package's progress file alone, atomically, and no
// other file: it is what a task move writes (format v3 §8.7). The progress
// holds every task of the plan and the integration task, in id order, with
// the states the Spec carries, and UpdatedAt. The lifecycle and intent guards
// of Save apply: a sealed package takes no task move either.
func (s *Spec) SaveProgress(dir string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return saveFailure("cannot access directory: %s", err)
	}
	data, err := marshalArtifact(s.progressArtifact(), s.extensions[ProgressFileName])
	if err != nil {
		return saveFailure("cannot marshal %s: %s", ProgressFileName, err)
	}
	tmpPath, err := writeTempFile(dir, ProgressFileName, data)
	if err != nil {
		return saveFailure("cannot write temp file for %s: %s", ProgressFileName, err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, ProgressFileName)); err != nil {
		os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return saveFailure("cannot rename temp file to %s: %s", ProgressFileName, err)
	}
	return nil
}

// checkWritable is what Save and SaveProgress refuse: a sealed, superseded or
// archived package, an active package whose identity changed, and an active
// package whose intent drifted.
func (s *Spec) checkWritable() error {
	if s.Status == "sealed" || s.Status == "superseded" || s.Status == "archived" {
		return &LifecycleError{
			Msg: fmt.Sprintf("cannot save spec in %s state", s.Status),
			Err: &SpecError{Msg: fmt.Sprintf("cannot save spec in %s state", s.Status)},
		}
	}

	// For active specs with a stored immutable snapshot, reject mutations to
	// spec_id, spec_name, and created_at (Section 9: immutable after creation).
	if s.Status == "active" && s.loaded != nil {
		if s.SpecID != s.loaded.SpecID {
			msg := fmt.Sprintf("cannot mutate immutable field spec_id on active spec (was %q, now %q)", s.loaded.SpecID, s.SpecID)
			return &LifecycleError{
				Msg: msg,
				Err: &SpecError{Msg: msg},
			}
		}
		if s.SpecName != s.loaded.SpecName {
			msg := fmt.Sprintf("cannot mutate immutable field spec_name on active spec (was %q, now %q)", s.loaded.SpecName, s.SpecName)
			return &LifecycleError{
				Msg: msg,
				Err: &SpecError{Msg: msg},
			}
		}
		if s.CreatedAt != s.loaded.CreatedAt {
			msg := fmt.Sprintf("cannot mutate immutable field created_at on active spec (was %q, now %q)", s.loaded.CreatedAt, s.CreatedAt)
			return &LifecycleError{
				Msg: msg,
				Err: &SpecError{Msg: msg},
			}
		}
	}

	// For active specs with a stored intent hash, detect drift by recomputing
	// the hash from the current PRD body and comparing against the stored value.
	if s.Status == "active" && s.IntentHash != nil {
		current, err := ComputeIntentHash(s.PRDBody)
		if err != nil {
			return err
		}
		if current != *s.IntentHash {
			msg := "intent drift detected: ## Intent section has changed since activation"
			return &IntentError{
				Msg: msg,
				Err: &SpecError{Msg: msg},
			}
		}
	}
	return nil
}

// saveToDisk is the internal save method that bypasses lifecycle checks.
// It is used by Transition, Supersede, MoveToArchive and MigrateV2, which
// need to persist specs in sealed, superseded, or archived states.
//
// Two-phase write strategy:
//
//	Phase 1 — all artifact data is written to temporary files (*.tmp.*) in
//	           dir and each file is fsynced to disk. If any write fails at
//	           this stage, all temporary files are removed and no on-disk
//	           artifact file is modified.
//	Phase 2 — all temporary files are renamed to their final names. Because
//	           renames are fast filesystem metadata operations on the same
//	           device, the window for inconsistency across artifacts is
//	           minimal compared to the previous interleaved write-then-rename
//	           approach.
//
// On return (success or failure) no *.tmp.* files remain in dir.
func (s *Spec) saveToDisk(dir string) error {
	// Verify directory exists
	if _, err := os.Stat(dir); err != nil {
		return saveFailure("cannot access directory: %s", err)
	}

	// Render PRD
	prdContent := renderPRD(s)

	// Marshal JSON artifacts
	reqData, err := marshalArtifact(s.Requirements, s.extensions[RequirementsFileName])
	if err != nil {
		return saveFailure("cannot marshal %s: %s", RequirementsFileName, err)
	}

	tasksData, err := marshalArtifact(s.Tasks, s.extensions[TasksFileName])
	if err != nil {
		return saveFailure("cannot marshal %s: %s", TasksFileName, err)
	}

	progressData, err := marshalArtifact(s.progressArtifact(), s.extensions[ProgressFileName])
	if err != nil {
		return saveFailure("cannot marshal %s: %s", ProgressFileName, err)
	}

	// Build the list of (name, data) pairs to persist.
	type artifact struct {
		name    string
		data    []byte
		tmpPath string
	}

	artifacts := []artifact{
		{name: "prd.md", data: prdContent},
		{name: RequirementsFileName, data: reqData},
		{name: TasksFileName, data: tasksData},
		{name: ProgressFileName, data: progressData},
	}

	if s.Architecture != "" {
		artifacts = append(artifacts, artifact{name: "architecture.md", data: []byte(s.Architecture)})
	}
	// The render goes through the same two phases, so it never falls behind
	// the artifacts it is derived from (ADR 14).
	artifacts = append(artifacts, artifact{name: RenderedFileName, data: s.RenderedBytes()})

	// tmpPaths accumulates paths of temp files that need cleanup on failure.
	// The deferred function removes any paths still in the slice, so callers
	// must clear the slice before returning nil to suppress cleanup on success.
	var tmpPaths []string
	defer func() {
		for _, p := range tmpPaths {
			os.Remove(p) //nolint:errcheck // best-effort cleanup
		}
	}()

	// Phase 1: write all artifact data to temp files, fsyncing each before
	// closing. If any write fails, the deferred cleanup removes all temp files
	// and no final artifact file has been modified.
	for i := range artifacts {
		tmpPath, err := writeTempFile(dir, artifacts[i].name, artifacts[i].data)
		if err != nil {
			return saveFailure("cannot write temp file for %s: %s", artifacts[i].name, err)
		}
		artifacts[i].tmpPath = tmpPath
		tmpPaths = append(tmpPaths, tmpPath)
	}

	// Phase 2: rename all temp files to their final names. Renames on the
	// same filesystem are atomic metadata operations; sequencing them all
	// after a fully-successful write phase minimises the inconsistency window
	// to near-zero.
	for _, a := range artifacts {
		finalPath := filepath.Join(dir, a.name)
		if err := os.Rename(a.tmpPath, finalPath); err != nil {
			return saveFailure("cannot rename temp file to %s: %s", a.name, err)
		}
	}

	// All renames succeeded; clear the cleanup list — temp paths no longer exist.
	tmpPaths = tmpPaths[:0]
	return nil
}

// progressArtifact is the progress file as a save writes it: the package's
// identity, UpdatedAt, and one entry per task of the plan and for the
// integration task, in id order, with the state and the evidence the Spec
// carries for each. An
// entry that names no task of the package is not written.
func (s *Spec) progressArtifact() *ProgressV3Json {
	p := &ProgressV3Json{
		Schema:        progressSchemaURI,
		SpecId:        s.SpecID,
		SpecName:      s.SpecName,
		SchemaVersion: SchemaVersion,
		UpdatedAt:     s.UpdatedAt,
		Tasks:         []TaskProgress{},
	}
	for _, t := range s.Plan() {
		p.Tasks = append(p.Tasks, TaskProgress{Id: t.Id, State: t.State, Evidence: s.TaskEvidence(t.Id)})
	}
	return p
}

// writeTempFile writes data to a new temporary file inside dir (named
// "<name>.tmp.<random>"), fsyncs it, and returns the path. The caller is
// responsible for either renaming the file to its final destination or
// removing it on failure.
func writeTempFile(dir, name string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, name+".tmp.*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}
	return tmpPath, nil
}
