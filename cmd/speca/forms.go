package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// call is what a form works from: the flags, the run its warnings go to, and
// the two directories everything is resolved against.
type call struct {
	opts     *options
	run      *cli.Run
	root     string
	specsDir string
}

// execute resolves where the call works, then runs the form the flags name:
// the list, the status form, the task form, the verdict on one package, or the
// report on it. A form returns a result or a failure, never both, except where
// a form says otherwise.
func execute(ref string, o *options, run *cli.Run) (*Result, *cli.ErrorInfo) {
	root, err := filepath.Abs(o.dir)
	if err != nil {
		return nil, usageInfo(fmt.Errorf("resolving --dir %q: %w", o.dir, err), nil)
	}
	if info, serr := os.Stat(root); serr != nil || !info.IsDir() {
		return nil, usageInfo(fmt.Errorf("--dir %s is not a directory", root), nil)
	}
	c := &call{opts: o, run: run, root: root, specsDir: afspec.ResolveSpecsDir(o.specsDir, root)}

	if o.list {
		return list(c)
	}
	dir, err := specpkg.Locate(tool, ref, root, c.specsDir)
	if err != nil {
		return nil, usageInfo(err, nil)
	}
	if o.statusForm() {
		return executeStatus(c, dir)
	}
	if o.taskForm() {
		return executeTask(c, dir)
	}
	if o.validate {
		return verdict(c, dir)
	}
	return report(c, dir)
}

// load loads the package in dir. A package the library refuses fails the call
// at stage load, naming the directory and the library's reason.
func load(dir string) (*afspec.Spec, *cli.ErrorInfo) {
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input",
			Message: fmt.Sprintf("the package %s could not be read: %v", dir, err)}
	}
	return spec, nil
}

// loadPackage loads the package in dir for the report and the verdict. One in
// another format version loads, but its tests and tasks are not read, so the
// form reports what did load and raises a high spec_invalid warning to say so.
func (c *call) loadPackage(dir string) (*afspec.Spec, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	if specpkg.Legacy(spec) {
		c.run.Warn(cli.WarnSpecInvalid, "high", "the package is format version %d, and speca reads version %d only: "+
			"its tests and tasks are not read, and what is reported holds only what the two versions share — "+
			"format v2 §13 maps each v1 construct to its v2 form", spec.SchemaVersion, afspec.SchemaVersion)
	}
	return spec, nil
}

// identityOf is what the report and the verdict both say about a package: where
// it is, who it is, and whether its intent has drifted.
func (c *call) identityOf(dir string, spec *afspec.Spec) envelope.AdminResult {
	r := envelope.AdminResult{
		SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName, Title: spec.Title,
		Status: spec.Status, CreatedAt: spec.CreatedAt, UpdatedAt: spec.UpdatedAt,
		IntentDrift: intentDrift(spec),
	}
	if spec.IntentHash != nil && *spec.IntentHash != "" {
		r.IntentHash = *spec.IntentHash
	}
	if len(spec.Supersedes) > 0 {
		r.Supersedes = slices.Clone(spec.Supersedes)
	}
	if len(spec.Tags) > 0 {
		r.Tags = slices.Clone(spec.Tags)
	}
	return r
}

// intentDrift reports whether an active package's intent has been changed
// since it was activated: it has an intent_hash, and its PRD either has no
// ## Intent section or has one that hashes to something else. The library
// refuses every save of such a package, so a caller learns here why.
func intentDrift(spec *afspec.Spec) bool {
	if spec.Status != string(afspec.PrdFrontmatterV2JsonStatusActive) || spec.IntentHash == nil || *spec.IntentHash == "" {
		return false
	}
	hash, err := afspec.ComputeIntentHash(spec.PRDBody)
	return err != nil || hash != *spec.IntentHash
}

// warnUnreadable raises the low specs_dir_unreadable warning: a directory the
// form reads (what, "the spec root" or "the archive") is missing, is not a
// directory or is not readable, and the form goes without what it would have
// read there.
func (c *call) warnUnreadable(what, path string, err error, leftOut string) {
	c.run.Warn(cli.WarnSpecsDirUnreadable, "low", "%s %s could not be read (%v); %s",
		what, specpkg.Display(path, c.root), err, leftOut)
}

// discover lists the packages of the spec root whose PRD can be read. When the
// root itself cannot be read it warns, saying what the form leaves out because
// of it, and reports ok as false. A package whose PRD cannot be parsed is left
// out of metas without a word: the library names such packages in the error,
// and the forms that matter list or load them themselves.
func (c *call) discover(leftOut string) (metas []afspec.SpecMeta, ok bool) {
	metas, err := afspec.DiscoverSpecs(c.specsDir)
	if metas == nil {
		// DiscoverSpecs returns a list, empty at worst, once it has read the root.
		c.warnUnreadable("the spec root", c.specsDir, err, leftOut)
		return nil, false
	}
	return metas, true
}
