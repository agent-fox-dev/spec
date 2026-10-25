package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

func TestHelp_GoesToStderrAndDescribesEveryForm(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Main([]string{flag}, &stdout, &stderr); code != cli.ExitOK || stdout.Len() != 0 {
				t.Fatalf("code %d, stdout %q", code, stdout.String())
			}
			siblingLine(t, flag, stderr.String())
			help := strings.Join(strings.Fields(stderr.String()), " ") // the help wraps its lines
			for _, want := range []string{
				"speca [flags] <spec>", "speca --list", "speca <spec> --validate", "Examples:", "Exit codes:",
				"-specs-dir", "-validate", "-list",
				// The task form.
				"speca <spec> --task T --state S", "--task", "--state", "--force", "--dry-run",
				"next", "all", "pending, in_progress, done or dropped", "state machine", "§8.3.1",
				"task_state_forced", "writes nothing",
				// The status form.
				"speca <spec> --status S", "--by", "active, sealed, archived or superseded", "archive/", "intent hash",
				"tasks_open", "supersedes_not_recorded", "resum",
			} {
				if !strings.Contains(help, want) {
					t.Errorf("the help does not say %q", want)
				}
			}
		})
	}
}

// A call with no input prints the help, exits 2 and, into a pipe, also writes a
// usage envelope, so a program piping stdout still gets one object to parse.
func TestBareInvocation_PrintsTheHelpAndAUsageEnvelope(t *testing.T) {
	root := newRepo(t)
	for name, argv := range map[string][]string{
		"nothing":         nil,
		"--validate":      {"--validate"},
		"--all":           {"--all"},
		"the status form": {"--status", "sealed"},
		"the task form":   {"--task", "1", "--state", "done"},
	} {
		t.Run(name, func(t *testing.T) {
			before := snapshot(t, root)
			env, code, stderr := run(t, append([]string{"--dir", root}, argv...)...)
			if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
				t.Fatalf("code %d, %+v", code, env.Error)
			}
			if env.Tool != "speca" || env.OK || !strings.Contains(env.Error.Message, "no input") {
				t.Errorf("envelope %+v", env)
			}
			siblingLine(t, name, stderr)
			if !maps.Equal(before, snapshot(t, root)) {
				t.Error("the repository changed")
			}
		})
	}
}

func TestVersion_PrintsTheBuildIdentity(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--version"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("code %d", code)
	}
	if want := "speca " + spec.Identity() + "\n"; stdout.String() != want {
		t.Errorf("--version = %q; want %q", stdout.String(), want)
	}
}

