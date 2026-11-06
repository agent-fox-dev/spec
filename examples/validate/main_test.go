package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "../../afspec/testdata/v3_example"

func TestValidate_ReportsAValidSpec(t *testing.T) {
	var out strings.Builder
	valid, err := run(fixture, &out)
	if err != nil || !valid {
		t.Fatalf("run = %v, %v; want valid\n%s", valid, err, out.String())
	}
	if !strings.Contains(out.String(), "coverage: 6 of 6") {
		t.Errorf("output lacks the coverage line:\n%s", out.String())
	}
}

func TestValidate_ReportsABrokenRule(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"prd.md", "requirements.json", "tasks.json", "progress.json"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		// Replace the first check with one shared with a criterion the spec
		// does not have, which leaves it unresolved (C3).
		if name == "requirements.json" {
			text := string(data)
			start := strings.Index(text, `"check": {`)
			end := start + strings.Index(text[start:], "\n          }") + len("\n          }")
			data = []byte(text[:start] + `"check": { "same_as": "REQ-9.9" }` + text[end:])
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out strings.Builder
	valid, err := run(dir, &out)
	if err != nil || valid {
		t.Fatalf("run = %v, %v; want invalid", valid, err)
	}
	if !strings.Contains(out.String(), "[C3]") {
		t.Errorf("output does not name rule C3:\n%s", out.String())
	}
}

func TestValidate_FailsOnAMissingDirectory(t *testing.T) {
	if _, err := run(filepath.Join(t.TempDir(), "missing"), io.Discard); err == nil {
		t.Fatal("run = nil; want an error")
	}
}
