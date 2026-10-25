package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// -h, --help and a bare invocation print the help to stderr: the sentence
// naming the sibling commands between the title and Usage:, the forms, and a
// paragraph saying that next names the task to render after this one and the
// speca call that claims or completes the rendered task. Only a bare
// invocation into a pipe also writes a usage envelope.
func TestHelp_NamesTheSiblingsTheFormsAndWhatNextSuggests(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		code int
	}{
		{"-h", []string{"-h"}, cli.ExitOK},
		{"--help", []string{"--help"}, cli.ExitOK},
		{"bare invocation", nil, cli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Main(tc.argv, &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("code %d, want %d", code, tc.code)
			}
			if tc.code == cli.ExitOK && stdout.Len() != 0 {
				t.Errorf("help wrote to stdout: %q", stdout.String())
			}
			if tc.code == cli.ExitUsage {
				env, err := envelope.DecodeSpecr(stdout.Bytes())
				if err != nil || env.Error == nil || env.Error.Category != "usage" || env.Error.Message != noInputMessage {
					t.Errorf("envelope %+v (%v)", env.Error, err)
				}
			}
			help := stderr.String()
			siblingLine(t, tc.name, help)
			if !strings.Contains(help, "specr <spec> --task N") {
				t.Errorf("the help lost the form %q", "specr <spec> --task N")
			}
			var para string
			for _, p := range strings.Split(help, "\n\n") {
				if flat := strings.Join(strings.Fields(p), " "); strings.Contains(flat, "next names") {
					para = flat
				}
			}
			for _, want := range []string{"the task to render after this one", "speca", "claims the rendered task", "records it done", "can take the change"} {
				if !strings.Contains(para, want) {
					t.Errorf("the paragraph on next does not say %q:\n%s", want, para)
				}
			}
		})
	}
}

func TestVersion_PrintsTheBuild(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--version"}, &stdout, &stderr); code != cli.ExitOK || !strings.HasPrefix(stdout.String(), "specr ") {
		t.Errorf("--version: %d %q", code, stdout.String())
	}
}

// A flag value that cannot hold, or an input that is not one reference, is a
// usage error before anything is read: exit 2, no result, no next, and the
// reason on stderr too.
func TestUsageErrors_AreRefusedBeforeAnythingIsRead(t *testing.T) {
	root := newRepo(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		argv []string
		says string
	}{
		{"a negative task", []string{"1", "--task", "-2"}, "--task must be a task id"},
		{"a negative budget", []string{"1", "--max-tokens", "-1"}, "--max-tokens cannot be negative"},
		{"a budget with --section", []string{"1", "--max-tokens", "10", "--section", "prd"}, "cannot be combined"},
		{"an unknown section", []string{"9", "--section", "intro"}, `unknown section "intro"`},
		{"--markdown-file on stdout", []string{"9", "--markdown-file", "-"}, "--markdown-file cannot be -"},
		{"--markdown-file a directory", []string{"9", "--markdown-file", t.TempDir()}, "is a directory"},
		{"two inputs", []string{"9", "10"}, "expected one input, got 2"},
		{"--dir that is not a directory", []string{"9", "--dir", file}, "is not a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, code, stderr := run(t, append([]string{"--dir", root}, tc.argv...)...)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Category != "usage" || env.Result != nil || len(env.Next) != 0 {
				t.Fatalf("code %d: %+v, next %+v", code, env.Error, env.Next)
			}
			if !strings.Contains(env.Error.Message, tc.says) {
				t.Errorf("message %q does not say %q", env.Error.Message, tc.says)
			}
			if !strings.Contains(stderr, tc.says) {
				t.Errorf("stderr does not say %q: %s", tc.says, stderr)
			}
		})
	}
}

// Every shape of reference names the same package.
func TestReference_EveryFormNamesThePackage(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	for _, ref := range []string{
		filepath.Join(root, ".specs", example),
		filepath.Join(".specs", example),
		filepath.Join(".specs", example, "tasks.json"),
		example, "09", "9", "agent_mode",
	} {
		t.Run(ref, func(t *testing.T) {
			env, code, _ := run(t, ref)
			if code != cli.ExitOK || env.Result.SpecDir != filepath.Join(".specs", example) {
				t.Errorf("code %d, %+v", code, env.Error)
			}
		})
	}
}

