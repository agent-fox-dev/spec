package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agent-fox-dev/spec/afspec"
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
// the list, the verdict on one package, or the report on it. A form returns a
// result or a failure, never both.
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
	if o.validate {
		return verdict(c, dir)
	}
	return report(c, dir)
}

// notYet is the failure a form returns until it is written.
func notYet(form string) *cli.ErrorInfo {
	return &cli.ErrorInfo{Stage: "load", Category: "input", Message: form + " form not yet implemented"}
}

// report is the form of speca <spec>: the state of the package in dir.
func report(c *call, dir string) (*Result, *cli.ErrorInfo) { return nil, notYet("the report") }

// verdict is the form of speca <spec> --validate: the format's verdict on the
// package in dir.
func verdict(c *call, dir string) (*Result, *cli.ErrorInfo) { return nil, notYet("the verdict") }

// list is the form of speca --list: every package under the spec root.
func list(c *call) (*Result, *cli.ErrorInfo) { return nil, notYet("the list") }
