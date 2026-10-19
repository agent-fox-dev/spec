package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "../../afspec/testdata/v2_example"

func TestValidateReportsAValidSpec(t *testing.T) {
	var out strings.Builder
	valid, err := run(fixture, &out)
	if err != nil || !valid {
		t.Fatalf("run = %v, %v; want valid\n%s", valid, err, out.String())
	}
	if !strings.Contains(out.String(), "coverage: 6 of 6") {
		t.Errorf("output lacks the coverage line:\n%s", out.String())
	}
}

func TestValidateReportsBrokenRules(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"prd.md", "requirements.json", "test_spec.json", "tasks.json"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		// Drop the only smoke test, which leaves its path unverified (C5).
		if name == "test_spec.json" {
			data = []byte(strings.Replace(string(data), `"09-PATH-1",`, "", 1))
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
	if !strings.Contains(out.String(), "[C5]") {
		t.Errorf("output does not name rule C5:\n%s", out.String())
	}
}

func TestValidateFailsOnAMissingDirectory(t *testing.T) {
	if _, err := run(filepath.Join(t.TempDir(), "missing"), io.Discard); err == nil {
		t.Fatal("run = nil; want an error")
	}
}