// A reference that names no package, or two, is a usage error: the message
// lists what is there, and points at an archived package that matches. There
// is no result and no next.
func TestReference_ThatNamesNoOnePackageIsAUsageError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
		ref   string
		says  []string
	}{
		{"an unknown name", func(t *testing.T, root string) {
			copyDir(t, filepath.Join(root, ".specs", example), filepath.Join(root, ".specs", "archive", "07_old_thing"))
		}, "old_thing", []string{"01_test_feature, 09_agent_mode", filepath.Join(".specs", "archive", "07_old_thing")}},
		{"an unknown number", nil, "99_missing", nil},
		{"two packages with one number", func(t *testing.T, root string) {
			copyDir(t, filepath.Join(root, ".specs", example), filepath.Join(root, ".specs", "09_other"))
		}, "9", []string{"09_agent_mode, 09_other"}},
		{"stdin", nil, "-", nil},
		{"an issue URL", nil, "https://github.com/acme/widgets/issues/1", nil},
		{"the spec root itself", nil, ".specs", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			if tc.setup != nil {
				tc.setup(t, root)
			}
			env, code, stderr := run(t, "--dir", root, tc.ref)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Category != "usage" || env.Result != nil || len(env.Next) != 0 {
				t.Fatalf("code %d: %+v, next %+v", code, env.Error, env.Next)
			}
			for _, want := range tc.says {
				if !strings.Contains(env.Error.Message, want) {
					t.Errorf("message lacks %q: %s", want, env.Error.Message)
				}
			}
			if !strings.Contains(stderr, env.Error.Message) {
				t.Errorf("stderr lacks the message: %s", stderr)
			}
		})
	}
}

// A package that cannot be read fails at stage load, with no result and no
// next.
func TestLoad_APackageThatCannotBeReadFails(t *testing.T) {
	root := newRepo(t)
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "malformed_json"), filepath.Join(root, ".specs", "05_broken"))
	env, code, _ := run(t, "--dir", root, "5")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" ||
		env.Result != nil || len(env.Next) != 0 {
		t.Errorf("code %d: %+v, next %+v", code, env.Error, env.Next)
	}
}

// --task naming a task the package does not have, or a package in an older
// format whose tasks cannot be read, is a usage error that says why.
func TestTask_ThatCannotBeScopedToIsAUsageError(t *testing.T) {
	emptyPlan := func(t *testing.T, root string) {
		tasks := filepath.Join(root, ".specs", example, "tasks.json")
		b, err := os.ReadFile(tasks)
		if err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(b, []byte(`"tasks": [`))
		if i < 0 {
			t.Fatal("no tasks array in the fixture")
		}
		if err := os.WriteFile(tasks, append(b[:i:i], []byte(`"tasks": []}`)...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
		argv  []string
		says  []string
		ends  string
		hint  []string
	}{
		{"an unknown task lists the ids", nil, []string{"9", "--task", "7"}, nil, "has no task 7; its tasks are 1, 2, 3", []string{"1", "2", "3"}},
		{"a plan with no tasks says so", emptyPlan, []string{"9", "--task", "1"}, nil, "has no task 1; it has no tasks", nil},
		{"a package in an older format names the version", newLegacyPackage, []string{"7", "--task", "1"},
			[]string{"format version 1", "§13"}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			if tc.setup != nil {
				tc.setup(t, root)
			}
			env, code, stderr := run(t, append([]string{"--dir", root}, tc.argv...)...)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" {
				t.Fatalf("code %d: %+v", code, env.Error)
			}
			for _, want := range tc.says {
				if !strings.Contains(env.Error.Message, want) || !strings.Contains(stderr, want) {
					t.Errorf("message or stderr lacks %q: %s", want, env.Error.Message)
				}
			}
			if !strings.HasSuffix(env.Error.Message, tc.ends) || !strings.Contains(stderr, tc.ends) {
				t.Errorf("message %q does not end with %q", env.Error.Message, tc.ends)
			}
			if tc.hint == nil {
				if env.Error.FixHint != nil {
					t.Errorf("fix hint %+v, want none", env.Error.FixHint)
				}
			} else if h := env.Error.FixHint; h == nil || h.Flag != "--task" || !slices.Equal(h.Valid, tc.hint) {
				t.Errorf("fix hint %+v, want --task %v", h, tc.hint)
			}
		})
	}
}

