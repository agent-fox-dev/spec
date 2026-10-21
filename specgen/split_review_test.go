package specgen

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// cleanReviews is n reviews that find nothing.
func cleanReviews(n int) []reviewOutcome { return make([]reviewOutcome, n) }

func reviewedNames(rv *scriptedReviewer) []string {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	var out []string
	for _, r := range rv.reviewReqs {
		out = append(out, r.PRD.SpecName)
	}
	return out
}

func dirHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\n%s\n", e.Name(), b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TS-09-44 (integration): the total-budget check before a later scope's PRD
// phase still fails the run at stage budget.
//
// Verifies: 09-REQ-7.6
func TestTS09_44_TheScopeBudgetCheckStillFailsTheRun(t *testing.T) {
	a := splitAuthor(t)
	a.prdCost = 1
	rv := &scriptedReviewer{t: t}
	o := fileOptions(newWorkspace(t), a)
	o.Adversarial, o.AdversarialRounds, o.reviewer, o.TotalBudgetUSD = true, 1, rv, 1
	_, err := Run(context.Background(), o)
	var f *Failure
	if !errors.As(err, &f) || f.Stage != "budget" || f.Category != agentrun.CategoryBudget {
		t.Fatalf("err = %v", err)
	}
	if len(a.prdRequests) != 1 {
		t.Errorf("%d PRD phases ran", len(a.prdRequests))
	}
}

// TS-09-46 (integration): a scope's rounds run in the coordinator after the
// planned name replaces the model's, and before the scope's package is
// reserved and started, while earlier packages are still being generated.
//
// Verifies: 09-REQ-8.2
func TestTS09_46_AScopesRoundsRunBeforeItsPackageIsReserved(t *testing.T) {
	ws := newWorkspace(t)
	inner := splitAuthor(t)
	inner.followOn["widget_github"] = PRD{SpecName: "model_choice", Title: "Store", Body: inner.prd.Body}
	a := &concurrentAuthor{inner: inner}
	specsDir := filepath.Join(ws.Root(), ".specs")
	reserved := func(dir string) bool {
		_, err := os.Stat(filepath.Join(specsDir, dir))
		return err == nil
	}

	secondReviewed := make(chan struct{})
	var once sync.Once
	rv := &scriptedReviewer{t: t, reviews: cleanReviews(3)}
	rv.onReview = func(req reviewRequest) {
		a.log("review " + req.PRD.SpecName)
		if req.PRD.SpecName == "widget_github" {
			if reserved("02_widget_github") {
				t.Error("scope 2's package was reserved before its review")
			}
			once.Do(func() { close(secondReviewed) })
		}
	}
	a.beforeGenerate = func(req artifactRequest) {
		if req.Step != afspec.StepRequirements {
			return
		}
		a.log("gen " + req.SpecName)
		// The first package is still being generated while the coordinator
		// reviews the second scope's PRD: the split stays pipelined.
		if req.SpecName == "widget_core" {
			wait(t, secondReviewed, "the second scope's review")
		}
	}
	o := fileOptions(ws, a)
	o.MaxParallel = 3
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if names := reviewedNames(rv); !slices.Equal(names, []string{"widget_core", "widget_github", "widget_adopt"}) {
		t.Errorf("reviewed %v", names)
	}
	if r, g := a.indexOf("review widget_github"), a.indexOf("gen widget_github"); r < 0 || g < 0 || r > g {
		t.Errorf("scope 2: review at %d, generation at %d: %v", r, g, a.events)
	}
}

