package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// checks lists the rule of each finding.
func checks(items []envelope.ValidationItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Check)
	}
	return out
}

// ownChecks are the rules the package breaks on its own, read straight from
// the library, in the order it reports them.
func ownChecks(t *testing.T, dir string) (errs, warns []string) {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := spec.Validate()
	for _, e := range v.Errors {
		errs = append(errs, e.Check)
	}
	for _, w := range v.Warnings {
		warns = append(warns, w.Check)
	}
	return errs, warns
}

// setActors gives every step of every execution path of the package this actor.
func setActors(t *testing.T, dir, actor string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		for _, p := range m["execution_paths"].([]any) {
			for _, step := range p.(map[string]any)["steps"].([]any) {
				step.(map[string]any)["actor"] = actor
			}
		}
	})
}

// setGlossaryTerm defines a term in the package's glossary.
func setGlossaryTerm(t *testing.T, dir, term, definition string) {
	t.Helper()
	editJSON(t, filepath.Join(dir, "requirements.json"), func(m map[string]any) {
		m["glossary"].(map[string]any)[term] = definition
	})
}

// addLegacyPackage adds a package in format version 1 as 07_legacy: the v2
// example with its frontmatter's version set back and a v1 task plan, whose
// task_groups the v2 types do not read.
func addLegacyPackage(t *testing.T, root string) {
	t.Helper()
	legacy := addPackage(t, root, "07_legacy", "07", "legacy")
	editFile(t, filepath.Join(legacy, "prd.md"), "schema_version: 2", "schema_version: 1")
	v1 := `{"spec_id": "07", "spec_name": "legacy", "schema_version": 1,
  "task_groups": [{"id": 1, "kind": "tests", "title": "Write tests", "subtasks": [{"id": "1.1", "title": "Write a test", "state": "done"}]}]}`
	if err := os.WriteFile(filepath.Join(legacy, "tasks.json"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
}

// both are the two forms that read one package.
var both = map[string][]string{"report": nil, "verdict": {"--validate"}}

// TS-10-33: a package the library refuses fails the call at stage load, in the
// report and in the verdict, naming the directory and the library's reason.
func TestAPackageThatDoesNotLoadFailsBothForms_TS10_33(t *testing.T) {
	root := newRepo(t)
	malformedPRD := pkgDir(root, "05_badprd")
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "valid_spec"), malformedPRD)
	if err := os.WriteFile(filepath.Join(malformedPRD, "prd.md"), []byte("---\nspec_id: [unclosed\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	malformedJSON := pkgDir(root, "06_badjson")
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "malformed_json"), malformedJSON)

	for name, broken := range map[string]string{"a malformed prd.md": malformedPRD, "a malformed artifact": malformedJSON} {
		_, reason := afspec.LoadSpec(broken)
		if reason == nil {
			t.Fatalf("%s: the library loads %s", name, broken)
		}
		for form, flags := range both {
			env, code, stderr := run(t, append([]string{"--dir", root, broken}, flags...)...)
			if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Result != nil || env.OK {
				t.Errorf("%s, %s: code %d: %+v", name, form, code, env.Error)
				continue
			}
			if !strings.Contains(env.Error.Message, broken) || !strings.Contains(env.Error.Message, reason.Error()) {
				t.Errorf("%s, %s: message = %q; want the directory and the library's reason %q", name, form, env.Error.Message, reason)
			}
			if env.Error.Retryable || env.Error.Resumable || stderr != "" {
				t.Errorf("%s, %s: retryable %v resumable %v stderr %q", name, form, env.Error.Retryable, env.Error.Resumable, stderr)
			}
		}
	}
}

// TS-10-34: a package in another format version is reported, in both forms,
// with what loaded and a high warning that its tests and tasks were not read.
func TestAPackageInAnotherFormatIsWarnedAbout_TS10_34(t *testing.T) {
	root := newRepo(t)
	addLegacyPackage(t, root)

	for form, flags := range both {
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
	}
}

