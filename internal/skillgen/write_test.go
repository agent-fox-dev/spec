package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hashTree maps every file under root to the hash of its contents, so that two
// runs can be compared file for file.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}

// threeSkills is a clean source tree: each skill links a file, one of them the
// shared pool's, and one reference file links another.
func threeSkills() map[string]string {
	return map[string]string{
		"af-prd/SKILL.md": skillFile("af-prd",
			"# af-prd\n\nRead [references/prd-shape.md](references/prd-shape.md), then {{invoke af-spec}}.\n"),
		"af-spec/SKILL.md": skillFile("af-spec",
			"# af-spec\n\nRead [references/tasks.md](references/tasks.md) and [references/speca-specr.md](references/speca-specr.md).\n"),
		"af-spec/references/tasks.md":       "# the plan\n\nThen [references/verdict.md](references/verdict.md).\n",
		"af-spec/references/verdict.md":     "# the verdict\n",
		"af-impl/SKILL.md":                  skillFile("af-impl", "# af-impl\n\nRead [references/speca-specr.md](references/speca-specr.md).\n"),
		"_shared/references/speca-specr.md": "# speca and specr\n",
		"_shared/references/prd-shape.md":   "# the PRD's shape\n",
	}
}

func TestMain_WritesATreePerAgentAndTheSameBytesOnARerun(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	out := filepath.Join(t.TempDir(), "skills")

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}

	// Every agent gets every skill, with a references/ holding exactly the
	// files that skill includes.
	wantRefs := map[string][]string{
		"af-prd":  {"prd-shape.md"},
		"af-spec": {"speca-specr.md", "tasks.md", "verdict.md"},
		"af-impl": {"speca-specr.md"},
	}
	for _, a := range agents {
		for name, refs := range wantRefs {
			dir := filepath.Join(out, a.name, name)
			if _, err := os.Stat(filepath.Join(dir, skillDoc)); err != nil {
				t.Errorf("%s: %v", a.name, err)
				continue
			}
			entries, err := os.ReadDir(filepath.Join(dir, refsSubdir))
			if err != nil {
				t.Errorf("%s/%s: %v", a.name, name, err)
				continue
			}
			var got []string
			for _, entry := range entries {
				got = append(got, entry.Name())
			}
			slices.Sort(got)
			if !slices.Equal(got, refs) {
				t.Errorf("%s/%s/references/ holds %v, want %v", a.name, name, got, refs)
			}
		}
	}

	first := hashTree(t, out)
	if len(first) != len(agents)*(1+1+3+1+1+1) {
		t.Errorf("the tree holds %d files", len(first))
	}
	if code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("the rerun gave code %d, stderr %q", code, stderr.String())
	}
	if second := hashTree(t, out); !mapsEqual(first, second) {
		t.Error("the rerun wrote different bytes")
	}

	// A file and a whole skill removed from the source leave nothing behind.
	if err := os.Remove(filepath.Join(source, "af-spec", refsSubdir, "verdict.md")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, source, map[string]string{
		"af-spec/SKILL.md": skillFile("af-spec",
			"# af-spec\n\nRead [references/tasks.md](references/tasks.md) and [references/speca-specr.md](references/speca-specr.md).\n"),
		"af-spec/references/tasks.md": "# the plan, with no link to the verdict now\n",
	})
	if err := os.RemoveAll(filepath.Join(source, "af-impl")); err != nil {
		t.Fatal(err)
	}
	if code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("the third run gave code %d, stderr %q", code, stderr.String())
	}
	for _, a := range agents {
		if _, err := os.Stat(filepath.Join(out, a.name, "af-spec", refsSubdir, "verdict.md")); !os.IsNotExist(err) {
			t.Errorf("%s still holds the removed reference file", a.name)
		}
		if _, err := os.Stat(filepath.Join(out, a.name, "af-impl")); !os.IsNotExist(err) {
			t.Errorf("%s still holds the removed skill", a.name)
		}
	}
}

func TestMain_UnderCheckWritesNothing(t *testing.T) {
	source := t.TempDir()
	writeTree(t, source, threeSkills())
	out := filepath.Join(t.TempDir(), "skills")

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", source, "--out", out, "--check"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("--check wrote %s", out)
	}
}

func TestMain_RefusesAnOutputDirectoryItWouldReplaceWhole(t *testing.T) {
	source := t.TempDir()
	for _, out := range []string{".", string(filepath.Separator), source} {
		var stdout, stderr bytes.Buffer
		if code := Main([]string{"--source", source, "--out", out}, &stdout, &stderr); code != exitUsage {
			t.Errorf("--out %q gave code %d, want %d", out, code, exitUsage)
		}
		if !strings.Contains(stderr.String(), "would be replaced whole") {
			t.Errorf("--out %q: stderr %q", out, stderr.String())
		}
	}
}

// mapsEqual compares two file-to-hash maps.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
