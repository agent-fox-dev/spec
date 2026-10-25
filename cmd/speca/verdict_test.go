package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// The verdict holds the identity, the combined verdict, the findings and the
// coverage, and none of what only the report holds. A package that does not
// validate is a successful call: the answer is in valid.
func TestVerdict_HoldsTheVerdictAndNothingElse(t *testing.T) {
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
	breakValidity(t, pkg)
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
	if env.ExitCode != cli.ExitOK || !env.OK || env.Error != nil || r.Valid == nil || *r.Valid || v == nil || v.Valid || v.ErrorCount == 0 || v.WarningCount == 0 {
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
	if !strings.Contains(env.Summary, "does not validate") || !strings.Contains(env.Summary, "C7") || len(env.Next) != 0 {
		t.Errorf("summary %q, next %+v; want the rules and no suggestion", env.Summary, env.Next)
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

// Of what the cross-spec rules find over the whole spec root, the verdict keeps
// what concerns the package, after the package's own findings.
func TestVerdict_KeepsTheCrossSpecFindingsOfThePackage(t *testing.T) {
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

// When the spec root cannot be read the cross-spec rules are skipped, with a
// warning, and the verdict is the package's own.
func TestVerdict_WithAnUnreadableSpecRoot(t *testing.T) {
	root := newRepo(t) // 09 depends on 08, which the root lacks: a cross-spec error, were the root read
	missing := filepath.Join(t.TempDir(), "missing")

	env, code, _ := run(t, "--specs-dir", missing, pkgDir(root, "09_agent_mode"), "--validate")
	r := result(t, env)
	if code != cli.ExitOK || r.Valid == nil || !*r.Valid || len(r.Validation.Errors) != 0 {
		t.Fatalf("exit %d: %+v", code, r)
	}
	if len(env.Warnings) != 1 || env.Warnings[0].Code != cli.WarnSpecsDirUnreadable || env.Warnings[0].Severity != "low" ||
		env.Warnings[0].Stage != "preflight" || !strings.Contains(env.Warnings[0].Message, missing) {
		t.Errorf("warnings = %+v", env.Warnings)
	}
}

// A sibling that cannot be loaded is left out of the cross-spec run, without a
// word: it is its own verdict's to report.
func TestVerdict_LeavesOutASiblingThatDoesNotLoad(t *testing.T) {
	root := newRepo(t)
	setDependencies(t, pkgDir(root, "09_agent_mode"))
	copyDir(t, fixture("malformed_yaml"), pkgDir(root, "05_broken"))
	if err := os.Remove(filepath.Join(pkgDir(root, "05_broken"), "prd.md")); err != nil {
		t.Fatal(err)
	}

	env, code, _ := run(t, "--dir", root, "09", "--validate")
	if code != cli.ExitOK || env.Error != nil || result(t, env).Valid == nil || !*result(t, env).Valid || len(env.Warnings) != 0 {
		t.Errorf("code %d, error %+v, result %+v, warnings %v", code, env.Error, env.Result, warningCodes(env))
	}
}

// intent_drift is written by --validate too, and only for a package that
// drifted.
func TestVerdict_IntentDrift(t *testing.T) {
	root := newRepo(t)
	setDependencies(t, pkgDir(root, "09_agent_mode"))
	driftIntent(t, activePackage(t, root, "12_edited", "12", "edited"))
	activePackage(t, root, "11_matching", "11", "matching")

	if _, raw := rawRun(t, "--dir", root, "12", "--validate"); string(raw["intent_drift"]) != "true" {
		t.Errorf("an edited intent: intent_drift = %s; want true", raw["intent_drift"])
	}
	for _, ref := range []string{"11", "09"} {
		if _, raw := rawRun(t, "--dir", root, ref, "--validate"); raw["intent_drift"] != nil {
			t.Errorf("%s: intent_drift = %s; want it absent", ref, raw["intent_drift"])
		}
	}
}
