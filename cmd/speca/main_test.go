package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"net/http"
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

// TS-11-52: the help describes the task form and the flags that belong to it.
func TestHelpDescribesTheTaskForm_TS11_52(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--help"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	help := stderr.String()
	for _, want := range []string{
		"speca <spec> --task T --state S", "--task", "--state", "--force", "--dry-run",
		"next", "all", "pending, in_progress, done or dropped", "state machine", "§8.3.1",
		"task_state_forced", "writes nothing",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("the help does not say %q", want)
		}
	}
	// The help no longer says the command changes nothing, which the task form does.
	for _, stale := range []string{"speca changes nothing", "It changes nothing"} {
		if strings.Contains(help, stale) {
			t.Errorf("the help still says %q", stale)
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

	// Nothing may leave the process: a request through the default transport or
	// a lookup through the default resolver is a network attempt.
	refuse := func(what string) error {
		t.Errorf("speca attempted a network connection: %s", what)
		return errors.New("the network is not available to speca")
	}
	wasTransport, wasResolver := http.DefaultTransport, net.DefaultResolver
	t.Cleanup(func() { http.DefaultTransport, net.DefaultResolver = wasTransport, wasResolver })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, refuse(r.Method + " " + r.URL.String()) })
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(_ context.Context, network, address string) (net.Conn, error) {
		return nil, refuse(network + " " + address)
	}}

	before := snapshot(t, root) // before the first call, so that a file the first call writes is seen
	unset := outputs()
	for _, name := range []string{
		cli.ModelEnv, cli.ModelEnvFallback, cli.EffortEnv, // the model the shell flags read
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_VERTEX_PROJECT_ID",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION",
		"GOOGLE_APPLICATION_CREDENTIALS", "OPENAI_API_KEY", "GITHUB_TOKEN", "GITLAB_TOKEN",
	} {
		t.Setenv(name, "invalid-"+strings.ToLower(name))
	}
	set := outputs()

	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Errorf("the file tree changed:\nbefore %v\nafter  %v", before, after)
	}
	for name := range forms {
		if unset[name] != set[name] {
			t.Errorf("%s: the output depends on the environment", name)
		}
	}
}

// roundTripFunc is an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mapsEqual(a, b map[string]string) bool {
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
// the command has nothing that could. The one exception is the task form, which
// writes the package it changes through the library: task.go may use Save, and
// nothing else of this list, so the report, the verdict and the list stay as
// they were.
func TestSpecaHasNoCodeThatWritesOrReachesOut_TS10_22(t *testing.T) {
	forbiddenImports := []string{"net", "net/http", "os/exec"}
	forbiddenOS := []string{"Create", "CreateTemp", "OpenFile", "WriteFile", "Mkdir", "MkdirAll", "MkdirTemp", "Rename",
		"Remove", "RemoveAll", "Truncate", "Chmod", "Chtimes", "Symlink", "Link", "Getenv", "LookupEnv", "Environ", "Setenv"}
	forbiddenMethods := []string{"Save", "Transition", "Supersede", "MoveToArchive", "CreateSpec", "WriteAtomic", "EmitWithOutput"}
	allowedMethods := map[string][]string{"task.go": {"Save"}, "status.go": {"Transition"}, "status_supersede.go": {"Supersede", "Save"}}

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
			if slices.Contains(forbiddenMethods, sel.Sel.Name) && !slices.Contains(allowedMethods[file], sel.Sel.Name) {
				t.Errorf("%s uses %s", file, sel.Sel.Name)
			}
			return true
		})
	}
}

// The four statuses the status form moves a package to, in the order of the
// lifecycle, written out so that changing them is a change to a test.
var statusTargetsForTests = []string{"active", "sealed", "superseded", "archived"}

// TS-12-10: --status draft is a usage error that says why.
func TestStatusDraftIsAUsageError_TS12_10(t *testing.T) {
	root := newRepo(t)
	e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--status", "draft"},
		"no move leads back to draft", "superseded by a new one")
	if e == nil {
		return
	}
	if h := e.FixHint; h == nil || h.Flag != "--status" || !slices.Equal(h.Valid, statusTargetsForTests) {
		t.Errorf("fix_hint = %+v; want --status with %v", h, statusTargetsForTests)
	}
}