// Every call that cannot hold is refused before anything is written, as a usage
// error with the message on stderr and no result.
func TestUsageErrors(t *testing.T) {
	root := newRepo(t)
	addPackage(t, root, "09_twin", "09", "twin") // "09" and "9" now name two packages
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	statusHint := &cli.FixHint{Flag: "--status", Valid: wantStatusTargets}
	const in = "09_agent_mode"

	for name, tc := range map[string]struct {
		argv []string
		want []string
		hint *cli.FixHint
	}{
		// The input.
		"two inputs":              {[]string{"01", "07"}, nil, nil},
		"stdin":                   {[]string{"-"}, nil, nil},
		"a URL":                   {[]string{"https://github.com/acme/widgets/issues/1"}, nil, nil},
		"several lines":           {[]string{"01\n02"}, nil, nil},
		"--dir is a file":         {[]string{"--dir", file, "01"}, []string{"not a directory"}, nil},
		"a reference naming none": {[]string{"99"}, nil, nil},
		"an ambiguous reference":  {[]string{"9"}, nil, nil},
		"an undefined flag":       {[]string{"--bogus", "01"}, nil, nil},
		"two inputs, task form":   {[]string{in, "01", "--task", "1", "--state", "done"}, nil, nil},

		// The report, the verdict and the list.
		"input with --list":  {[]string{"--list", "01"}, []string{"--list takes no input"}, nil},
		"--validate --list":  {[]string{"--validate", "--list"}, []string{"two operations"}, nil},
		"--all without list": {[]string{"--all", "01"}, []string{"--all"}, nil},
		"--force alone":      {[]string{in, "--force"}, []string{"--force", "--task"}, nil},
		"--dry-run alone":    {[]string{in, "--dry-run"}, []string{"--dry-run", "--task"}, nil},
		"--validate --force": {[]string{in, "--validate", "--force"}, []string{"--force"}, nil},
		"--list --dry-run":   {[]string{"--list", "--dry-run"}, []string{"--dry-run"}, nil},

		// The task form.
		"--task without --state": {[]string{in, "--task", "1"}, []string{"--task", "--state"}, nil},
		"--state without --task": {[]string{in, "--state", "done"}, []string{"--state", "--task"}, nil},
		"a state outside the four": {[]string{in, "--task", "1", "--state", "finished"}, []string{`"finished"`, "pending, in_progress, done, dropped"},
			&cli.FixHint{Flag: "--state", Valid: wantTaskStates}},
		"--task 0":                {[]string{in, "--task", "0", "--state", "done"}, []string{"--task", `"0"`}, nil},
		"--task -1":               {[]string{in, "--task", "-1", "--state", "done"}, []string{"--task", `"-1"`}, nil},
		"--task abc":              {[]string{in, "--task", "abc", "--state", "done"}, []string{"--task", `"abc"`}, nil},
		"--task 1,,2":             {[]string{in, "--task", "1,,2", "--state", "done"}, []string{"--task", `"1,,2"`}, nil},
		"--task next,1":           {[]string{in, "--task", "next,1", "--state", "done"}, []string{"--task", `"next,1"`}, nil},
		"--task all,2":            {[]string{in, "--task", "all,2", "--state", "done"}, []string{"--task", `"all,2"`}, nil},
		"--task next,all":         {[]string{in, "--task", "next,all", "--state", "done"}, []string{"--task", `"next,all"`}, nil},
		"--task next, repeated":   {[]string{in, "--task", "next", "--task", "1", "--state", "done"}, []string{"--task", "next"}, nil},
		"the task form, validate": {[]string{in, "--task", "1", "--state", "in_progress", "--validate"}, []string{"two operations", "--validate"}, nil},
		"the task form, list":     {[]string{"--list", "--task", "1", "--state", "done"}, []string{"two operations", "--list"}, nil},

		// The status form.
		"--status draft": {[]string{in, "--status", "draft"}, []string{"no move leads back to draft", "superseded by a new one"}, statusHint},
		"--status bogus": {[]string{in, "--status", "bogus"}, []string{"--status", "active, sealed, superseded and archived"}, statusHint},
		// A status is case-sensitive.
		"--status Active": {[]string{in, "--status", "Active"}, []string{"--status", "active, sealed, superseded and archived"}, statusHint},
		// An empty status must not fall through to the report.
		"--status empty":            {[]string{in, "--status", ""}, []string{"--status", "active, sealed, superseded and archived"}, statusHint},
		"superseded without --by":   {[]string{in, "--status", "superseded"}, []string{"--status superseded", "--by"}, nil},
		"--by without --status":     {[]string{in, "--by", "01"}, []string{"--by", "--status superseded"}, nil},
		"--by with --status sealed": {[]string{in, "--status", "sealed", "--by", "01"}, []string{"--by", "--status superseded"}, nil},
		"an empty --by":             {[]string{in, "--status", "superseded", "--by", ""}, []string{"--by"}, nil},
		"the status form, task form": {[]string{in, "--status", "sealed", "--task", "1", "--state", "done"},
			[]string{"--status", "--task", "two operations"}, nil},
		"the status form, validate": {[]string{in, "--status", "sealed", "--validate"}, []string{"--status", "--validate", "two operations"}, nil},
		"the status form, list":     {[]string{"--status", "sealed", "--list"}, []string{"--status", "--list", "two operations"}, nil},
		"--by names no package":     {[]string{in, "--status", "superseded", "--by", "nonexistent"}, []string{"--by:"}, nil},
		"--by names the package":    {[]string{in, "--status", "superseded", "--by", in}, []string{"--by", "itself"}, nil},
		"--by names a file of the package": {[]string{in, "--status", "superseded", "--by", ".specs/09_agent_mode/prd.md"},
			[]string{"--by", "itself"}, nil},
		"--by has the same spec_id": {[]string{in, "--status", "superseded", "--by", "09_twin"}, []string{"--by", "spec_id 09"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			e := refusedAsUsage(t, root, append([]string{"--dir", root}, tc.argv...), tc.want...)
			// A refusal of the resolver of --by is told apart from one of the input.
			if e != nil && slices.Contains(tc.want, "--by:") && !strings.HasPrefix(e.Message, "--by:") {
				t.Errorf("message %q does not begin --by:", e.Message)
			}
			if e == nil || tc.hint == nil {
				return
			}
			if h := e.FixHint; h == nil || h.Flag != tc.hint.Flag || !slices.Equal(h.Valid, tc.hint.Valid) {
				t.Errorf("fix_hint = %+v; want %+v", h, tc.hint)
			}
		})
	}
}

