package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// normalized is the stdout of a call without the two members that say when it
// ran, so that two calls can be compared.
func normalized(t *testing.T, stdout []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		t.Fatalf("%v:\n%s", err, stdout)
	}
	delete(m, "duration_ms")
	delete(m, "started_at")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS-10-16: every form writes exactly one envelope with tool speca and an exit
// code equal to the process's, and the flags may come on either side of the
// input.
func TestEveryFormWritesOneSpecaEnvelope_TS10_16(t *testing.T) {
	root := newRepo(t)
	for name, argv := range map[string][]string{
		"report":              {"--dir", root, "09"},
		"verdict, flag first": {"--dir", root, "--validate", "09"},
		"verdict, flag last":  {"--dir", root, "09", "--validate"},
		"list":                {"--dir", root, "--list"},
		"list with archive":   {"--dir", root, "--list", "--all"},
		"specs-dir given":     {"--dir", root, "--specs-dir", ".specs", "09"},
	} {
		var stdout, stderr bytes.Buffer
		code := Main(argv, &stdout, &stderr)
		env, err := envelope.DecodeSpeca(stdout.Bytes()) // refuses anything but one object
		if err != nil {
			t.Errorf("%s: stdout is not one envelope: %v\n%s", name, err, stdout.String())
			continue
		}
		if env.Tool != "speca" || code != env.ExitCode || code != cli.ExitOK || env.Result == nil || env.Summary == "" {
			t.Errorf("%s: tool %q, exit %d, envelope says %d, summary %q", name, env.Tool, code, env.ExitCode, env.Summary)
		}
	}

	same := func(a, b []string) {
		t.Helper()
		var out1, out2 bytes.Buffer
		Main(a, &out1, new(bytes.Buffer))
		Main(b, &out2, new(bytes.Buffer))
		if normalized(t, out1.Bytes()) != normalized(t, out2.Bytes()) {
			t.Errorf("%v and %v differ", a, b)
		}
	}
	same([]string{"--dir", root, "--validate", "09"}, []string{"09", "--validate", "--dir", root})
	same([]string{"--dir", root, "--list", "--all"}, []string{"--all", "--list", "--dir", root})
	same([]string{"--dir", root, "09"}, []string{"09", "--dir", root, "--specs-dir", ".specs"})
}

// snapshot records every file and directory under root with its size and
// modification time.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = info.Mode().String() + " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TS-10-22: speca writes nothing, and the credentials and models in the
// environment change nothing it says.
func TestSpecaWritesNothingAndReadsNoCredential_TS10_22(t *testing.T) {
	root := newRepo(t)
	addPackage(t, root, "03_third", "03", "third")
	addPackage(t, root, "archive/05_old", "05", "old")
	forms := map[string][]string{
		"report":  {"--dir", root, "09"},
		"verdict": {"--dir", root, "09", "--validate"},
		"list":    {"--dir", root, "--list", "--all"},
		"schema":  {"--schema"},
	}
	outputs := func() map[string]string {
		out := map[string]string{}
		for name, argv := range forms {
			var stdout bytes.Buffer
			Main(argv, &stdout, new(bytes.Buffer))
			if name == "schema" {
				out[name] = stdout.String()
			} else {
				out[name] = normalized(t, stdout.Bytes())
			}
		}
		return out
	}

	unset := outputs()
	before := snapshot(t, root)
	for _, name := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_VERTEX_PROJECT_ID",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION",
		"GOOGLE_APPLICATION_CREDENTIALS", "OPENAI_API_KEY", "GITHUB_TOKEN", "GITLAB_TOKEN",
	} {
		t.Setenv(name, "invalid-"+strings.ToLower(name))
	}
	set := outputs()

	if after := snapshot(t, root); !maps_equal(before, after) {
		t.Errorf("the file tree changed:\nbefore %v\nafter  %v", before, after)
	}
	for name := range forms {
		if unset[name] != set[name] {
			t.Errorf("%s: the output depends on the environment", name)
		}
	}
}

func maps_equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// The code of the command writes no file, reads no variable, runs no program
// and reaches no network. The test above shows what a call does; this shows
// the command has nothing that could.
func TestSpecaHasNoCodeThatWritesOrReachesOut_TS10_22(t *testing.T) {
	forbiddenImports := []string{"net", "net/http", "os/exec"}
	forbiddenOS := []string{"Create", "CreateTemp", "OpenFile", "WriteFile", "Mkdir", "MkdirAll", "MkdirTemp", "Rename",
		"Remove", "RemoveAll", "Truncate", "Chmod", "Chtimes", "Symlink", "Link", "Getenv", "LookupEnv", "Environ", "Setenv"}
	forbiddenMethods := []string{"Save", "Transition", "Supersede", "MoveToArchive", "CreateSpec", "WriteAtomic", "EmitWithOutput"}

	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("sources: %v, %v", files, err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); slices.Contains(forbiddenImports, path) {
				t.Errorf("%s imports %s", file, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && slices.Contains(forbiddenOS, sel.Sel.Name) {
				t.Errorf("%s calls os.%s", file, sel.Sel.Name)
			}
			if slices.Contains(forbiddenMethods, sel.Sel.Name) {
				t.Errorf("%s uses %s", file, sel.Sel.Name)
			}
			return true
		})
	}
}
