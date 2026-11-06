package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// The three outcomes of rendering one package.
const (
	renderedWritten   = "written"
	renderedUnchanged = "unchanged"
	renderedFailed    = "failed"
)

// renderOne is the form of speca <spec> --render: it writes the package's
// spec.md, whatever its status, and no other file. It does not save the
// package, so updated_at stays as it is. A package that cannot be loaded, or
// whose spec.md cannot be written, fails the call.
func renderOne(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := c.loadPackage(dir)
	if failure != nil {
		return nil, failure
	}
	info := renderedInfo(c, dir, spec, false)
	outcome, err := c.writeRender(dir, spec)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "write", Category: envelope.CategoryDisk, Message: err.Error()}
	}
	info.Rendered = outcome
	return &Result{AdminResult: envelope.AdminResult{Rendered: []envelope.RenderedInfo{info}, DryRun: c.opts.dryRun}, renderForm: true}, nil
}

// renderedInfo is the entry of the package in dir, without its outcome.
func renderedInfo(c *call, dir string, spec *afspec.Spec, archived bool) envelope.RenderedInfo {
	return envelope.RenderedInfo{SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName,
		Status: spec.Status, Archived: archived}
}

// writeRender brings the spec.md of the package in dir up to date and says
// what it did: unchanged when the file already holds the render, which is
// then not written again, and written otherwise. Under --dry-run it decides
// the same and writes nothing.
func (c *call) writeRender(dir string, spec *afspec.Spec) (string, error) {
	path := filepath.Join(dir, afspec.RenderedFileName)
	want := spec.RenderedBytes()
	if got, err := os.ReadFile(path); err == nil && bytes.Equal(got, want) {
		return renderedUnchanged, nil
	}
	if c.opts.dryRun {
		return renderedWritten, nil
	}
	if err := cli.WriteAtomic(path, want); err != nil {
		return "", fmt.Errorf("%s could not be written: %w", path, err)
	}
	return renderedWritten, nil
}
