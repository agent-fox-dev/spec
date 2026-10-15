package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// fixture is afspec's valid package: its PRD body and its three artifacts,
// with the artifacts' $schema field left for the submit handlers to write.
func fixture(t *testing.T) (prdBody string, artifacts map[afspec.GenerationStep]string) {
	t.Helper()
	root := filepath.Join("..", "..", "afspec", "testdata", "valid_spec")
	raw, err := os.ReadFile(filepath.Join(root, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if i := strings.Index(body[4:], "\n---\n"); i >= 0 {
		body = strings.TrimSpace(body[4+i+5:])
	}
	artifacts = map[afspec.GenerationStep]string{}
	for _, step := range afspec.GenerationSteps {
		b, err := os.ReadFile(filepath.Join(root, afspec.ArtifactFileName(step)))
		if err != nil {
			t.Fatal(err)
		}
		artifacts[step] = string(b)
	}
	return body, artifacts
}

func submit(id, tool string, args any) faux.Turn {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxToolCall(id, tool, string(raw))}, StopReason: core.StopReasonToolUse}
}

// The whole command against a scripted model: the real shell, the real
// pipeline, the real agent loop and submit handlers. The model writes the
// PRD, reads one file, then submits the three artifacts; spec writes,
// validates and activates the package and reports it.
func TestSpecWritesAValidPackageEndToEnd(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	turns := []faux.Turn{
		submit("p1", "list_files", map[string]any{}),
		submit("p2", "submit_prd", map[string]any{
			"spec_name": "test_feature", "title": "Test Feature", "body": body,
			"open_questions": []map[string]string{{"question": "Which store?", "decision": "The file store.", "why_unsure": "Not stated."}},
		}),
	}
	for i, step := range afspec.GenerationSteps {
		var content map[string]any
		if err := json.Unmarshal([]byte(artifacts[step]), &content); err != nil {
			t.Fatal(err)
		}
		turns = append(turns, submit("g"+string(rune('1'+i)), "submit_"+string(step), content))
	}
	model := faux.New(turns...)
	t.Cleanup(cli.SetProvider(model))

	dir := goRepo(t)
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--phase-model", "test_spec=claude-haiku-4-5:low", "build the test feature"},
		strings.NewReader(""), &stdout, &stderr)
	// Read the output the way a program would: with the public decoder.
	env, err := envelope.DecodeSpec(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope: %v\n%s", err, stdout.String())
	}
	if code != cli.ExitOK || !env.OK || env.Result == nil {
		t.Fatalf("code = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	r := env.Result
	if r.SpecID != "01" || r.SpecDir != filepath.Join(".specs", "01_test_feature") || r.Status != "active" || !r.Validation.Valid {
		t.Errorf("result = %+v", r)
	}
	if strings.Join(r.Artifacts, ",") != "prd.md,requirements.json,test_spec.json,tasks.json" {
		t.Errorf("artifacts = %v", r.Artifacts)
	}
	if r.Requirements == 0 || r.Tests == 0 || len(r.OpenQuestions) != 1 {
		t.Errorf("counts = %+v", r)
	}
	if len(env.Artifacts) != 1 || env.Artifacts[0].Kind != envelope.ArtifactSpecPackage || !env.Artifacts[0].Valid {
		t.Errorf("envelope artifacts = %+v", env.Artifacts)
	}
	if len(env.Next) != 1 || env.Next[0].Tool != "impl" || env.Next[0].Input != r.SpecDir {
		t.Errorf("next = %+v", env.Next)
	}
	var phases []string
	for _, p := range env.Usage.Phases {
		phases = append(phases, p.Name)
	}
	if strings.Join(phases, ",") != "prd,generate:requirements,generate:test_spec,generate:tasks" {
		t.Errorf("phases = %v", phases)
	}
	// STANDARD runs the PRD and the requirements at high effort and the rest
	// at medium; --phase-model moved the test spec to another model.
	var models []string
	for _, p := range env.Usage.Phases {
		models = append(models, p.Model+"/"+p.Effort)
	}
	if got := strings.Join(models, ","); got != "claude-sonnet-5-5/high,claude-sonnet-5-5/high,claude-haiku-4-5/low,claude-sonnet-5-5/medium" {
		t.Errorf("phase models = %s", got)
	}
	if env.Usage.Turns != len(turns) {
		t.Errorf("turns = %d, want %d", env.Usage.Turns, len(turns))
	}

	// The package on disk is the one reported, and it validates.
	spec, err := afspec.LoadSpec(filepath.Join(dir, r.SpecDir))
	if err != nil {
		t.Fatal(err)
	}
	if v := spec.Validate(); !v.Valid || spec.Status != "active" {
		t.Errorf("on disk: status %s, valid %v: %+v", spec.Status, v.Valid, v.Errors)
	}
	if !strings.Contains(spec.PRDBody, "## Intent") {
		t.Error("the PRD body was not written")
	}

	// Each phase was its own conversation: every generation phase started
	// from one user message, not the PRD phase's transcript.
	for i, req := range model.Requests() {
		if i == 0 || i == 2 {
			if n := len(req.Messages); n != 1 {
				t.Errorf("request %d starts a phase with %d messages, want 1", i, n)
			}
		}
	}
}

// --dry-run runs the same phases and writes nothing.
func TestSpecDryRunWritesNothing(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	turns := []faux.Turn{submit("p", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body})}
	for _, step := range afspec.GenerationSteps {
		var content map[string]any
		_ = json.Unmarshal([]byte(artifacts[step]), &content)
		turns = append(turns, submit("g"+string(step), "submit_"+string(step), content))
	}
	t.Cleanup(cli.SetProvider(faux.New(turns...)))

	dir := goRepo(t)
	var stdout bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--dir", dir, "--dry-run", "--repo-map-tokens", "0", "build the test feature"},
		strings.NewReader(""), &stdout, &bytes.Buffer{})
	if code != cli.ExitOK {
		t.Fatalf("code = %d\n%s", code, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".specs")); !os.IsNotExist(err) {
		t.Errorf("--dry-run created the spec root: %v", err)
	}
	var env struct {
		Artifacts []struct {
			DryRun bool `json:"dry_run"`
		} `json:"artifacts"`
		Next []any `json:"next"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Artifacts) != 1 || !env.Artifacts[0].DryRun || len(env.Next) != 0 {
		t.Errorf("artifacts = %+v, next = %+v", env.Artifacts, env.Next)
	}
}
