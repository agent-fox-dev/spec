package main

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
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
	if tr := r.Traceability; tr == nil || tr.CriteriaCovered != 5 || tr.PathsCovered != 1 || len(tr.CriteriaUncovered) != 0 || len(tr.PathsUncovered) != 0 || len(tr.CriteriaUnowned) != 0 {
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

	// Damage the package: a task that claims a criterion the package does not
	// have breaks rules, and a criterion that names an error outcome without a
	// contract is a warning.
	breakValidity(t, pkg)
	editJSON(t, filepath.Join(pkg, "requirements.json"), func(m map[string]any) {
		c := m["requirements"].([]any)[0].(map[string]any)["criteria"].([]any)[0].(map[string]any)
		c["action"] = c["action"].(string) + ", or fail with an error"
		delete(c, "contract")
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
	if !slices.Contains(checks(v.Errors), "C4") || !slices.Contains(checks(v.Warnings), "missing_contract") {
		t.Errorf("errors %v warnings %v", checks(v.Errors), checks(v.Warnings))
	}
	if !strings.Contains(env.Summary, "does not validate") || !strings.Contains(env.Summary, "C4") || len(env.Next) != 0 {
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

	// B: the cycle, and its actor error; not A's unknown dependency. The
	// cross-spec rules raise no warning: its warnings are its own.
	ownErrs, ownWarns := ownChecks(t, b)
	errs, warns, report := verdict("02")
	if want := append(slices.Clone(ownErrs), "cross_spec_cycle", "cross_spec_actor"); !slices.Equal(errs, want) {
		t.Errorf("B: errors %v; want %v", errs, want)
	}
	if !slices.Equal(warns, ownWarns) {
		t.Errorf("B: warnings %v; want its own, %v", warns, ownWarns)
	}
	for _, it := range report.Errors {
		if it.Check == "cross_spec_unknown_dependency" {
			t.Errorf("B reports A's unknown dependency: %+v", it)
		}
	}

	// C has an edge, so the cycle, which names no package, is kept too; the
	// actor error is B's, which names C's actors, not C's own.
	errs, _, _ = verdict("03")
	if !slices.Contains(errs, "cross_spec_cycle") || slices.Contains(errs, "cross_spec_actor") {
		t.Errorf("C: errors %v", errs)
	}

	// D has no edge, so the cycle is not its business.
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

// A steering whose frontmatter breaks a rule is one high warning on the
// verdict, which is the verdict it would have been without the frontmatter;
// the report does not raise it.
func TestVerdict_WarnsOnceAboutAMalformedSteeringRecord(t *testing.T) {
	root := newRepo(t)
	const body = "# Steering\n\nUse make check.\n"
	steering := filepath.Join(root, ".specs", "steering.md")
	writeFile(t, steering, "---\nversion: 1.0.0\namendments:\n  - version: 1.0.0\n    date: 2026-11-01\n    change: \"\"\n---\n"+body)

	env, code, _ := run(t, "--dir", root, "09", "--validate")
	if code != cli.ExitOK {
		t.Fatalf("exit %d: %+v", code, env.Error)
	}
	var malformed []envelope.Warning
	for _, w := range env.Warnings {
		if w.Code == cli.WarnSteeringMalformed {
			malformed = append(malformed, w)
		}
	}
	if len(malformed) != 1 || malformed[0].Severity != "high" || malformed[0].Stage != "preflight" ||
		!strings.Contains(malformed[0].Message, "must have a change") {
		t.Errorf("warnings %+v; want one high steering_malformed at preflight naming the rule", env.Warnings)
	}

	writeFile(t, steering, body)
	plain, _, _ := run(t, "--dir", root, "09", "--validate")
	if !reflect.DeepEqual(result(t, env).Valid, result(t, plain).Valid) || !reflect.DeepEqual(result(t, env).Validation, result(t, plain).Validation) {
		t.Errorf("the verdict changed with the frontmatter:\n%+v\n%+v", result(t, env).Validation, result(t, plain).Validation)
	}

	writeFile(t, steering, "---\nversion: x\n---\n"+body)
	report, _, _ := run(t, "--dir", root, "09")
	if slices.Contains(warningCodes(report), string(cli.WarnSteeringMalformed)) {
		t.Errorf("the report raised steering_malformed: %v", warningCodes(report))
	}
}

// The verdict reports the authoring hints beside the format's verdict: an
// empty list for a clean package, and a vague word, a line reference and a
// foreign test command for a package that has them, none of which changes
// valid.
func TestVerdict_ReportsTheAuthoringHintsWithoutChangingValid(t *testing.T) {
	root := newRepo(t)
	pkg := pkgDir(root, "09_agent_mode")
	setDependencies(t, pkg)

	env, raw := rawRun(t, "--dir", root, "--validate", "09")
	if got := string(raw["hints"]); got != "[]" {
		t.Errorf("a clean package's hints = %s, want []", got)
	}
	if strings.Contains(env.Summary, "hint") {
		t.Errorf("summary %q names hints that are not there", env.Summary)
	}

	// The repository is now a Python project, so the example's go commands
	// are foreign; one criterion and one step get a word and a line each.
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"x\"\n")
	editJSON(t, filepath.Join(pkg, "requirements.json"), func(m map[string]any) {
		c := m["requirements"].([]any)[0].(map[string]any)["criteria"].([]any)[0].(map[string]any)
		c["action"] = c["action"].(string) + " as needed"
	})
	editJSON(t, filepath.Join(pkg, "tasks.json"), func(m map[string]any) {
		task := m["tasks"].([]any)[0].(map[string]any)
		task["steps"] = append(task["steps"].([]any), "Change the flag parsing at cmd/spec/main.go:40")
	})
	succeeds(t, root, "09", "--render")

	env, _ = rawRun(t, "--dir", root, "--validate", "09")
	r := result(t, env)
	if r.Valid == nil || !*r.Valid || r.Validation.WarningCount != 0 {
		t.Fatalf("the hints changed the verdict: valid %v, %+v", r.Valid, r.Validation)
	}
	want := []string{"vague_language", "line_reference", "foreign_test_command", "foreign_test_command", "foreign_test_command"}
	if got := checks(r.Hints); !slices.Equal(got, want) {
		t.Fatalf("hint checks = %v, want %v", got, want)
	}
	for _, h := range r.Hints {
		if h.Artifact == "" || h.Entity == "" || h.Message == "" {
			t.Errorf("hint %+v lacks an artifact, an entity or a message", h)
		}
	}
	if !strings.Contains(env.Summary, "validates; 5 authoring hints") {
		t.Errorf("summary %q", env.Summary)
	}
}

// A handoff goes to a package that depends on the sender, directly or
// transitively; one to no package of the spec root, to the package itself or
// the wrong way is an error of the sending package only.
func TestVerdict_RefusesAHandoffThatPointsTheWrongWay(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to string
		says     string // empty when valid
	}{
		"01 to 02":             {"01", "02", ""},
		"01 to 03, transitive": {"01", "03", ""},
		"02 to 01":             {"02", "01", "does not depend"},
		"02 to itself":         {"02", "02", "itself"},
		"01 to 99":             {"01", "99", "no package"},
		"01 to an archived 00": {"01", "00", "no package"},
		"03 to 01":             {"03", "01", "does not depend"},
	} {
		t.Run(name, func(t *testing.T) {
			root, dirs := handoffRoot(t)
			setHandoffs(t, dirs[tc.from], [4]string{"H-1", "the hook", tc.to, "REQ-1.1"})
			v := validation(t, root, dirs[tc.from])
			errs := itemsOf(v.Errors, "cross_spec_handoff_direction")
			if tc.says == "" {
				if len(errs) != 0 || !v.Valid {
					t.Errorf("errors %+v; want the handoff accepted", v.Errors)
				}
				return
			}
			if len(errs) != 1 || v.Valid || errs[0].Artifact != "requirements.json" ||
				!strings.Contains(errs[0].Message, "H-1") || !strings.Contains(errs[0].Message, tc.says) {
				t.Fatalf("errors %+v; want one cross_spec_handoff_direction saying %q", v.Errors, tc.says)
			}
			for id := range dirs {
				if id != tc.from && len(itemsOf(validation(t, root, dirs[id]).Errors, "cross_spec_handoff_direction")) != 0 {
					t.Errorf("package %s is told of %s's handoff", id, tc.from)
				}
			}
		})
	}
}

// A takes_handoffs entry names a handoff its package declares to this one.
func TestVerdict_RefusesATakeOfNoHandoffForThisPackage(t *testing.T) {
	for name, tc := range map[string]struct {
		taker, take string
		says        string
	}{
		"02 takes 01:H-1":        {"02", "01:H-1", ""},
		"02 takes 01:H-2":        {"02", "01:H-2", "H-2"},
		"02 takes 09:H-1":        {"02", "09:H-1", "09"},
		"03 takes 01:H-1, to 02": {"03", "01:H-1", "02"},
	} {
		t.Run(name, func(t *testing.T) {
			root, dirs := handoffRoot(t)
			setHandoffs(t, dirs["01"], [4]string{"H-1", "the hook", "02", "REQ-1.1"})
			setTakes(t, dirs[tc.taker], tc.take)
			v := validation(t, root, dirs[tc.taker])
			errs := itemsOf(v.Errors, "cross_spec_handoff_taken")
			if tc.says == "" {
				if len(errs) != 0 || !v.Valid {
					t.Errorf("errors %+v; want the take accepted", v.Errors)
				}
				return
			}
			if len(errs) != 1 || v.Valid || errs[0].Artifact != "tasks.json" || !strings.Contains(errs[0].Message, "task 1") ||
				!strings.Contains(errs[0].Message, tc.take) || !strings.Contains(errs[0].Message, tc.says) {
				t.Errorf("errors %+v; want one cross_spec_handoff_taken naming task 1, %s and %s", v.Errors, tc.take, tc.says)
			}
		})
	}
}

// The receiver is warned of each inbound handoff no task takes, and with
// --strict-handoffs it is an error; the flag writes nothing.
func TestVerdict_TellsTheReceiverWhatItHasNotTaken(t *testing.T) {
	root, dirs := handoffRoot(t)
	setHandoffs(t, dirs["01"], [4]string{"H-1", "the landing hook", "02", "REQ-1.1"}, [4]string{"H-2", "the fork hook", "02", "REQ-1.2"})
	setTakes(t, dirs["02"], "01:H-1")

	v := validation(t, root, dirs["02"])
	warns := itemsOf(v.Warnings, "handoff_not_taken")
	if !v.Valid || len(warns) != 1 || !strings.Contains(warns[0].Message, "01") || !strings.Contains(warns[0].Message, "H-2") ||
		!strings.Contains(warns[0].Message, "the fork hook") {
		t.Fatalf("valid %v, warnings %+v; want valid with one handoff_not_taken for 01's H-2", v.Valid, v.Warnings)
	}
	for _, other := range []string{"01", "03"} {
		if w := itemsOf(validation(t, root, dirs[other]).Warnings, "handoff_not_taken"); len(w) != 0 {
			t.Errorf("%s is warned: %+v", other, w)
		}
	}

	before := snapshot(t, root)
	strict := validation(t, root, dirs["02"], "--strict-handoffs")
	if strict.Valid || !slices.Equal(checks(strict.Errors), []string{"handoff_not_taken"}) || len(itemsOf(strict.Warnings, "handoff_not_taken")) != 0 {
		t.Errorf("strict: valid %v, errors %+v, warnings %+v; want one handoff_not_taken error", strict.Valid, strict.Errors, strict.Warnings)
	}
	if !maps.Equal(snapshot(t, root), before) {
		t.Error("--strict-handoffs changed the spec root")
	}
	if !validation(t, root, dirs["02"]).Valid {
		t.Error("the call without the flag is no longer valid")
	}
	setTakes(t, dirs["02"], "01:H-1", "01:H-2")
	if !validation(t, root, dirs["02"], "--strict-handoffs").Valid {
		t.Error("with every handoff taken the strict call is not valid")
	}
}