// TS-10-35: the verdict holds the identity, the combined verdict, the findings
// and the coverage, and none of what only the report holds.
func TestVerdictHoldsTheVerdictAndNothingElse_TS10_35(t *testing.T) {
	root := newRepo(t)
	pkg := pkgDir(root, "09_agent_mode")
	setDependencies(t, pkg) // the example depends on spec 08, which this root lacks

	env, raw := rawRun(t, "--dir", root, "--validate", "09")
	r := result(t, env)
	if env.ExitCode != cli.ExitOK || r.Valid == nil || !*r.Valid || r.Validation == nil || !r.Validation.Valid || r.Validation.ErrorCount != 0 {
		t.Fatalf("a valid package: exit %d, %+v", env.ExitCode, r)
	}
	if r.SpecDir != filepath.Join(".specs", "09_agent_mode") || r.SpecID != "09" || r.Status != "draft" || r.Title == "" || r.CreatedAt == "" {
		t.Errorf("identity = %+v", r)
	}
	if tr := r.Traceability; tr == nil || tr.CriteriaCovered != 5 || tr.PathsCovered != 1 || len(tr.CriteriaUncovered) != 0 || len(tr.PathsUncovered) != 0 || len(tr.TestsUnowned) != 0 {
		t.Errorf("traceability = %+v", tr)
	}
	for _, absent := range []string{"tasks", "task_counts", "next_task", "allowed_status", "depends_on", "dependents", "failing_rules", "packages", "intent_drift"} {
		if _, ok := raw[absent]; ok {
			t.Errorf("the verdict writes %s: %s", absent, raw[absent])
		}
	}
	for _, present := range []string{"valid", "validation", "traceability", "spec_dir"} {
		if _, ok := raw[present]; !ok {
			t.Errorf("the verdict lacks %s", present)
		}
	}

	// Damage the package: a test that does not exist breaks rules, and a term in
	// every criterion with no glossary entry is a warning.
	editFile(t, filepath.Join(pkg, "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
	editJSON(t, filepath.Join(pkg, "requirements.json"), func(m map[string]any) {
		for _, req := range m["requirements"].([]any) {
			for _, c := range req.(map[string]any)["criteria"].([]any) {
				c.(map[string]any)["action"] = c.(map[string]any)["action"].(string) + " using `Frobnicate`"
			}
		}
	})
	env, _ = rawRun(t, "--dir", root, "09", "--validate")
	r = result(t, env)
	v := r.Validation
	if env.ExitCode != cli.ExitOK || r.Valid == nil || *r.Valid || v == nil || v.Valid || v.ErrorCount == 0 || v.WarningCount == 0 {
		t.Fatalf("an invalid package: exit %d, valid %v, %+v", env.ExitCode, r.Valid, v)
	}
	if v.ErrorCount != len(v.Errors) || v.WarningCount != len(v.Warnings) {
		t.Errorf("counts %d and %d, findings %d and %d", v.ErrorCount, v.WarningCount, len(v.Errors), len(v.Warnings))
	}
	for _, it := range append(slices.Clone(v.Errors), v.Warnings...) {
		if it.Check == "" || it.Artifact == "" || it.Message == "" {
			t.Errorf("finding %+v lacks a rule, an artifact or a message", it)
		}
	}
	if !slices.Contains(checks(v.Errors), "C7") || !slices.Contains(checks(v.Warnings), "glossary_hint") {
		t.Errorf("errors %v warnings %v", checks(v.Errors), checks(v.Warnings))
	}

	// valid is the combined verdict: a package that validates alone does not
	// when the dependency it names is not in the spec root.
	other := newRepo(t)
	env, _, _ = run(t, "--dir", other, "09")
	if result(t, env).Valid == nil || !*result(t, env).Valid {
		t.Fatalf("the report of the example: valid %v", result(t, env).Valid)
	}
	env, _, _ = run(t, "--dir", other, "09", "--validate")
	r = result(t, env)
	if r.Valid == nil || *r.Valid || r.Validation.Valid || !slices.Equal(checks(r.Validation.Errors), []string{"cross_spec_unknown_dependency"}) {
		t.Errorf("the example under --validate: valid %v, errors %v", r.Valid, checks(r.Validation.Errors))
	}
}

// TS-10-36: of what the cross-spec rules find over the whole spec root, the
// verdict keeps what concerns the package, after the package's own findings.
func TestVerdictKeepsTheCrossSpecFindingsOfThePackage_TS10_36(t *testing.T) {
	root := t.TempDir()
	a := addPackage(t, root, "01_a", "01", "a")
	b := addPackage(t, root, "02_b", "02", "b")
	c := addPackage(t, root, "03_c", "03", "c")
	d := addPackage(t, root, "04_d", "04", "d")
	old := addPackage(t, root, "archive/05_old", "05", "old")

	setDependencies(t, a, "02", "99") // 99 is not in the root
	setDependencies(t, b, "01", "03") // 01 and 02 are a cycle; 03 shares no actor with 02
	setDependencies(t, old, "98")
	setActors(t, c, "nobody else")
	setGlossaryTerm(t, b, "frobnicate", "as b defines it")
	setGlossaryTerm(t, c, "frobnicate", "as c defines it")

	// verdict is the rules of the errors and of the warnings --validate reports.
	verdict := func(ref string) (errs, warns []string, report *envelope.ValidationReport) {
		t.Helper()
		env, code, _ := run(t, "--dir", root, ref, "--validate")
		if code != cli.ExitOK {
			t.Fatalf("%s: code %d: %+v", ref, code, env.Error)
		}
		v := result(t, env).Validation
		return checks(v.Errors), checks(v.Warnings), v
	}

	// A: its own findings, then its unknown dependency and the cycle.
	ownErrs, _ := ownChecks(t, a)
	errs, _, report := verdict("01")
	if want := append(slices.Clone(ownErrs), "cross_spec_unknown_dependency", "cross_spec_cycle"); !slices.Equal(errs, want) {
		t.Errorf("A: errors %v; want %v", errs, want)
	}
	// The cycle is made of dependencies, which tasks.json declares; the library names no artifact for it.
	if cycle := report.Errors[len(report.Errors)-1]; cycle.Check != "cross_spec_cycle" || cycle.Artifact != "tasks.json" || cycle.Message == "" {
		t.Errorf("A: the cycle is reported as %+v", cycle)
	}

	// B: the cycle, and its actor error; not A's unknown dependency.
	ownErrs, ownWarns := ownChecks(t, b)
	errs, warns, report := verdict("02")
	if want := append(slices.Clone(ownErrs), "cross_spec_cycle", "cross_spec_actor"); !slices.Equal(errs, want) {
		t.Errorf("B: errors %v; want %v", errs, want)
	}
	// The library gives a glossary conflict no rule and no artifact, and a
	// verdict names the rule of every finding; the conflict is about the term.
	glossary := report.Warnings[len(report.Warnings)-1]
	if want := append(slices.Clone(ownWarns), "cross_spec_glossary"); !slices.Equal(warns, want) ||
		glossary.Entity != "frobnicate" || glossary.Artifact != "requirements.json" || glossary.Message == "" {
		t.Errorf("B: warnings %v, last %+v; want its own and the glossary conflict over frobnicate", warns, glossary)
	}
	for _, it := range report.Errors {
		if it.Check == "cross_spec_unknown_dependency" {
			t.Errorf("B reports A's unknown dependency: %+v", it)
		}
	}

	// C: the glossary warning for a term it defines. It has an edge, so the
	// cycle, which names no package, is kept too.
	errs, _, report = verdict("03")
	if !slices.Contains(errs, "cross_spec_cycle") || slices.Contains(errs, "cross_spec_actor") {
		t.Errorf("C: errors %v", errs)
	}
	if len(report.Warnings) == 0 || report.Warnings[len(report.Warnings)-1].Entity != "frobnicate" {
		t.Errorf("C: warnings %+v; want the glossary conflict over frobnicate", report.Warnings)
	}

	// D has no edge, so the cycle is not its business, and defines no term in conflict.
	ownErrs, ownWarns = ownChecks(t, d)
	errs, warns, _ = verdict("04")
	if !slices.Equal(errs, ownErrs) || !slices.Equal(warns, ownWarns) {
		t.Errorf("D: errors %v warnings %v; want only its own %v %v", errs, warns, ownErrs, ownWarns)
	}

	// An archived package is not in the spec root, and is in the cross-spec run.
	errs, _, _ = verdict(old)
	if !slices.Contains(errs, "cross_spec_unknown_dependency") {
		t.Errorf("the archived package: errors %v; want its unknown dependency", errs)
	}
}

// TS-10-37: when the spec root cannot be read the cross-spec rules are skipped,
// with a warning, and the verdict is the package's own.
func TestVerdictWithAnUnreadableSpecRoot_TS10_37(t *testing.T) {
	root := newRepo(t) // 09 depends on 08, which the root lacks: a cross-spec error, were the root read
	pkg := pkgDir(root, "09_agent_mode")
	missing := filepath.Join(t.TempDir(), "missing")

	env, _ := rawRun(t, "--specs-dir", missing, pkg, "--validate")
	r := result(t, env)
	if env.ExitCode != cli.ExitOK || r.Valid == nil || !*r.Valid || len(r.Validation.Errors) != 0 {
		t.Fatalf("exit %d: %+v", env.ExitCode, r)
	}
	if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSpecsDirUnreadable || env.Warnings[0].Severity != "low" ||
		env.Warnings[0].Stage != "preflight" || !strings.Contains(env.Warnings[0].Message, missing) {
		t.Errorf("warnings = %+v", env.Warnings)
	}
}

// TS-10-38: a sibling that cannot be loaded is left out of the cross-spec run.
func TestVerdictLeavesOutASiblingThatDoesNotLoad_TS10_38(t *testing.T) {
	root := newRepo(t)
	setDependencies(t, pkgDir(root, "09_agent_mode"))
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "malformed_yaml"), pkgDir(root, "05_broken"))
	if err := os.Remove(filepath.Join(pkgDir(root, "05_broken"), "prd.md")); err != nil {
		t.Fatal(err)
	}

	env, code, _ := run(t, "--dir", root, "09", "--validate")
	if code != cli.ExitOK || env.Error != nil || result(t, env).Valid == nil || !*result(t, env).Valid {
		t.Errorf("code %d, error %+v, result %+v", code, env.Error, env.Result)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings = %v", warningCodes(env))
	}
}

