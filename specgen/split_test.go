package specgen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// A file or issue input resumes by where it came from, so an input edited
// between runs still resumes; text has no origin worth matching, so it
// resumes by content only.
func TestSplitPlan_MatchesItsInput(t *testing.T) {
	text := cli.Input{Kind: cli.KindText, Origin: "argument", Body: "one"}
	file := cli.Input{Kind: cli.KindFile, Origin: "a.md", Body: "one"}
	issue := cli.Input{Kind: cli.KindIssue, Origin: "https://github.com/a/b/issues/1", Body: "one"}
	for _, tc := range []struct {
		name        string
		planned, in cli.Input
		ok, changed bool
	}{
		{"the same text", text, text, true, false},
		{"another text", text, cli.Input{Kind: cli.KindText, Origin: "argument", Body: "two"}, false, false},
		{"the same text from stdin", text, cli.Input{Kind: cli.KindStdin, Origin: "stdin", Body: "one"}, true, false},
		{"an edited file", file, cli.Input{Kind: cli.KindFile, Origin: "a.md", Body: "two"}, true, true},
		{"another file", file, cli.Input{Kind: cli.KindFile, Origin: "b.md", Body: "two"}, false, false},
		{"an edited issue", issue, cli.Input{Kind: cli.KindIssue, Origin: issue.Origin, Body: "two"}, true, true},
		{"a file at an issue's origin", issue, cli.Input{Kind: cli.KindFile, Origin: issue.Origin, Body: "two"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := newSplitPlan(tc.planned, threeScopes())
			if ok, changed := plan.matches(tc.in); ok != tc.ok || changed != tc.changed {
				t.Errorf("matches = %v, %v; want %v, %v", ok, changed, tc.ok, tc.changed)
			}
		})
	}
}

// Every plan in the spec root that is not this input's is reported rather
// than passed over: an unreadable one, another input's, and this input's
// planned from text that has changed since.
func TestFindSplitPlan_ReportsEveryPlanItDoesNotResume(t *testing.T) {
	in := cli.Input{Kind: cli.KindFile, Origin: "docs/widgets.md", Body: "widgets"}
	for _, tc := range []struct {
		name  string
		file  string
		write func(t *testing.T, specs string)
		code  cli.WarnCode
		found bool
	}{
		{"a plan that is not JSON", "broken.split.json", func(t *testing.T, specs string) {
			writeFiles(t, specs, map[string]string{"broken.split.json": "{"})
		}, cli.WarnSplitPlanUnreadable, false},
		{"a plan of one scope", "one.split.json", func(t *testing.T, specs string) {
			writeFiles(t, specs, map[string]string{"one.split.json": `{"scopes":[{"name":"one","scope":"s"}]}`})
		}, cli.WarnSplitPlanUnreadable, false},
		{"a plan with an unusable scope name", "bad.split.json", func(t *testing.T, specs string) {
			writeFiles(t, specs, map[string]string{"bad.split.json": `{"scopes":[{"name":"bad","scope":"s"},{"name":"Bad Name","scope":"t"}]}`})
		}, cli.WarnSplitPlanUnreadable, false},
		{"another input's plan", "other_core.split.json", func(t *testing.T, specs string) {
			p := newSplitPlan(cli.Input{Kind: cli.KindFile, Origin: "docs/other.md", Body: "other"},
				[]SplitScope{{Name: "other_core", Scope: "x"}, {Name: "other_rest", Scope: "y"}})
			if err := p.save(specs); err != nil {
				t.Fatal(err)
			}
		}, cli.WarnSplitPlanForeign, false},
		{"this input's plan, planned from other text", "widget_core.split.json", func(t *testing.T, specs string) {
			if err := newSplitPlan(cli.Input{Kind: in.Kind, Origin: in.Origin, Body: "older text"}, threeScopes()).save(specs); err != nil {
				t.Fatal(err)
			}
		}, cli.WarnSplitPlanStale, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			specs := t.TempDir()
			tc.write(t, specs)
			run := cli.NewRun("spec", "test")
			plan, err := findSplitPlan(specs, in, run)
			if err != nil || (plan != nil) != tc.found {
				t.Fatalf("plan %+v, err %v; want found %v", plan, err, tc.found)
			}
			if w, ok := warning(run, tc.code); !ok || !strings.Contains(w.Message, tc.file) {
				t.Errorf("warnings = %+v, want %s naming %s", run.Warnings(), tc.code, tc.file)
			}
			if !exists(filepath.Join(specs, tc.file)) {
				t.Error("the plan was removed")
			}
		})
	}
}

