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
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func TestMain(m *testing.M) {
	// Unset model-vendor variables so that tests exercise the intended
	// credential paths regardless of the developer's shell environment.
	for _, v := range []string{"AF_MODEL", "AGENTKIT_MODEL", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK",
		"ANTHROPIC_VERTEX_PROJECT_ID", "ANTHROPIC_VERTEX_BASE_URL"} {
		_ = os.Unsetenv(v)
	}
	os.Exit(m.Run())
}

// cleanEnv empties, for the test's duration, every variable through which a
// model or forge credential, a model choice, a spec root or a state directory
// can be found, so a test sees an environment that has none whatever the
// machine carries. A test then sets exactly the ones it means to.
func cleanEnv(t testing.TB) {
	t.Helper()
	for _, v := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK",
		"ANTHROPIC_VERTEX_PROJECT_ID", "ANTHROPIC_VERTEX_BASE_URL",
		"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_API_URL", "GITLAB_TOKEN", "GITLAB_API_URL",
		"AF_MODEL", "AGENTKIT_MODEL", "AF_MODEL_EFFORT", "AF_SPEC_DIR",
	} {
		t.Setenv(v, "")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// withIndex makes the shell build idx as the run's one code-search index.
func withIndex(t *testing.T, idx tools.Index) {
	t.Helper()
	old := agentrun.NewIndex
	agentrun.NewIndex = func(*workspace.Workspace) (tools.Index, error) { return idx, nil }
	t.Cleanup(func() { agentrun.NewIndex = old })
}

// goRepo is an empty Go module to run spec in.
func goRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runSpec drives the real App, as main does, and decodes its envelope with
// the public decoder, the way a program reads it.
func runSpec(t *testing.T, argv ...string) (int, envelope.SpecEnvelope, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), argv, strings.NewReader(""), &stdout, &stderr)
	env, err := envelope.DecodeSpec(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return code, env, stderr.String()
}

// runSpecRaw is runSpec decoding into a map, for a test that compares or
// looks for fields the typed envelope does not name.
func runSpecRaw(t *testing.T, argv ...string) (int, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), argv, strings.NewReader(""), &stdout, &stderr)
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not JSON: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return code, env
}

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

// submit is a scripted model turn that calls tool with args.
func submit(id, tool string, args any) faux.Turn {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxToolCall(id, tool, string(raw))}, StopReason: core.StopReasonToolUse}
}

// generation is the three generation phases of the fixture's package, each
// submitting the fixture's artifact.
func generation(t *testing.T, artifacts map[afspec.GenerationStep]string) []faux.Turn {
	t.Helper()
	var turns []faux.Turn
	for _, step := range afspec.GenerationSteps {
		var content map[string]any
		if err := json.Unmarshal([]byte(artifacts[step]), &content); err != nil {
			t.Fatal(err)
		}
		turns = append(turns, submit("g"+string(step), "submit_"+string(step), content))
	}
	return turns
}

func phaseNames(env envelope.SpecEnvelope) []string {
	var out []string
	if env.Usage != nil {
		for _, p := range env.Usage.Phases {
			out = append(out, p.Name)
		}
	}
	return out
}

// siblingLine fails the test unless help carries the sentence naming the three
// commands on a line of its own, after the title line and before Usage:, with a
// blank line on each side.
func siblingLine(t *testing.T, what, help string) {
	sentence := cli.SiblingCommands
	t.Helper()
	usage := strings.Index(help, "Usage:")
	if usage < 0 {
		t.Errorf("%s: the help has no Usage: line:\n%s", what, help)
		return
	}
	before := help[:usage]
	if !strings.Contains(before, "\n\n"+sentence+"\n\n") {
		t.Errorf("%s: the sentence is not on a line of its own, with a blank line on each side, before Usage::\n%s", what, before)
	}
	if title, _, _ := strings.Cut(help, "\n"); title == "" || strings.Index(before, sentence) < len(title) {
		t.Errorf("%s: the title line %q does not come before the sentence:\n%s", what, title, before)
	}
}