// TS-10-39: a package that does not validate is a successful call.
func TestVerdictOfAnInvalidPackageIsASuccess_TS10_39(t *testing.T) {
	root := newRepo(t)
	editFile(t, filepath.Join(pkgDir(root, "09_agent_mode"), "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)

	env, code, _ := run(t, "--dir", root, "--validate", "09")
	r := result(t, env)
	if code != cli.ExitOK || !env.OK || env.Error != nil || r.Valid == nil || *r.Valid || r.Validation.ErrorCount == 0 {
		t.Errorf("code %d: ok %v, error %+v, valid %v", code, env.OK, env.Error, r.Valid)
	}
	if !strings.Contains(env.Summary, "does not validate") {
		t.Errorf("summary = %q", env.Summary)
	}
}

// TS-10-26 (the verdict): intent_drift is written by --validate too.
func TestVerdictIntentDrift_TS10_26(t *testing.T) {
	root := newRepo(t)
	setDependencies(t, pkgDir(root, "09_agent_mode"))
	edited := addPackage(t, root, "12_edited", "12", "edited")
	activate(t, edited)
	editFile(t, filepath.Join(edited, "prd.md"), "so that an agent can parse results", "so that anyone can parse results")
	matching := addPackage(t, root, "11_matching", "11", "matching")
	activate(t, matching)

	if _, raw := rawRun(t, "--dir", root, "12", "--validate"); string(raw["intent_drift"]) != "true" {
		t.Errorf("an edited intent: intent_drift = %s; want true", raw["intent_drift"])
	}
	for _, ref := range []string{"11", "09"} {
		if _, raw := rawRun(t, "--dir", root, ref, "--validate"); raw["intent_drift"] != nil {
			t.Errorf("%s: intent_drift = %s; want it absent", ref, raw["intent_drift"])
		}
	}
}