// Someone else's unfinished split is reported and left alone — by a run, a
// dry run and a dry-run preflight alike — and a fresh split is not planned
// over its file.
func TestRun_LeavesAnotherInputsPlanAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context, Options) (*Result, error)
		dry  bool
	}{
		{"a run", Run, false},
		{"a dry run", Run, true},
		{"a dry-run preflight", RunPreflight, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := newWorkspace(t)
			specs := filepath.Join(ws.Root(), ".specs")
			other := newSplitPlan(cli.Input{Kind: cli.KindFile, Origin: "docs/other.md", Body: "other"},
				[]SplitScope{{Name: "other_core", Scope: "x"}, {Name: "other_rest", Scope: "y"}})
			if err := other.save(specs); err != nil {
				t.Fatal(err)
			}
			o := preflightOptions(t)
			o.Workspace, o.DryRun = ws, tc.dry
			got, err := tc.run(context.Background(), o)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(got.Split) != 0 || !exists(other.Path(specs)) {
				t.Errorf("split %+v; the other plan should be left in place", got.Split)
			}
			if w := warningText(o.Run); !strings.Contains(w, "docs/other.md") {
				t.Errorf("the stray plan should be reported: %q", w)
			}
		})
	}

	t.Run("a split planned over its file", func(t *testing.T) {
		ws := newWorkspace(t)
		specs := filepath.Join(ws.Root(), ".specs")
		other := newSplitPlan(cli.Input{Kind: cli.KindFile, Origin: "docs/other.md", Body: "other"}, threeScopes())
		if err := other.save(specs); err != nil {
			t.Fatal(err)
		}
		a := splitAuthor(t)
		_, err := Run(context.Background(), fileOptions(ws, a))
		if f := asFailure(t, err); f.Stage != "plan" || f.Category != "usage" || !strings.Contains(f.Error(), "different input") {
			t.Errorf("err = %v", err)
		}
		if len(a.steps) != 0 {
			t.Errorf("a package was generated: %v", a.steps)
		}
	})
}

