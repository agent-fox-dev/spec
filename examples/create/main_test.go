package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

func TestCreateWritesAValidActiveSpec(t *testing.T) {
	root := t.TempDir()
	var stdout strings.Builder
	if err := run([]string{"-root", root}, &stdout, io.Discard); err != nil {
		t.Fatalf("run = %v", err)
	}

	dir := filepath.Join(root, "01_greet_command")
	if got := strings.TrimSpace(stdout.String()); got != dir {
		t.Errorf("stdout = %q; want %q", got, dir)
	}

	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatalf("LoadSpec = %v", err)
	}
	if spec.Status != "active" || spec.IntentHash == nil {
		t.Errorf("status %q, intent hash %v; want active with a hash", spec.Status, spec.IntentHash)
	}
	if result := spec.Validate(); !result.Valid || len(result.Warnings) > 0 {
		t.Errorf("Validate = %+v; want valid without warnings", result)
	}
}
