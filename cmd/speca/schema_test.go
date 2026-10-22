package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// TS-10-20: --schema is the interface, generated from the flag set and the Go
// types, without reading any directory. A change to either shows up as a diff
// to the golden file, which is where a reviewer sees the interface change.
// UPDATE_GOLDEN=1 go test -run '^TestSchemaGolden_TS10_20$' ./cmd/speca
// regenerates it.
func TestSchemaGolden_TS10_20(t *testing.T) {
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

// The document is usable as a tool definition: the description names the
// three commands, the flags are a JSON Schema object without --schema and
// --version, and the result names every field of the result type with a
// description.
func TestSchemaDocument_TS10_20(t *testing.T) {
	var stdout, stderr bytes.Buffer
	Main([]string{"--schema"}, &stdout, &stderr)
	var doc struct {
		Tool        string `json:"tool"`
		Description string `json:"description"`
		Input       struct {
			Description string   `json:"description"`
			Examples    []string `json:"examples"`
		} `json:"input"`
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
	if doc.Tool != "speca" || doc.Flags.Type != "object" || len(doc.ExitCodes) != 3 {
		t.Errorf("tool %q flags %q exit codes %v", doc.Tool, doc.Flags.Type, doc.ExitCodes)
	}
	if !strings.Contains(doc.Description, siblingSentence) {
		t.Errorf("description %q lacks the sentence naming the three commands", doc.Description)
	}
	if doc.Input.Description == "" || len(doc.Input.Examples) == 0 {
		t.Errorf("input = %+v", doc.Input)
	}
	for _, name := range []string{"validate", "list", "all", "specs-dir", "dir"} {
		if doc.Flags.Properties[name]["description"] == "" {
			t.Errorf("flag %s is not described", name)
		}
	}
	for _, name := range []string{"schema", "version"} {
		if _, ok := doc.Flags.Properties[name]; ok {
			t.Errorf("flag %s is listed", name)
		}
	}
	if doc.Flags.Properties["validate"]["type"] != "boolean" || doc.Flags.Properties["dir"]["type"] != "string" {
		t.Errorf("flag types: --validate %v, --dir %v", doc.Flags.Properties["validate"]["type"], doc.Flags.Properties["dir"]["type"])
	}
	result := doc.Result.Properties["result"].Properties
	for _, name := range []string{"spec_dir", "spec_id", "status", "valid", "failing_rules", "intent_drift", "tasks", "task_counts",
		"next_task", "allowed_status", "depends_on", "dependents", "validation", "traceability", "packages"} {
		if d, _ := result[name]["description"].(string); d == "" {
			t.Errorf("result.%s is not described", name)
		}
	}
	for _, name := range []string{"model", "usage", "input", "side_effects"} {
		if _, ok := doc.Result.Properties[name]; ok {
			t.Errorf("envelope field %s is described, and speca never writes it", name)
		}
	}
}

// TS-11-53: --schema describes the task form: the new flags, with --state an
// enum of the four states, the new result fields, and exit codes that cover the
// refusals. It is the golden file, byte for byte.
func TestSchemaDescribesTheTaskForm_TS11_53(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--schema", "--dir", "/nonexistent", "anything"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "schema.golden.json"))
	if err != nil || !bytes.Equal(stdout.Bytes(), golden) {
		t.Errorf("--schema differs from the golden file (%v)", err)
	}
	var doc struct {
		Description string `json:"description"`
		Flags       struct {
			Properties map[string]struct {
				Type        string   `json:"type"`
				Enum        []string `json:"enum"`
				Description string   `json:"description"`
			} `json:"properties"`
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
	if got := doc.Flags.Properties["state"].Enum; !slices.Equal(got, []string{"pending", "in_progress", "done", "dropped"}) {
		t.Errorf("--state enum = %v; want the four states", got)
	}
	for _, name := range []string{"task", "state", "force", "dry-run"} {
		if doc.Flags.Properties[name].Description == "" {
			t.Errorf("flag %s is not described", name)
		}
	}
	if doc.Flags.Properties["force"].Type != "boolean" || doc.Flags.Properties["dry-run"].Type != "boolean" {
		t.Errorf("--force is %s, --dry-run %s; want booleans", doc.Flags.Properties["force"].Type, doc.Flags.Properties["dry-run"].Type)
	}
	result := doc.Result.Properties["result"].Properties
	for _, name := range []string{"changes", "dry_run"} {
		if d, _ := result[name]["description"].(string); d == "" {
			t.Errorf("result.%s is not described", name)
		}
	}
	for _, code := range []string{"1", "2"} {
		if !strings.Contains(doc.ExitCodes[code], "refus") {
			t.Errorf("exit code %s is described as %q; want it to cover the refusals", code, doc.ExitCodes[code])
		}
	}
	if strings.Contains(doc.Description, "It changes nothing") || !strings.Contains(doc.Description, "task") {
		t.Errorf("description %q still says the command changes nothing, or does not mention the task form", doc.Description)
	}
}

// TS-10-56: a --schema document that cannot be encoded is an error envelope
// at stage schema, not a lost exit code.
func TestSchemaThatCannotBeEncodedIsAnError_TS10_56(t *testing.T) {
	was := encodeJSON
	t.Cleanup(func() { encodeJSON = was })
	encodeJSON = func(any) ([]byte, error) { return nil, os.ErrInvalid }

	env, code, _ := run(t, "--schema")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "schema" || env.Error.Category != "internal" || env.Result != nil {
		t.Errorf("code %d: %+v", code, env.Error)
	}
}
