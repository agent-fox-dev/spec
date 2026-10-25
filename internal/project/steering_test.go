package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSteering_IgnoresAFileWithNothingToSay(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"bare heading", "# Steering\n"},
		{"placeholder", "# Steering\n<!-- steering:placeholder -->\n"},
		{"over the size bound", "# Steering\n\n" + strings.Repeat("x", maxSteeringBytes)},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "steering.md"), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := Steering(dir); got != "" {
				t.Errorf("Steering = %q", got)
			}
		})
	}
}

func TestSteering_IgnoresAMissingFile(t *testing.T) {
	if got := Steering(t.TempDir()); got != "" {
		t.Errorf("Steering = %q", got)
	}
}

// A symlink could point anywhere on the machine; steering is only read from a
// regular file in the specs directory.
func TestSteering_IgnoresASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(target, []byte("# Steering\n\nUse make check.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "steering.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := Steering(dir); got != "" {
		t.Errorf("Steering followed a symlink: %q", got)
	}
}

func TestSteering_ReturnsTheDirectives(t *testing.T) {
	dir := t.TempDir()
	body := "# Steering\n\nUse make check.\n"
	if err := os.WriteFile(filepath.Join(dir, "steering.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Steering(dir); got != body {
		t.Errorf("Steering = %q, want %q", got, body)
	}
}