// TS-12-11: a --status that is not one of the four targets is a usage error that
// lists them. So is an empty one, which must not fall through to the report.
func TestAnUnknownStatusIsAUsageError_TS12_11(t *testing.T) {
	for _, value := range []string{"bogus", "Active", ""} {
		root := newRepo(t)
		e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--status", value}, "--status", "active, sealed, superseded and archived")
		if e == nil {
			continue
		}
		if h := e.FixHint; h == nil || h.Flag != "--status" || !slices.Equal(h.Valid, statusTargetsForTests) {
			t.Errorf("--status %q: fix_hint = %+v; want --status with %v", value, h, statusTargetsForTests)
		}
	}
}

// TS-12-12: calls that cannot hold are usage errors that write nothing, each
// saying what is wrong with it.
func TestMalformedStatusCallsAreUsageErrors_TS12_12(t *testing.T) {
	root := newRepo(t)
	for name, tc := range map[string]struct {
		argv []string
		want []string
	}{
		"superseded without --by":   {[]string{"09", "--status", "superseded"}, []string{"--status superseded", "--by"}},
		"--by without --status":     {[]string{"09", "--by", "01"}, []string{"--by", "--status superseded"}},
		"--by with --status sealed": {[]string{"09", "--status", "sealed", "--by", "01"}, []string{"--by", "--status superseded"}},
		"an empty --by":             {[]string{"09", "--status", "superseded", "--by", ""}, []string{"--by"}},
		"with the task form":        {[]string{"09", "--status", "sealed", "--task", "1", "--state", "done"}, []string{"--status", "--task", "two operations"}},
		"with --validate":           {[]string{"09", "--status", "sealed", "--validate"}, []string{"--status", "--validate", "two operations"}},
		"with --list":               {[]string{"--status", "sealed", "--list"}, []string{"--status", "--list", "two operations"}},
	} {
		t.Run(name, func(t *testing.T) {
			refusedAsUsage(t, root, append([]string{"--dir", root}, tc.argv...), tc.want...)
		})
	}

	// With no input the call is a bare invocation: the help goes to stderr, and
	// the usage envelope to stdout.
	t.Run("with no input", func(t *testing.T) {
		before := snapshot(t, root)
		env, code, stderr := run(t, "--dir", root, "--status", "sealed")
		if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
			t.Fatalf("code %d: %+v", code, env.Error)
		}
		if !strings.Contains(env.Error.Message, "no input") || !strings.Contains(stderr, "Usage:") {
			t.Errorf("message %q, stderr %q; want the no-input message and the help", env.Error.Message, stderr)
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Error("the repository changed")
		}
	})
}

// TS-12-13: a --by that cannot be the replacing package is a usage error: one
// the resolver refuses (the message begins --by:), the input itself, and a
// package that has the input's spec_id.
func TestABadByIsAUsageError_TS12_13(t *testing.T) {
	root := newRepo(t)
	addPackage(t, root, "09_twin", "09", "twin")
	e := refusedAsUsage(t, root, []string{"--dir", root, "09_agent_mode", "--status", "superseded", "--by", "nonexistent"}, "--by:")
	if e != nil && !strings.HasPrefix(e.Message, "--by:") {
		t.Errorf("message %q does not begin --by:", e.Message)
	}
	refusedAsUsage(t, root, []string{"--dir", root, "09_agent_mode", "--status", "superseded", "--by", "09_agent_mode"}, "--by", "itself")
	refusedAsUsage(t, root, []string{"--dir", root, "09_agent_mode", "--status", "superseded", "--by", ".specs/09_agent_mode/prd.md"}, "--by", "itself")
	refusedAsUsage(t, root, []string{"--dir", root, "09_agent_mode", "--status", "superseded", "--by", "09_twin"}, "--by", "spec_id 09")
}
