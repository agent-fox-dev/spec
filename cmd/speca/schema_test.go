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

// --schema is the interface, generated from the flag set and the Go types,
// without reading any directory. A change to either shows up as a diff to the
// golden file, which is where a reviewer sees the interface change.
// UPDATE_GOLDEN=1 go test -run '^TestSchema_MatchesTheGoldenFile$' ./cmd/speca
// regenerates it.
func TestSchema_MatchesTheGoldenFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--schema", "--dir", "/nonexistent", "anything"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	golden := filepath.Join("testdata", "schema.golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		mustMkdir(t, "testdata")
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

// The document is usable as a tool definition, so that a regenerated golden
// file cannot quietly lose what an agent needs: the description ends with the
// sentence naming the three commands, the flags are a JSON Schema object with
// every operational flag described and the enums of --state and --status, and
// the result names every field of the result type with a description.
func TestSchema_DescribesTheFormsFlagsAndResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--schema"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	var doc struct {
		Tool        string `json:"tool"`
		Description string `json:"description"`
		Input       struct {
			Description string   `json:"description"`
			Examples    []string `json:"examples"`
		} `json:"input"`
		Flags struct {
			Type       string `json:"type"`
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
	if doc.Tool != "speca" || doc.Flags.Type != "object" || len(doc.ExitCodes) != 3 {
		t.Errorf("tool %q flags %q exit codes %v", doc.Tool, doc.Flags.Type, doc.ExitCodes)
	}
	if d := doc.Description; !strings.HasSuffix(d, " "+cli.SiblingCommands) || strings.Count(d, cli.SiblingCommands) != 1 || !strings.Contains(d, "task") {
		t.Errorf("description %q does not mention the task form, or does not end with the sentence naming the three commands, once", d)
	}
	if doc.Input.Description == "" || len(doc.Input.Examples) == 0 {
		t.Errorf("input = %+v", doc.Input)
	}

	flags := doc.Flags.Properties
	for name, typ := range map[string]string{
		"validate": "boolean", "list": "boolean", "all": "boolean", "specs-dir": "string", "dir": "string",
		"task": "", "state": "string", "force": "boolean", "dry-run": "boolean", "status": "string", "by": "string",
	} {
		if flags[name].Description == "" || typ != "" && flags[name].Type != typ {
			t.Errorf("flag %s = %+v; want a described %s", name, flags[name], typ)
		}
	}
	for _, name := range []string{"schema", "version"} {
		if _, ok := flags[name]; ok {
			t.Errorf("flag %s is listed", name)
		}
	}
	if got := flags["state"].Enum; !slices.Equal(got, wantTaskStates) {
		t.Errorf("--state enum = %v; want %v", got, wantTaskStates)
	}
	if got := flags["status"].Enum; !slices.Equal(got, wantStatusTargets) {
		t.Errorf("--status enum = %v; want %v", got, wantStatusTargets)
	}
	for _, name := range []string{"force", "dry-run"} {
		if d := flags[name].Description; !strings.Contains(d, "--status") {
			t.Errorf("--%s is described as %q, which does not say it applies to the status form", name, d)
		}
	}

	result := doc.Result.Properties["result"].Properties
	for _, name := range []string{"spec_dir", "spec_id", "status", "valid", "failing_rules", "intent_drift", "tasks", "task_counts",
		"next_task", "allowed_status", "depends_on", "dependents", "validation", "traceability", "packages", "changes", "dry_run"} {
		if d, _ := result[name]["description"].(string); d == "" {
			t.Errorf("result.%s is not described", name)
		}
	}
	for _, name := range []string{"model", "usage", "input", "side_effects"} {
		if _, ok := doc.Result.Properties[name]; ok {
			t.Errorf("envelope field %s is described, and speca never writes it", name)
		}
	}
	for _, code := range []string{"1", "2"} {
		if !strings.Contains(doc.ExitCodes[code], "refus") {
			t.Errorf("exit code %s is described as %q; want it to cover the refusals", code, doc.ExitCodes[code])
		}
	}
}

// A --schema document that cannot be encoded is an error envelope at stage
// schema, not a lost exit code.
func TestSchema_ThatCannotBeEncodedIsAnError(t *testing.T) {
	failWith(t, &encodeJSON, func(any) ([]byte, error) { return nil, os.ErrInvalid })

	env, code, _ := run(t, "--schema")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "schema" || env.Error.Category != "internal" || env.Result != nil {
		t.Errorf("code %d: %+v", code, env.Error)
	}
}