// A follow-on PRD phase is told the decided split as a fact: every scope and
// its state, where the written packages live, what the earlier scopes deliver
// — their Intent, Goals and Non-goals only, from disk or this run's drafts —
// and what the earlier PRD phases learned, nearest first, within a bound.
func TestSplitBlock_StatesTheDecidedSplit(t *testing.T) {
	specsDir := t.TempDir()
	long := strings.Repeat("A widget core sentence. ", 200)
	writeFiles(t, specsDir, map[string]string{"01_widget_core/prd.md": "---\nspec_id: \"01\"\n---\n\n## Intent\n\nA widget core.\n\n" +
		"## Goals\n\n- ship it\n\n" + long + "\n\n## Design Decisions\n\n1. SECRET-DETAIL\n\n## Non-goals\n\n- no UI\n"})
	plan := &SplitPlan{Scopes: []PlanScope{
		{Name: "widget_core", Scope: "core", SpecID: "01", Dir: "01_widget_core"},
		{Name: "widget_github", Scope: "the  github\nstore"},
		{Name: "widget_adopt", Scope: "adoption"},
		{Name: "widget_later", Scope: "later"},
	}}
	ctx := &splitContext{Plan: plan, Index: 2, SpecsDir: specsDir, SpecRoot: ".specs",
		Drafts: map[int]PRD{1: {Body: "## Intent\n\nGITHUB-DRAFT\n\n## Background\n\nNOT-SHOWN\n"}},
		Notes:  map[int]string{0: "core: model.go", 1: "github: store.go"}}
	block := splitBlock(ctx)
	for _, want := range []string{
		"| 1 | `01_widget_core` | core | written |", "| 2 | `widget_github` | the github store | PRD written; its package is being generated |",
		"| 3 | `widget_adopt` | adoption | **this PRD** |", "| 4 | `widget_later` | later | not yet written |",
		"under `.specs/`", "A widget core.", "ship it", "GITHUB-DRAFT", "[…cut; the full PRD is in 01_widget_core/prd.md]",
		"Write the PRD for scope 3 only: **widget_adopt** — adoption", "`spec_name` is `widget_adopt`",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the block lacks %q:\n%s", want, block)
		}
	}
	for _, not := range []string{"SECRET-DETAIL", "NOT-SHOWN", "no UI"} {
		if strings.Contains(block, not) {
			t.Errorf("the block carries %q, past what an earlier scope contributes", not)
		}
	}
	if core, gh := strings.Index(block, "core: model.go"), strings.Index(block, "github: store.go"); core < 0 || gh < 0 || gh > core {
		t.Errorf("the earlier notes are missing or not nearest first:\n%s", block)
	}

	// Notes past the bound are left out rather than cut.
	ctx.Notes = map[int]string{0: strings.Repeat("y", maxEarlierNotes), 1: "github: store.go"}
	if b := splitBlock(ctx); strings.Contains(b, "yyy") || !strings.Contains(b, "github: store.go") {
		t.Error("the notes past the bound were not left out")
	}
	if splitBlock(nil) != "" {
		t.Error("an undivided input has a split block")
	}
}

// The review of a split scope is told the split and what the earlier scopes
// deliver, and that another scope's work is not a dropped ask, without the
// instructions for writing the PRD.
func TestSplitReviewBlock_TellsTheReviewTheSplitButNotToWriteAPRD(t *testing.T) {
	block := splitReviewBlock(scopeTwoOfThree())
	for _, want := range []string{"widget_store", "widget_cache", "widget_evict", "STORE-SCOPE", "CACHE-SCOPE",
		"EVICT-SCOPE", "This PRD covers scope 2 only", "DELIVERS-MARK", "EARLIER-NOTES-MARK", "not a dropped ask"} {
		if !strings.Contains(block, want) {
			t.Errorf("the review's split block has no %q:\n%s", want, block)
		}
	}
	for _, instruction := range []string{"Write the PRD for scope", "Submit exactly that name", "recommended_split"} {
		if strings.Contains(block, instruction) {
			t.Errorf("the review's split block carries the PRD-writing instruction %q", instruction)
		}
	}
	if splitReviewBlock(nil) != "" {
		t.Error("an undivided input has a split block")
	}
}

// A plan already gone is not an error to remove, and a plan in a spec root
// that cannot be written is.
func TestSplitPlan_SaveAndRemove(t *testing.T) {
	specs := filepath.Join(t.TempDir(), ".specs")
	plan := newSplitPlan(cli.Input{Kind: cli.KindText, Body: "x"}, threeScopes())
	if err := plan.save(specs); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSplitPlan(plan.Path(specs))
	if err != nil || loaded.Pending() != 3 || loaded.Done() || loaded.Input != plan.Input {
		t.Errorf("loaded %+v, %v", loaded, err)
	}
	for range 2 {
		if err := plan.remove(specs); err != nil {
			t.Errorf("remove: %v", err)
		}
	}
	blocked := filepath.Join(t.TempDir(), "file")
	writeFiles(t, filepath.Dir(blocked), map[string]string{"file": ""})
	if err := plan.save(filepath.Join(blocked, ".specs")); err == nil {
		t.Error("a plan was saved under a file")
	}
	if _, err := os.Stat(plan.Path(specs)); !os.IsNotExist(err) {
		t.Error("the plan is still there")
	}
}