// A package that reads but does not validate is rendered as it is, with a
// warning: low for a v2 package that breaks a rule, high for a package in an
// older format whose tests and tasks were not read.
func TestValidation_APackageThatDoesNotValidateRendersWithAWarning(t *testing.T) {
	brokenRule := func(t *testing.T, root string) {
		tasks := filepath.Join(root, ".specs", example, "tasks.json")
		b, err := os.ReadFile(tasks)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tasks, bytes.Replace(b, []byte(`"TS-09-6"`), []byte(`"TS-09-99"`), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, root string)
		ref      string
		severity string
		says     string
		tasks    int
	}{
		{"a broken rule", brokenRule, "9", "low", "does not validate", 3},
		{"an older format", newLegacyPackage, "7", "high", "format version 1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, root)
			env, code, _ := run(t, "--dir", root, tc.ref)
			if code != cli.ExitOK || env.Result.Valid || len(env.Result.Tasks) != tc.tasks {
				t.Fatalf("code %d valid %v tasks %+v", code, env.Result.Valid, env.Result.Tasks)
			}
			if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSpecInvalid || env.Warnings[0].Severity != tc.severity ||
				!strings.Contains(env.Warnings[0].Message, tc.says) {
				t.Errorf("warnings %+v", env.Warnings)
			}
			if tc.severity == "high" && !strings.Contains(env.Summary, "1 high-severity warning") {
				t.Errorf("summary %q", env.Summary)
			}
		})
	}
}

// Every task of a render carries ready, true for a pending task whose
// dependencies are done, and so does the task rendered.
func TestTasks_SayWhichAreReady(t *testing.T) {
	ready := func(tasks []envelope.TaskInfo) []bool {
		var out []bool
		for _, task := range tasks {
			out = append(out, task.Ready)
		}
		return out
	}
	// In the example task 2 depends on 1, and task 3 on 1 and 2.
	root := newRepo(t)
	env, code, _ := run(t, "--dir", root, "09")
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	if got := ready(env.Result.Tasks); !slices.Equal(got, []bool{true, false, false}) {
		t.Errorf("ready = %v; want only task 1", got)
	}

	plan(t, filepath.Join(root, ".specs", example), []string{"done", "pending", "pending"}, nil)
	env, _, _ = run(t, "--dir", root, "09", "--task", "2")
	if got := ready(env.Result.Tasks); !slices.Equal(got, []bool{false, true, false}) {
		t.Errorf("with task 1 done, ready = %v; want only task 2", got)
	}
	if env.Result.Tasks[0].State != "done" {
		t.Errorf("tasks[0] = %+v", env.Result.Tasks[0])
	}
	if env.Result.Task == nil || env.Result.Task.ID != 2 || !env.Result.Task.Ready {
		t.Errorf("result.task = %+v; want task 2, ready", env.Result.Task)
	}
}

// specr keeps whether the package can take a write, which decides whether next
// offers a call of speca, and writes it nowhere in its output.
func TestWritability_IsKeptButNotWritten(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"draft", true},
		{"active", true},
		{"drifted", false},
		{"sealed", false},
		{"superseded", false},
		{"archived", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			repo := newRepo(t)
			prepareStatus(t, repo, tc.status)
			code, res, failure := execute("09", &options{dir: repo}, cli.NewRun(tool, "test"))
			if code != cli.ExitOK || failure != nil || res.writable != tc.want {
				t.Errorf("code %d, failure %+v, writable %v; want %v", code, failure, res != nil && res.writable, tc.want)
			}
			var stdout, stderr bytes.Buffer
			Main([]string{"--dir", repo, "09"}, &stdout, &stderr)
			for _, key := range []string{"writable", "intent_drift"} {
				if strings.Contains(stdout.String(), key) {
					t.Errorf("the output has a %q key", key)
				}
			}
		})
	}
}