// TS-09-47 (integration): the first scope is reviewed after the split plan is
// recorded, with a split context for scope 1.
//
// Verifies: 09-REQ-8.3
func TestTS09_47_TheFirstScopeIsReviewedWithItsSplitContext(t *testing.T) {
	ws := newWorkspace(t)
	planPath := filepath.Join(ws.Root(), ".specs", "widget_core"+SplitPlanSuffix)
	rv := &scriptedReviewer{t: t, reviews: cleanReviews(3)}
	rv.onReview = func(req reviewRequest) {
		if req.PRD.SpecName != "widget_core" {
			return
		}
		if _, err := os.Stat(planPath); err != nil {
			t.Errorf("the plan is not on disk when scope 1 is reviewed: %v", err)
		}
		if req.Split == nil || req.Split.Index != 0 || len(req.Split.Plan.Scopes) != 3 {
			t.Errorf("scope 1's review has split %+v", req.Split)
		}
	}
	o := fileOptions(ws, splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n, _ := rv.counts(); n != 3 {
		t.Errorf("%d reviews", n)
	}
}

// scopeOneRevised scripts a split whose first scope's PRD is revised to
// REVISED-1 with notes NOTES-REV, the others reviewed clean.
func scopeOneRevised(t *testing.T) *scriptedReviewer {
	return &scriptedReviewer{t: t,
		reviews: []reviewOutcome{{severities: []string{"minor"}}, {}, {}},
		revisions: []revisionOutcome{{edit: func(p PRD) PRD {
			p.Body = "## Intent\n\nREVISED-1: the widget model.\n\n## Goals\n\n- One model.\n"
			p.CodebaseNotes = "NOTES-REV"
			return p
		}}},
	}
}

// TS-09-48 (integration): the next scope's PRD phase is shown the earlier
// scope's revised PRD and notes, not the originals.
//
// Verifies: 09-REQ-8.4
func TestTS09_48_TheNextScopeSeesTheRevisedPRD(t *testing.T) {
	// A dry run keeps every earlier PRD in memory, so the draft itself is
	// what the next scope is shown.
	a := splitAuthor(t)
	a.prd.CodebaseNotes = "NOTES-ORIGINAL"
	o := fileOptions(newWorkspace(t), a)
	o.DryRun = true
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, scopeOneRevised(t)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if len(a.prdRequests) != 3 {
		t.Fatalf("%d PRD phases", len(a.prdRequests))
	}
	split := a.prdRequests[1].Split
	if split == nil || !strings.Contains(split.Drafts[0].Body, "REVISED-1") || split.Notes[0] != "NOTES-REV" {
		t.Errorf("scope 2 was shown %+v", split)
	}

	// Written to disk, the earlier scope is read from its prd.md, which is
	// the revised PRD.
	ws := newWorkspace(t)
	a = splitAuthor(t)
	o = fileOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, scopeOneRevised(t)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if block := earlierScopesBlock(a.prdRequests[1].Split); !strings.Contains(block, "REVISED-1") {
		t.Errorf("scope 2 was shown:\n%s", block)
	}
}

// TS-09-49 (integration): a resumed split reviews only the scopes it writes,
// and leaves the packages of the earlier run as they were.
//
// Verifies: 09-REQ-8.5
func TestTS09_49_AResumedSplitReviewsOnlyWhatItWrites(t *testing.T) {
	ws := newWorkspace(t)
	a := splitAuthor(t)
	a.followOnErr["widget_github"] = errors.New("budget exceeded")
	o := fileOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, &scriptedReviewer{t: t, reviews: cleanReviews(1)}
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("the first run should stop at scope 2")
	}
	first := filepath.Join(ws.Root(), ".specs", "01_widget_core")
	before := dirHash(t, first)

	rv := &scriptedReviewer{t: t, reviews: cleanReviews(2)}
	o = fileOptions(ws, splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	got, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if names := reviewedNames(rv); !slices.Equal(names, []string{"widget_github", "widget_adopt"}) {
		t.Errorf("the resumed run reviewed %v", names)
	}
	if dirHash(t, first) != before {
		t.Error("the earlier run's package changed")
	}
	if got.SpecID != "02" || got.Review == nil || len(got.FollowOnSpecs) != 1 || got.FollowOnSpecs[0].Review == nil {
		t.Errorf("the resumed run reports %s %+v and %+v", got.SpecID, got.Review, got.FollowOnSpecs)
	}
}

// TS-09-50 (integration): every package the result reports carries its
// review: the first and each follow-on of a split, and an undivided
// input's package whose generation failed. A split scope that fails before
// its write reports no package (D-3), so it has none to carry one.
//
// Verifies: 09-REQ-9.1
func TestTS09_50_EveryReportedPackageCarriesItsReview(t *testing.T) {
	a := &concurrentAuthor{inner: splitAuthor(t), failGenerate: map[string]error{"widget_adopt": errors.New("no room")}}
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}}, {}, {}},
		revisions: []revisionOutcome{{}}}
	o := fileOptions(newWorkspace(t), a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 2, rv
	got, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("the third scope should fail")
	}
	if got.Review == nil || got.Review.RoundsAllowed != 2 || got.Review.StopReason != envelope.ReviewStopNoSeriousFindings ||
		got.Review.FindingsCount != 1 || got.Review.Rounds[0].Round != 1 || len(got.Review.Rounds[0].Findings) != 1 {
		t.Errorf("the first package's review = %+v", got.Review)
	}
	if len(got.FollowOnSpecs) != 1 || got.FollowOnSpecs[0].Review == nil ||
		got.FollowOnSpecs[0].Review.StopReason != envelope.ReviewStopNoFindings {
		t.Errorf("follow-on packages = %+v", got.FollowOnSpecs)
	}

	u := newAuthor(t, "01", "test_feature")
	u.artifactErr[afspec.StepTestSpec] = errors.New("no room")
	o = adversarialOptions(t, u, &scriptedReviewer{t: t, reviews: cleanReviews(1)}, 1)
	got, err = Run(context.Background(), o)
	if err == nil || got.SpecDir != "" || got.SpecName != "test_feature" || got.Review == nil ||
		got.Review.StopReason != envelope.ReviewStopNoFindings {
		t.Errorf("an unwritten package: err %v, result %+v", err, got)
	}
}