// The usage errors of one call are all named, in the same order every time.
func TestUsageErrors_AreInAFixedOrder(t *testing.T) {
	for _, argv := range [][]string{
		{"09", "--force", "--dry-run"},
		{"09", "--validate", "--list", "--task", "1", "--state", "done"},
	} {
		var first string
		for i := range 12 {
			env, _, _ := run(t, argv...)
			if env.Error == nil {
				t.Fatalf("%v: no error", argv)
			}
			if i == 0 {
				first = env.Error.Message
			} else if env.Error.Message != first {
				t.Fatalf("%v: the message changed between runs:\n%q\n%q", argv, first, env.Error.Message)
			}
		}
		if argv[1] == "--force" && strings.Index(first, "--force") > strings.Index(first, "--dry-run") {
			t.Errorf("message %q names --dry-run before --force", first)
		}
	}
}

// A call that is well-formed is not a usage error: it reaches the package.
func TestUsageErrors_AcceptEveryWellFormedTaskCall(t *testing.T) {
	root := newRepo(t)
	for name, flags := range map[string][]string{
		"an id":         {"09", "--task", "1", "--state", "in_progress"},
		"ids, repeated": {"09", "--task", "1,2", "--task", "2", "--state", "in_progress"},
		"next":          {"09", "--task", "next", "--state", "in_progress"},
		"all":           {"09", "--task", "all", "--state", "done"},
		"flags after":   {"--task", "1", "--state", "in_progress", "--force", "09"},
		"pending":       {"09", "--task", "1", "--state", "pending"},
		"dropped":       {"09", "--task", "1", "--state", "dropped"},
	} {
		env, _, _ := run(t, append([]string{"--dir", root, "--dry-run"}, flags...)...)
		if env.Error != nil && env.Error.Stage == "usage" {
			t.Errorf("%s: refused as a usage error: %s", name, env.Error.Message)
		}
	}
}

// Every form writes exactly one envelope with tool speca and an exit code equal
// to the process's, and the flags may come on either side of the input.
func TestEveryFormWritesOneSpecaEnvelope(t *testing.T) {
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

// The report, the verdict, the list and --schema write nothing and reach no
// network, and the credentials and models in the environment change nothing
// they say.
func TestReadingFormsWriteNothingAndReadNoCredential(t *testing.T) {
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

	if after := snapshot(t, root); !maps.Equal(before, after) {
		t.Errorf("the file tree changed:\nbefore %v\nafter  %v", before, after)
	}
	for name := range forms {
		if unset[name] != set[name] {
			t.Errorf("%s: the output depends on the environment", name)
		}
	}
}