// --markdown-file writes exactly result.markdown and lists the file. A file
// that cannot be written fails the call at stage write, with the result and
// the next a render that succeeded would have.
func TestMarkdownFile_WritesTheRender(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(t.TempDir(), "out", "task.md")
	env, code, _ := run(t, "--dir", root, "9", "--task", "1", "--markdown-file", path)
	if code != cli.ExitOK {
		t.Fatalf("code %d: %+v", code, env.Error)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != env.Result.Markdown || env.Result.MarkdownFile != path {
		t.Fatalf("file %q (%v), result %q", path, err, env.Result.MarkdownFile)
	}
	if len(env.Artifacts) != 1 || env.Artifacts[0].Kind != envelope.ArtifactMarkdownFile || env.Artifacts[0].Path != path {
		t.Errorf("artifacts = %+v", env.Artifacts)
	}

	good, _, _ := run(t, "--dir", root, "9")
	blocked := filepath.Join(t.TempDir(), "file") // a file where the Markdown's directory would be
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bad, code, _ := run(t, "--dir", root, "9", "--markdown-file", filepath.Join(blocked, "x.md"))
	if code != cli.ExitFailed || bad.Error == nil || bad.Error.Stage != "write" || bad.Error.Category != "disk" || bad.Result == nil {
		t.Fatalf("code %d: %+v, result %v", code, bad.Error, bad.Result != nil)
	}
	if !slices.EqualFunc(bad.Next, good.Next, func(a, b envelope.Next) bool {
		return a.Tool == b.Tool && a.Input == b.Input && a.Why == b.Why && slices.Equal(a.Flags, b.Flags)
	}) || len(byTool(bad.Next, "speca")) != 1 {
		t.Errorf("next = %+v after a failed write; %+v after a good one", bad.Next, good.Next)
	}
}

// The Markdown reaches stdout unescaped: a model reading the raw JSON sees
// <, > and & as they are, not as unicode escapes.
func TestMarkdown_IsNotHTMLEscaped(t *testing.T) {
	root := newRepo(t)
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--dir", root, "1"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stdout.String(), "&LoadError") {
		t.Errorf("the test's pseudocode lost its & on stdout")
	}
	for _, esc := range []string{"3c", "3e", "26"} {
		if strings.Contains(stdout.String(), "\\u00"+esc) {
			t.Errorf("stdout escapes HTML (u00%s)", esc)
		}
	}
}

// No render changes a file of the package, not even one whose next offers a
// call of speca that would.
func TestRender_NeverChangesThePackage(t *testing.T) {
	for _, states := range [][]string{{"pending", "pending", "pending"}, {"in_progress", "pending", "pending"}} {
		root := exampleWith(t, states, nil)
		dir := filepath.Join(root, ".specs", example)
		before := treeOf(t, dir)
		for _, argv := range [][]string{{"09"}, {"09", "--task", "1"}, {"09", "--section", "tasks"}} {
			env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
			// A task render offers a call whichever way task 1 stands; a whole render only
			// when nothing is in progress. Without the call the case would prove less.
			wantCall := slices.Contains(argv, "--task") || states[0] == "pending"
			if got := len(byTool(env.Next, "speca")) > 0; code != cli.ExitOK || got != wantCall {
				t.Fatalf("%v with %v: code %d, next %+v; want a call of speca: %v", argv, states, code, env.Next, wantCall)
			}
		}
		after := treeOf(t, dir)
		if len(before) != len(after) {
			t.Errorf("files before %d, after %d", len(before), len(after))
		}
		for name, was := range before {
			if after[name] != was {
				t.Errorf("%s changed (bytes or modification time)", name)
			}
		}
	}
}
