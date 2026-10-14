package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// --schema is the interface, generated from the flag set and the Go types.
// A change to either shows up as a diff to the golden file, which is where a
// reviewer sees the interface change.
// UPDATE_GOLDEN=1 go test -run '^TestSchemaGolden$' ./cmd/specr regenerates it.
func TestSchemaGolden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--schema", "--dir", "/nonexistent", "anything"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	golden := filepath.Join("testdata", "schema.golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, stdout.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (UPDATE_GOLDEN=1 writes it)", err)
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Errorf("--schema differs from %s; if the interface change is deliberate, regenerate it with UPDATE_GOLDEN=1", golden)
	}
}

// The document is usable as a tool definition: the flags are a JSON Schema
// object without --schema and --version, and the result names every field
// of the result type with a description.
func TestSchemaDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	Main([]string{"--schema"}, &stdout, &stderr)
	var doc struct {
		Tool  string `json:"tool"`
		Flags struct {
			Type       string                    `json:"type"`
			Properties map[string]map[string]any `json:"properties"`
		} `json:"flags"`
		Result struct {
			Properties map[string]struct {
				Properties map[string]map[string]any `json:"properties"`
			} `json:"properties"`
		} `json:"result"`
		ExitCodes map[string]string `json:"exit_codes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Tool != "specr" || doc.Flags.Type != "object" || len(doc.ExitCodes) != 3 {
		t.Errorf("tool %q flags %q exit codes %v", doc.Tool, doc.Flags.Type, doc.ExitCodes)
	}
	for _, name := range []string{"task", "section", "max-tokens", "markdown-file", "specs-dir", "dir"} {
		if doc.Flags.Properties[name]["description"] == "" {
			t.Errorf("flag %s is not described", name)
		}
	}
	for _, name := range []string{"schema", "version"} {
		if _, ok := doc.Flags.Properties[name]; ok {
			t.Errorf("flag %s is listed", name)
		}
	}
	if doc.Flags.Properties["task"]["type"] != "integer" {
		t.Errorf("--task is %v", doc.Flags.Properties["task"]["type"])
	}
	result := doc.Result.Properties["result"].Properties
	for _, name := range []string{"spec_dir", "scope", "task", "sections", "reduced", "tokens", "tasks", "markdown"} {
		if d, _ := result[name]["description"].(string); d == "" {
			t.Errorf("result.%s is not described", name)
		}
	}
	for _, name := range []string{"model", "usage", "input", "side_effects"} {
		if _, ok := doc.Result.Properties[name]; ok {
			t.Errorf("envelope field %s is described, and specr never writes it", name)
		}
	}
}
