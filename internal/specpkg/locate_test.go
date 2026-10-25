package specpkg

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// A reference is a path first, then a name, a number or a bare name, and an
// archived package is reached by its path only.
func TestLocate_ResolvesPathsNamesAndNumbers(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, ".specs")
	makePackage(t, filepath.Join(specs, "01_a"))
	makePackage(t, filepath.Join(specs, "09_b"))
	makePackage(t, filepath.Join(specs, "archive", "09_old"))
	want := filepath.Join(specs, "09_b")

	t.Chdir(root)
	for name, ref := range map[string]string{
		"absolute path":       want,
		"relative path":       filepath.Join(".specs", "09_b"),
		"a file in it":        filepath.Join(".specs", "09_b", "prd.md"),
		"whole name":          "09_b",
		"number":              "09",
		"number without zero": "9",
		"bare name":           "b",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Locate("speca", ref, root, specs)
			if err != nil {
				t.Fatal(err)
			}
			sameDir(t, got, want)
		})
	}

	t.Run("path relative to the root from elsewhere", func(t *testing.T) {
		t.Chdir(t.TempDir())
		got, err := Locate("speca", filepath.Join(".specs", "09_b"), root, specs)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, got, want)
	})

	t.Run("archived package by its path", func(t *testing.T) {
		archived := filepath.Join(specs, "archive", "09_old")
		got, err := Locate("specr", archived, root, specs)
		if err != nil {
			t.Fatal(err)
		}
		sameDir(t, got, archived)
	})
}

// A refusal is a usage error that begins with the tool that was called, and
// names what the caller can give instead.
func TestLocate_RefusesWithAUsageErrorNamingTheTool(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, ".specs")
	makePackage(t, filepath.Join(specs, "09_b"))
	makePackage(t, filepath.Join(specs, "09_c"))
	makePackage(t, filepath.Join(specs, "archive", "05_old"))
	mkdirs(t, specs, "07_empty")

	for _, tool := range []string{"specr", "speca"} {
		for _, tc := range []struct {
			name, ref, mention string
		}{
			{"stdin", "-", "not stdin"},
			{"URL", "https://example.com/x", "not a URL"},
			{"multi-line", "09_b\n09_c", "not several lines"},
			{"unknown", "nothing_here", "09_b, 09_c"},
			{"ambiguous", "09", "09_b, 09_c"},
			{"archived", "05", "05_old"},
			{"not a package", filepath.Join(specs, "07_empty"), "no prd.md"},
			{"the spec root", specs, "no prd.md"},
		} {
			t.Run(tool+" "+tc.name, func(t *testing.T) {
				_, err := Locate(tool, tc.ref, root, specs)
				var usage *cli.UsageError
				if !errors.As(err, &usage) || usage.Category() != "usage" {
					t.Fatalf("err = %v; want a *cli.UsageError", err)
				}
				if !strings.HasPrefix(err.Error(), tool) {
					t.Errorf("message %q does not begin with the tool", err)
				}
				if !strings.Contains(err.Error(), tc.mention) {
					t.Errorf("message %q does not mention %q", err, tc.mention)
				}
			})
		}
	}

	t.Run("unreadable spec root", func(t *testing.T) {
		if _, err := Locate("speca", "09", root, filepath.Join(root, "missing")); err == nil ||
			!strings.Contains(err.Error(), "could not be read") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("empty spec root", func(t *testing.T) {
		empty := t.TempDir()
		if _, err := Locate("speca", "09", empty, empty); err == nil || !strings.Contains(err.Error(), "no spec packages") {
			t.Errorf("err = %v", err)
		}
	})
}

// List holds the NN_name directories and no other entry, in the order of their
// numbers: 100 follows 99, though it sorts before it as text, and a tie on the
// number is broken by the whole name.
func TestList(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "01_a", "10_b", "misc", "archive")
	for _, file := range []string{"notes.txt", "20_file"} {
		if err := os.WriteFile(filepath.Join(root, file), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List(root)
	if err != nil || !slices.Equal(got, []string{"01_a", "10_b"}) {
		t.Errorf("List = %v, %v; want [01_a 10_b]", got, err)
	}
	if _, err := List(filepath.Join(root, "missing")); err == nil {
		t.Error("List of a missing root = nil error")
	}

	numeric := t.TempDir()
	mkdirs(t, numeric, "100_x", "99_y", "02_b", "02_a", "002_c")
	if got, _ := List(numeric); !slices.Equal(got, []string{"002_c", "02_a", "02_b", "99_y", "100_x"}) {
		t.Errorf("List = %v; want 002_c 02_a 02_b 99_y 100_x", got)
	}
}
