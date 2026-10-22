package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// siblingSentence is the sentence the help and --schema each carry, written out
// here so that changing it is a change to a test: what the three commands are,
// so that an agent that finds one knows the other two.
const siblingSentence = "spec writes a package from an idea, specr renders one, speca reports its state"

// newRepo lays out a repository with two packages under .specs: the v2
// example as 09_agent_mode, and the fixture with an architecture.md as
// 01_test_feature. It returns the repository root.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "v2_example"), filepath.Join(root, ".specs", "09_agent_mode"))
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "valid_spec_with_arch"), filepath.Join(root, ".specs", "01_test_feature"))
	return root
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// run runs speca and decodes the one JSON object it printed with the public
// decoder, so the tests read the output the way a program would.
func run(t *testing.T, argv ...string) (envelope.AdminEnvelope, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(argv, &stdout, &stderr)
	env, err := envelope.DecodeSpeca(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope (%v):\n%s", err, stdout.String())
	}
	if code != env.ExitCode {
		t.Fatalf("exit code %d, envelope says %d", code, env.ExitCode)
	}
	return env, code, stderr.String()
}

// TS-10-17: -h and --help print the help to stderr and nothing to stdout; the
// help names the forms and examples, and near the top, the three commands.
func TestHelpGoesToStderr_TS10_17(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		var stdout, stderr bytes.Buffer
		if code := Main([]string{flag}, &stdout, &stderr); code != cli.ExitOK || stdout.Len() != 0 {
			t.Errorf("%s: code %d, stdout %q", flag, code, stdout.String())
		}
		help := stderr.String()
		for _, want := range []string{"speca [flags] <spec>", "speca --list", "speca <spec> --validate", "Examples:", "-specs-dir", "-validate", "-list", "Exit codes:"} {
			if !strings.Contains(help, want) {
				t.Errorf("%s: the help lacks %q", flag, want)
			}
		}
		lines := strings.Split(help, "\n")
		top := strings.Join(lines[:min(5, len(lines))], "\n")
		if !strings.Contains(top, siblingSentence) {
			t.Errorf("%s: the sentence naming the three commands is not near the top:\n%s", flag, help)
		}
	}
}

// TS-10-18: a bare invocation prints the help, exits 2 and, into a pipe, also
// writes a usage envelope. So does one that names flags but no input.
func TestBareInvocation_TS10_18(t *testing.T) {
	for _, argv := range [][]string{nil, {"--validate"}, {"--all"}} {
		env, code, stderr := run(t, argv...)
		if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
			t.Errorf("%v: code %d, %+v", argv, code, env.Error)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("%v: stderr lacks the help: %q", argv, stderr)
		}
		if env.Tool != "speca" || env.OK || env.ExitCode != cli.ExitUsage {
			t.Errorf("%v: envelope %+v", argv, env)
		}
	}
}

// TS-10-19: --version prints the tool and the build, and is not an envelope.
func TestVersion_TS10_19(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--version"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	if want := "speca " + spec.Identity() + "\n"; stdout.String() != want {
		t.Errorf("--version = %q; want %q", stdout.String(), want)
	}
}

// TS-10-21: every malformed invocation is refused before a package is read,
// as a usage error with the message on stderr and no result.
func TestMalformedInvocationsAreUsageErrors_TS10_21(t *testing.T) {
	root := newRepo(t)
	copyDir(t, filepath.Join(root, ".specs", "09_agent_mode"), filepath.Join(root, ".specs", "09_other"))
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := []string{"--dir", root}

	for name, argv := range map[string][]string{
		"two inputs":              append([]string{"01", "07"}, dir...),
		"input with --list":       append([]string{"--list", "01"}, dir...),
		"--validate --list":       append([]string{"--validate", "--list"}, dir...),
		"--all without list":      append([]string{"--all", "01"}, dir...),
		"stdin":                   append([]string{"-"}, dir...),
		"a URL":                   append([]string{"https://github.com/acme/widgets/issues/1"}, dir...),
		"several lines":           append([]string{"01\n02"}, dir...),
		"--dir is a file":         {"--dir", file, "01"},
		"a reference naming none": append([]string{"99"}, dir...),
		"an ambiguous reference":  append([]string{"9"}, dir...),
		"an undefined flag":       append([]string{"--bogus", "01"}, dir...),
	} {
		env, code, stderr := run(t, argv...)
		if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" {
			t.Errorf("%s: code %d, error %+v", name, code, env.Error)
			continue
		}
		if env.Result != nil || env.OK || env.Error.Retryable || env.Error.Resumable {
			t.Errorf("%s: result %+v, ok %v, retryable %v, resumable %v", name, env.Result, env.OK, env.Error.Retryable, env.Error.Resumable)
		}
		if !strings.Contains(stderr, env.Error.Message) || env.Error.Message == "" {
			t.Errorf("%s: stderr %q lacks the message %q", name, stderr, env.Error.Message)
		}
	}
}
