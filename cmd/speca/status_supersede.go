package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// The library calls superseding makes are variables, so that a test can fail
// them.
var (
	supersedeSpec = (*afspec.Spec).Supersede
	saveReplacing = (*afspec.Spec).Save
)

// checkReplacing refuses a --by package that cannot replace another: only a
// draft, an active or a sealed one can. A superseded or archived package is
// finished. --force does not override it, and the hint names the flag to
// change.
func (sc *statusCall) checkReplacing() *cli.ErrorInfo {
	switch sc.by.Status {
	case string(afspec.PrdFrontmatterV2JsonStatusDraft), string(afspec.PrdFrontmatterV2JsonStatusActive), string(afspec.PrdFrontmatterV2JsonStatusSealed):
		return nil
	}
	return lifecycleRefusal(fmt.Sprintf("the --by package %s is %s, and a %s package cannot replace another one: name a draft, active or sealed package",
		specpkg.Display(sc.byDir, sc.root), sc.by.Status, sc.by.Status), &cli.FixHint{Flag: "--by"})
}

// supersede writes the old package's banner and its superseded status. The
// replacing package is named in the banner by its spec id.
func (sc *statusCall) supersede() error {
	_, err := supersedeSpec(sc.spec, sc.by.SpecID, sc.dir)
	return err
}

// recordSupersedes adds the old package's spec_id to the end of the replacing
// package's supersedes, unless it is there. It saves the replacing package only
// when that can be done without harm: it is a draft, or an active package whose
// intent has not drifted, and it holds no key a save would drop. In any other
// case, or when the save fails, the call goes on: the change the caller asked for
// is the old package's, and a low supersedes_not_recorded warning says what was
// not recorded and why. --force does not reach the replacing package.
//
// It returns the change it made, or nil.
func (sc *statusCall) recordSupersedes() *envelope.Change {
	oldID, by := sc.spec.SpecID, sc.by
	if slices.Contains(by.Supersedes, oldID) {
		return nil
	}
	byName := specpkg.Display(sc.byDir, sc.root)
	notRecorded := func(reason string) *envelope.Change {
		sc.run.Warn(cli.WarnSupersedesNotRecorded, "low", "%s was not given the id %s in its supersedes, because %s; add it to prd.md yourself",
			byName, oldID, reason)
		return nil
	}
	switch {
	case by.Status == string(afspec.PrdFrontmatterV2JsonStatusSealed):
		return notRecorded("it is sealed, and a sealed package cannot be saved")
	case intentDrift(by):
		return notRecorded("its ## Intent has changed since it was activated, and the library refuses every save of such a package")
	}
	if keys := append(unknownKeys(by.Validate().Errors), frontmatterUnknownKeys(sc.byDir)...); len(keys) > 0 {
		return notRecorded("it holds keys the schema does not allow, which a save would drop: " + strings.Join(keys, ", "))
	}
	by.Supersedes = append(slices.Clone(by.Supersedes), oldID)
	by.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveReplacing(by, sc.byDir); err != nil {
		return notRecorded("it could not be saved: " + err.Error())
	}
	return &envelope.Change{Kind: "supersedes", Spec: by.SpecID, Added: oldID}
}
