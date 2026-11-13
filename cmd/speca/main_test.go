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
				"next", "all", "pending, in_progress, done or dropped", "state machine", "§8.1",
				"task_state_forced", "writes nothing", "progress.json",
				// The status form.
				"speca <spec> --status S", "--by", "active, sealed, archived or superseded", "archive/", "intent hash",
				"tasks_open", "supersedes_not_recorded", "resum",
				// The render form and the migrate form.
				"speca <spec> --render", "speca --render", "spec.md",
				"speca <spec> --migrate", "speca --migrate", "version 2", "version 3", "test_spec.json", "C1 to C8",
				// The gate form and the gate report.
				"speca <spec> --gate task|full", "--clean-env", "--timeout", "not_run", "tree id", "gate_not_recorded",
				"XDG_STATE_HOME", "speca <spec> --gate-report", "--since", "MERGE_HEAD",
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
	gateHint := &cli.FixHint{Flag: "--gate", Valid: []string{"task", "full"}}
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

		// --next.
		"input with --next": {[]string{"--next", "01"}, []string{"--next takes no input"}, nil},
		"--next --list":     {[]string{"--next", "--list"}, []string{"--next", "--list", "two operations"}, nil},
		"--next --validate": {[]string{"--next", "--validate"}, []string{"--next", "--validate", "two operations"}, nil},
		"--next --render":   {[]string{"--next", "--render"}, []string{"--next", "--render", "two operations"}, nil},
		"--next --migrate":  {[]string{"--next", "--migrate"}, []string{"--next", "--migrate", "two operations"}, nil},
		"--next, task form": {[]string{"--next", "--task", "1", "--state", "done"}, []string{"--next", "--task", "two operations"}, nil},
		"--next, status":    {[]string{"--next", "--status", "sealed"}, []string{"--next", "--status", "two operations"}, nil},
		"--next --all":      {[]string{"--next", "--all"}, []string{"--all"}, nil},
		"--next --dry-run":  {[]string{"--next", "--dry-run"}, []string{"--dry-run"}, nil},

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

		// The render form.
		"--render --validate": {[]string{in, "--render", "--validate"}, []string{"--render", "--validate", "two operations"}, nil},
		"--render --list":     {[]string{"--render", "--list"}, []string{"--render", "--list", "two operations"}, nil},
		"--render, the task form": {[]string{in, "--render", "--task", "1", "--state", "done"},
			[]string{"--render", "--task", "two operations"}, nil},
		"--render, the status form": {[]string{in, "--render", "--status", "active"}, []string{"--render", "--status", "two operations"}, nil},
		"--render --by":             {[]string{in, "--render", "--by", "02"}, []string{"--render", "--by", "two operations"}, nil},
		"--render --force":          {[]string{in, "--render", "--force"}, []string{"--force has no effect with --render"}, nil},
		"--render --all, one package": {[]string{in, "--render", "--all"},
			[]string{"--all adds the archive to the sweep", "means nothing for one package"}, nil},

		// The migrate form.
		"--migrate --validate": {[]string{in, "--migrate", "--validate"}, []string{"--migrate", "--validate", "two operations"}, nil},
		"--migrate --list":     {[]string{"--migrate", "--list"}, []string{"--migrate", "--list", "two operations"}, nil},
		"--migrate, the task form": {[]string{in, "--migrate", "--task", "1", "--state", "done"},
			[]string{"--migrate", "--task", "two operations"}, nil},
		"--migrate, the status form": {[]string{in, "--migrate", "--status", "active"}, []string{"--migrate", "--status", "two operations"}, nil},
		"--migrate --render":         {[]string{in, "--migrate", "--render"}, []string{"--render", "--migrate", "two operations"}, nil},
		"--migrate --force":          {[]string{in, "--migrate", "--force"}, []string{"--force has no effect with --migrate"}, nil},
		"--migrate --all, one package": {[]string{in, "--migrate", "--all"},
			[]string{"--all adds the archive to the sweep", "speca --migrate", "means nothing for one package"}, nil},

		// The gate form. Its level is one of two, case-sensitive.
		"--gate empty":      {[]string{in, "--gate", ""}, []string{"--gate", "task", "full"}, gateHint},
		"--gate all":        {[]string{in, "--gate", "all"}, []string{"--gate", `"all"`, "task", "full"}, gateHint},
		"--gate Full":       {[]string{in, "--gate", "Full"}, []string{"--gate", `"Full"`, "task", "full"}, gateHint},
		"--gate --validate": {[]string{in, "--gate", "full", "--validate"}, []string{"--gate", "--validate", "two operations"}, nil},
		"--gate --list":     {[]string{"--gate", "full", "--list"}, []string{"--gate", "--list", "two operations"}, nil},
		"--gate --next":     {[]string{"--gate", "full", "--next"}, []string{"--gate", "--next", "two operations"}, nil},
		"--gate, the task form": {[]string{in, "--gate", "full", "--task", "1", "--state", "done"},
			[]string{"--gate", "--task", "two operations"}, nil},
		"--gate, the status form":    {[]string{in, "--gate", "full", "--status", "sealed"}, []string{"--gate", "--status", "two operations"}, nil},
		"--gate --render":            {[]string{in, "--gate", "full", "--render"}, []string{"--gate", "--render", "two operations"}, nil},
		"--gate --migrate":           {[]string{in, "--gate", "full", "--migrate"}, []string{"--gate", "--migrate", "two operations"}, nil},
		"--gate --gate-report":       {[]string{in, "--gate", "full", "--gate-report"}, []string{"--gate", "--gate-report", "two operations"}, nil},
		"--gate without a package":   {[]string{"--gate", "full"}, []string{"--gate", "package"}, nil},
		"--clean-env without --gate": {[]string{in, "--clean-env"}, []string{"--clean-env", "--gate"}, nil},
		"--timeout without --gate":   {[]string{in, "--timeout", "1m"}, []string{"--timeout", "--gate"}, nil},
		"--timeout 0s":               {[]string{in, "--gate", "full", "--timeout", "0s"}, []string{"--timeout", "greater than zero"}, nil},
		"--timeout -1m":              {[]string{in, "--gate", "full", "--timeout", "-1m"}, []string{"--timeout", "greater than zero"}, nil},
		"--timeout soon":             {[]string{in, "--gate", "full", "--timeout", "soon"}, []string{"timeout"}, nil},
		"--since without the report": {[]string{in, "--since", "HEAD"}, []string{"--since", "--gate-report"}, nil},
		"--timeout with --validate":  {[]string{in, "--validate", "--timeout", "1m"}, []string{"--timeout"}, nil},
		"--timeout, a claim":         {[]string{in, "--task", "1", "--state", "in_progress", "--timeout", "1m"}, []string{"--timeout"}, nil},

		// The verify form.
		"--verify-evidence, the task form":   {[]string{in, "--verify-evidence", "--task", "1", "--state", "done"}, []string{"--verify-evidence", "--task", "two operations"}, nil},
		"--verify-evidence --state":          {[]string{in, "--verify-evidence", "--state", "done"}, []string{"--verify-evidence", "two operations"}, nil},
		"--verify-evidence, the status form": {[]string{in, "--verify-evidence", "--status", "sealed"}, []string{"--verify-evidence", "--status", "two operations"}, nil},
		"--verify-evidence --gate":           {[]string{in, "--verify-evidence", "--gate", "full"}, []string{"--verify-evidence", "--gate", "two operations"}, nil},
		"--verify-evidence --validate":       {[]string{in, "--verify-evidence", "--validate"}, []string{"--verify-evidence", "--validate", "two operations"}, nil},
		"--verify-evidence --render":         {[]string{in, "--verify-evidence", "--render"}, []string{"--verify-evidence", "--render", "two operations"}, nil},
		"--verify-evidence --list":           {[]string{"--verify-evidence", "--list"}, []string{"--verify-evidence", "--list", "two operations"}, nil},
		"--verify-evidence, the evidence form": {[]string{in, "--verify-evidence", "--task", "1", "--evidence", file},
			[]string{"--verify-evidence", "--evidence", "two operations"}, nil},
		"--verify-evidence without a package": {[]string{"--verify-evidence"}, []string{"--verify-evidence", "package"}, nil},

		// The gate report.
		"--gate-report --validate":         {[]string{in, "--gate-report", "--validate"}, []string{"--gate-report", "--validate", "two operations"}, nil},
		"--gate-report --list":             {[]string{"--gate-report", "--list"}, []string{"--gate-report", "--list", "two operations"}, nil},
		"--gate-report --next":             {[]string{"--gate-report", "--next"}, []string{"--gate-report", "--next", "two operations"}, nil},
		"--gate-report --render":           {[]string{in, "--gate-report", "--render"}, []string{"--gate-report", "--render", "two operations"}, nil},
		"--gate-report --migrate":          {[]string{in, "--gate-report", "--migrate"}, []string{"--gate-report", "--migrate", "two operations"}, nil},
		"--gate-report, the task form":     {[]string{in, "--gate-report", "--task", "1", "--state", "done"}, []string{"--gate-report", "--task", "two operations"}, nil},
		"--gate-report, the status form":   {[]string{in, "--gate-report", "--status", "sealed"}, []string{"--gate-report", "--status", "two operations"}, nil},
		"--gate-report without a package":  {[]string{"--gate-report"}, []string{"--gate-report", "package"}, nil},
		"--gate-report --clean-env":        {[]string{in, "--gate-report", "--clean-env"}, []string{"--clean-env", "--gate"}, nil},
		"--strict-handoffs alone":          {[]string{in, "--strict-handoffs"}, []string{"--strict-handoffs", "--validate"}, nil},
		"--strict-handoffs --list":         {[]string{"--list", "--strict-handoffs"}, []string{"--strict-handoffs", "--validate"}, nil},
		"--strict-handoffs, the task form": {[]string{in, "--task", "1", "--state", "done", "--strict-handoffs"}, []string{"--strict-handoffs", "--validate"}, nil},
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
		"render":        {"09", "--render"},
		"migrate":       {"09", "--migrate"},
	} {
		env, _, _ := run(t, append([]string{"--dir", root, "--dry-run"}, flags...)...)
		if env.Error != nil && env.Error.Stage == "usage" {
			t.Errorf("%s: refused as a usage error: %s", name, env.Error.Message)
		}
	}
	// --timeout goes with every form that may run a package's tests.
	for _, flags := range [][]string{
		{"09", "--verify-evidence", "--timeout", "5m"},
		{"09", "--task", "1", "--state", "done", "--timeout", "5m", "--dry-run"},
	} {
		if env, _, _ := run(t, append([]string{"--dir", root}, flags...)...); env.Error != nil && env.Error.Stage == "usage" {
			t.Errorf("%v: refused as a usage error: %s", flags, env.Error.Message)
		}
	}
	// The render form and the migrate form take --dry-run, and --all without
	// a package, which every other form refuses.
	for _, flags := range [][]string{
		{"--render", "--dry-run"}, {"--render", "--all"}, {"--render", "--all", "--dry-run"},
		{"--migrate", "--dry-run"}, {"--migrate", "--all"}, {"--migrate", "--all", "--dry-run"},
	} {
		if env, code, _ := run(t, append([]string{"--dir", root}, flags...)...); code != cli.ExitOK {
			t.Errorf("%v: code %d: %+v", flags, code, env.Error)
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
		"next":                {"--dir", root, "--next"},
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

// The report, the verdict, the list, --next and --schema write nothing and reach no
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
		"next":    {"--dir", root, "--next"},
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
