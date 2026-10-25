package main

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// A package the library refuses fails the report, the verdict and the task form
// at stage load, naming the directory and the library's reason, with no result
// and nothing said about the format.
func TestForms_FailAtLoadOnAPackageTheLibraryRefuses(t *testing.T) {
	root := newRepo(t)
	malformedPRD := pkgDir(root, "05_badprd")
	copyDir(t, fixture("valid_spec"), malformedPRD)
	writeFile(t, filepath.Join(malformedPRD, "prd.md"), "---\nspec_id: [unclosed\n---\n")
	malformedJSON := pkgDir(root, "06_badjson")
	copyDir(t, fixture("malformed_json"), malformedJSON)
	malformedTasks := addPackage(t, root, "08_badtasks", "08", "badtasks")
	writeFile(t, filepath.Join(malformedTasks, "tasks.json"), "{ not json")

	for name, broken := range map[string]string{"a malformed prd.md": malformedPRD, "a malformed artifact": malformedJSON, "a malformed tasks.json": malformedTasks} {
		_, reason := afspec.LoadSpec(broken)
		if reason == nil {
			t.Fatalf("%s: the library loads %s", name, broken)
		}
		for form, flags := range map[string][]string{"report": nil, "verdict": {"--validate"}, "task form": {"--task", "1", "--state", "in_progress"}} {
			t.Run(name+", "+form, func(t *testing.T) {
				before := snapshot(t, root)
				env, code, stderr := run(t, append([]string{"--dir", root, broken}, flags...)...)
				if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Result != nil || env.OK {
					t.Fatalf("code %d: %+v", code, env.Error)
				}
				if !strings.Contains(env.Error.Message, broken) || !strings.Contains(env.Error.Message, reason.Error()) {
					t.Errorf("message = %q; want the directory and the library's reason %q", env.Error.Message, reason)
				}
				if env.Error.Retryable || env.Error.Resumable || stderr != "" || len(env.Warnings) != 0 {
					t.Errorf("retryable %v resumable %v stderr %q warnings %v", env.Error.Retryable, env.Error.Resumable, stderr, warningCodes(env))
				}
				if !maps.Equal(before, snapshot(t, root)) {
					t.Error("the repository changed")
				}
			})
		}
	}
}

// A package in another format version is reported by the report and the
// verdict, with what loaded and a high warning that its tests and tasks were
// not read.
func TestForms_WarnAboutAPackageInAnotherFormatVersion(t *testing.T) {
	root := newRepo(t)
	addLegacyPackage(t, root)

	for form, flags := range map[string][]string{"report": nil, "verdict": {"--validate"}} {
		env, code, _ := run(t, append([]string{"--dir", root, "07"}, flags...)...)
		if code != cli.ExitOK || env.Result == nil || env.Result.SpecID != "07" || env.Result.Status == "" {
			t.Errorf("%s: code %d: %+v", form, code, env.Result)
			continue
		}
		if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSpecInvalid || env.Warnings[0].Severity != "high" ||
			!strings.Contains(env.Warnings[0].Message, "tests and tasks are not read") {
			t.Errorf("%s: warnings = %+v", form, env.Warnings)
		}
		if !strings.Contains(env.Summary, "1 high-severity warning") {
			t.Errorf("%s: summary = %q", form, env.Summary)
		}
		if form == "report" && len(statusCalls(env.Next)) != 0 {
			t.Errorf("report: next %+v; the status form does not change a package in another version", env.Next)
		}
	}
}
