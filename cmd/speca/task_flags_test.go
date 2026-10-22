package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// refusedAsUsage runs the call and fails the test unless it is a usage error
// that wrote nothing, with the message saying each of want.
func refusedAsUsage(t *testing.T, root string, argv []string, want ...string) *cli.ErrorInfo {
	t.Helper()
	before := snapshot(t, root)
	env, code, stderr := run(t, argv...)
	if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil || env.OK {
		t.Errorf("%v: code %d: %+v", argv, code, env.Error)
		return env.Error
	}
	for _, w := range want {
		if !strings.Contains(env.Error.Message, w) {
			t.Errorf("%v: message %q does not say %q", argv, env.Error.Message, w)
		}
	}
	if !strings.Contains(stderr, env.Error.Message) {
		t.Errorf("%v: stderr %q does not hold the message", argv, stderr)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return env.Error
}

// 11-REQ-1.1: --task is a comma-separated list of positive ids, next or all,
// given once or repeated; duplicate ids collapse into one, in the order they
// first appear.
func TestParseTaskSpec_11REQ1_1(t *testing.T) {
	for name, tc := range map[string]struct {
		values  []string
		ids     []int
		keyword string
	}{
		"one id":                  {[]string{"3"}, []int{3}, ""},
		"a comma list":            {[]string{"1,2"}, []int{1, 2}, ""},
		"repeated, with a repeat": {[]string{"1,2", "2"}, []int{1, 2}, ""},
		"order of first sight":    {[]string{"3", "1,3,2"}, []int{3, 1, 2}, ""},
		"spaces":                  {[]string{" 1 , 2 "}, []int{1, 2}, ""},
		"next":                    {[]string{"next"}, nil, "next"},
		"all":                     {[]string{"all"}, nil, "all"},
		"next twice":              {[]string{"next", "next"}, nil, "next"},
	} {
		got, err := parseTaskSpec(tc.values)
		if err != nil || !slices.Equal(got.ids, tc.ids) || got.keyword != tc.keyword {
			t.Errorf("%s: parseTaskSpec(%q) = %+v, %v; want ids %v keyword %q", name, tc.values, got, err, tc.ids, tc.keyword)
		}
	}
	for name, values := range map[string][]string{
		"zero": {"0"}, "negative": {"-1"}, "a word": {"abc"}, "an empty item": {"1,,2"}, "an empty value": {""},
		"a trailing comma": {"1,"}, "next with an id": {"next,1"}, "all with an id": {"all,2"}, "next with all": {"next,all"},
		"next and an id, repeated": {"next", "1"}, "a fraction": {"1.5"},
	} {
		if got, err := parseTaskSpec(values); err == nil {
			t.Errorf("%s: parseTaskSpec(%q) = %+v; want an error", name, values, got)
		}
	}
}

// A call that is well-formed is not a usage error: it reaches the package.
func TestAValidTaskFormIsNotAUsageError(t *testing.T) {
	root := newRepo(t)
	for name, argv := range map[string][]string{
		"an id":                {"--dir", root, "09", "--task", "1", "--state", "in_progress"},
		"ids, repeated":        {"--dir", root, "09", "--task", "1,2", "--task", "2", "--state", "in_progress"},
		"next":                 {"--dir", root, "09", "--task", "next", "--state", "in_progress"},
		"all":                  {"--dir", root, "09", "--task", "all", "--state", "done"},
		"flags after":          {"--dir", root, "--task", "1", "--state", "in_progress", "--force", "--dry-run", "09"},
		"every state: pending": {"--dir", root, "09", "--task", "1", "--state", "pending"},
		"every state: dropped": {"--dir", root, "09", "--task", "1", "--state", "dropped"},
	} {
		env, _, _ := run(t, argv...)
		if env.Error != nil && env.Error.Stage == "usage" {
			t.Errorf("%s: refused as a usage error: %s", name, env.Error.Message)
		}
	}
}

// TS-11-2: --task without --state, and --state without --task, are usage errors.
func TestTaskWithoutStateAndStateWithoutTask_TS11_2(t *testing.T) {
	root := newRepo(t)
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "1"}, "--task", "--state")
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--state", "done"}, "--state", "--task")
}

// TS-11-3: a --state outside the four is refused, with the four as the fix.
func TestAStateOutsideTheFourIsRefused_TS11_3(t *testing.T) {
	root := newRepo(t)
	e := refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "1", "--state", "finished"}, `"finished"`, "pending, in_progress, done, dropped")
	if e == nil || e.FixHint == nil || e.FixHint.Flag != "--state" ||
		!slices.Equal(e.FixHint.Valid, []string{"pending", "in_progress", "done", "dropped"}) {
		t.Errorf("fix_hint = %+v; want --state with the four states", e)
	}
}

// TS-11-4: a --task value that is not a positive id, next or all, and next or
// all beside anything else, are usage errors.
func TestBadTaskValuesAreRefused_TS11_4(t *testing.T) {
	root := newRepo(t)
	for _, v := range []string{"0", "-1", "abc", "1,,2", "next,1", "all,2", "next,all"} {
		refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", v, "--state", "done"}, "--task", `"`+v+`"`)
	}
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "next", "--task", "1", "--state", "done"}, "--task", "next")
}

// TS-11-5: the task form beside --validate or --list names two operations.
func TestTheTaskFormWithAnotherOperationIsRefused_TS11_5(t *testing.T) {
	root := newRepo(t)
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--task", "1", "--state", "in_progress", "--validate"}, "two operations", "--validate")
	refusedAsUsage(t, root, []string{"--dir", root, "--list", "--task", "1", "--state", "done"}, "two operations", "--list")
}

// TS-11-6: the task form takes exactly one input, and it is a package on disk.
func TestTheTaskFormTakesOneInput_TS11_6(t *testing.T) {
	root := newRepo(t)
	flags := []string{"--task", "1", "--state", "done"}
	for name, argv := range map[string][]string{
		"no input":      append([]string{"--dir", root}, flags...),
		"two inputs":    append([]string{"--dir", root, "09", "01"}, flags...),
		"stdin":         append([]string{"--dir", root, "-"}, flags...),
		"a URL":         append([]string{"--dir", root, "https://example.com/x/issues/1"}, flags...),
		"several lines": append([]string{"--dir", root, "09\n01"}, flags...),
	} {
		before := snapshot(t, root)
		env, code, _ := run(t, argv...)
		if code != cli.ExitUsage || env.Error == nil || env.Error.Stage != "usage" || env.Error.Category != "usage" || env.Result != nil {
			t.Errorf("%s: code %d: %+v", name, code, env.Error)
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%s: the repository changed", name)
		}
	}
}

// TS-11-7: --force and --dry-run apply to the task form only.
func TestForceAndDryRunNeedTheTaskForm_TS11_7(t *testing.T) {
	root := newRepo(t)
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--force"}, "--force", "--task")
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--dry-run"}, "--dry-run", "--task")
	refusedAsUsage(t, root, []string{"--dir", root, "09", "--validate", "--force"}, "--force")
	refusedAsUsage(t, root, []string{"--dir", root, "--list", "--dry-run"}, "--dry-run")
}
