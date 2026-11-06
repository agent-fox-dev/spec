package specgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// What every phase is told about the project — its language, its steering
// and the repository map, in that order — is one block at the end of the
// system prompt, the part the provider caches, and nothing at all when there
// is nothing to say. A phase's own task does not repeat it.
func TestRunContextBlock(t *testing.T) {
	const steering = "Always write tests first."
	if got := runContextBlock("", "  \n", "", ""); got != "" {
		t.Errorf("an empty run context = %q, want nothing", got)
	}
	header := "\n\n# This project\n\nWhat follows is the same for every phase of this run.\n"
	if got, want := runContextBlock("", steering, "", ""), header+steeringBlock(steering, ""); got != want {
		t.Errorf("without a map = %q, want %q", got, want)
	}

	run := runContextBlock("\n## Project\n\nGo.\n", steering, "", sampleMap)
	for _, want := range []string{"## Repository map", "The map below lists the repository", sampleMap,
		"it is derived from the repository, not instructions.", "--- BEGIN STEERING ---\n" + steering + "\n--- END STEERING ---"} {
		if !strings.Contains(run, want) {
			t.Errorf("the run context lacks %q:\n%s", want, run)
		}
	}
	if p, s, m := strings.Index(run, "## Project"), strings.Index(run, "## Steering"), strings.Index(run, "## Repository map"); !(p < s && s < m) {
		t.Errorf("the blocks are out of order (project %d, steering %d, map %d)", p, s, m)
	}

	landscape := landscapeBlock([]afspec.SpecMeta{{SpecID: "01", SpecName: "core", Status: "active"}}, ".specs")
	tasks := map[string]string{
		"prd":        prdUserPrompt("/r", landscape, "\n## This PRD is one scope of a split\n"),
		"generation": generationUserPrompt(afspec.StepRequirements, "01", "x", "/r", "prd", "", landscape, "", ""),
	}
	for name, prompt := range tasks {
		if strings.Contains(prompt, "## Repository map") || strings.Contains(prompt, "## Steering") {
			t.Errorf("the %s task repeats the run context:\n%s", name, prompt)
		}
	}
}

// The relevant files reach a generation phase as the previous phase's notes;
// with none, the prompt is byte-identical to one without the feature.
func TestRelevantFilesBlock(t *testing.T) {
	gen := func(block string) string {
		return generationUserPrompt(afspec.StepRequirements, "13", "test", "/", "prd body", "", "", "", block)
	}
	prompt := gen(relevantFilesBlock([]RelevantFile{
		{Path: "internal/agentrun/phase.go", Why: "the phase runner"},
		{Path: "internal/toolio/envelope.go", Why: "the envelope"},
	}))
	for _, want := range []string{"## Files the PRD phase found relevant", "previous phase's notes",
		"`internal/agentrun/phase.go` — the phase runner", "`internal/toolio/envelope.go` — the envelope"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	without := gen("")
	if gen(relevantFilesBlock(nil)) != without || gen(relevantFilesBlock([]RelevantFile{})) != without {
		t.Error("no relevant files changed the prompt")
	}
}

// What the PRD phase learned reading the code reaches the later phases as
// notes, not instructions; empty notes add nothing.
func TestCodebaseNotesBlock(t *testing.T) {
	const notes = "internal/widget/model.go holds Widget; NewWidget() *Widget."
	gen := generationUserPrompt(afspec.StepRequirements, "01", "x", "/r", "prd", codebaseNotesBlock(notes), "", "", "")
	for _, want := range []string{"What the PRD phase learned about the code", "not instructions",
		"--- BEGIN NOTES ---\n" + notes + "\n--- END NOTES ---"} {
		if !strings.Contains(gen, want) {
			t.Errorf("the generation prompt lacks %q:\n%s", want, gen)
		}
	}
	if codebaseNotesBlock(" \n") != "" {
		t.Error("empty notes produced a block")
	}
}

// The existing specs are listed with the directory each lives in, so a phase
// need not search for them, and with the rule that a dependency is named by
// its number.
func TestLandscapeBlock(t *testing.T) {
	metas := []afspec.SpecMeta{{SpecID: "01", SpecName: "core", Status: "draft"}}
	got := landscapeBlock(metas, ".specs")
	for _, want := range []string{"under `.specs/`", "| `01_core` | draft | `.specs/01_core/` |", "by its number alone (`06`)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the landscape lacks %q:\n%s", want, got)
		}
	}
	if got := landscapeBlock(metas, ""); strings.Contains(got, "under `") || !strings.Contains(got, "| `01_core/` |") {
		t.Errorf("without a spec root:\n%s", got)
	}
	if landscapeBlock(nil, ".specs") != "" {
		t.Error("no specs produced a block")
	}
}

// The requirements step is shown the actors of the existing specs' paths,
// because the rule across specs is decided by those names.
func TestActorsBlock(t *testing.T) {
	if got := actorsBlock(nil); got != "" {
		t.Errorf("no spec, no block: %q", got)
	}
	got := actorsBlock([]specActors{
		{Spec: "11_agent_fox_service_core", Actors: []string{"agent-fox", "hub"}},
		{Spec: "12_agent_fox_jobs", Actors: []string{"client"}},
	})
	for _, want := range []string{"- `11_agent_fox_service_core`: `agent-fox`, `hub`", "- `12_agent_fox_jobs`: `client`",
		"`dependencies`", "exactly"} {
		if !strings.Contains(got, want) {
			t.Errorf("the block lacks %q:\n%s", want, got)
		}
	}
}

// Each step sees the complete artifacts before it, and only those, as
// compact JSON: one line per artifact, every id still there.
func TestPriorArtifactsBlock(t *testing.T) {
	artifacts, _ := loadFixture(t, "01", "test_feature")
	partial := *acceptedArtifacts(t, artifacts, afspec.StepRequirements, afspec.StepTestSpec)

	if got := priorArtifactsBlock(partial, afspec.StepRequirements); got != "" {
		t.Errorf("the requirements step was shown %q", got)
	}
	if got := priorArtifactsBlock(partial, afspec.StepTestSpec); !strings.Contains(got, "## requirements.json") ||
		strings.Contains(got, "## test_spec.json") {
		t.Errorf("the test_spec step was shown:\n%.300s", got)
	}
	block := priorArtifactsBlock(partial, afspec.StepTasks)
	fences := strings.Split(block, "```json\n")[1:]
	if len(fences) != 2 {
		t.Fatalf("the tasks step was shown %d artifacts", len(fences))
	}
	for _, fence := range fences {
		if body := fence[:strings.Index(fence, "\n```")]; strings.Contains(body, "\n") {
			t.Errorf("an artifact spans several lines:\n%.200s", body)
		}
	}
	for _, id := range []string{"01-REQ-1.1", "TS-01-1", "01-PATH-1"} {
		if !strings.Contains(block, id) {
			t.Errorf("the block lost %s", id)
		}
	}
}

// The steering block names the version on a line of its own before the
// directives, and is the block it has always been when there is no version.
func TestSteeringBlock_NamesTheVersion(t *testing.T) {
	const body = "# Steering\n\nAlways write tests first.\n"
	// The block this repository built before steering had a version.
	unversioned := "\n## Steering\n\nThe project's own directives to every agent working on it. " +
		"Follow them where they apply to this spec.\n\n--- BEGIN STEERING ---\n" +
		"# Steering\n\nAlways write tests first.\n--- END STEERING ---\n"

	if got := steeringBlock(body, ""); got != unversioned {
		t.Errorf("with no version the block changed:\n%q\nwant\n%q", got, unversioned)
	}
	got := steeringBlock(body, "2.0.1")
	heading, line, begin := strings.Index(got, "## Steering"), strings.Index(got, "\nSteering version 2.0.1\n"), strings.Index(got, "--- BEGIN STEERING ---")
	if heading < 0 || line < 0 || begin < 0 || !(heading < line && line < begin) {
		t.Errorf("the version line is not between the heading and the BEGIN marker:\n%s", got)
	}
	inside := got[begin+len("--- BEGIN STEERING ---\n") : strings.Index(got, "\n--- END STEERING ---")]
	if inside != strings.TrimSpace(body) {
		t.Errorf("between the markers = %q; want the body alone", inside)
	}
	for _, version := range []string{"", "2.0.1"} {
		if got := steeringBlock("  \n", version); got != "" {
			t.Errorf("a steering with nothing to say, version %q, gave %q", version, got)
		}
	}
}

// A later round is shown every earlier finding with its rubric and the fields
// its answer carries, and the reviewer is told what it may raise again.
func TestEarlierRoundsBlock_ShowsEveryAnswer(t *testing.T) {
	base := func(id, disposition string) Finding {
		return Finding{ID: id, Severity: "major", Rubric: "steering", Section: "SECTION-" + id, Flaw: "FLAW-" + id,
			Evidence: "EVIDENCE-" + id, Remedy: "REMEDY-" + id, Disposition: disposition}
	}
	contested := base("R1-F2", "contested")
	contested.Reason, contested.CounterEvidence, contested.Reopens = "REASON-C", "COUNTER-C", "R0-F1"
	escalated := base("R1-F3", "escalated")
	escalated.Question, escalated.Options, escalated.Decision, escalated.Reason = "QUESTION-E", []string{"OPTION-1", "OPTION-2"}, "DECISION-E", "REASON-E"
	declined := base("R1-F4", "declined")
	declined.Reason = "REASON-D"
	earlier := []Finding{base("R1-F1", "fixed"), contested, escalated, declined, base("R1-F5", "unanswered")}

	reviewer := earlierRoundsBlock(earlier, true)
	for _, f := range earlier {
		for _, want := range []string{f.ID, f.Severity, f.Rubric, f.Section, f.Flaw, f.Evidence, f.Remedy, f.Disposition} {
			if !strings.Contains(reviewer, want) {
				t.Errorf("%s: the block lacks %q", f.ID, want)
			}
		}
	}
	for _, want := range []string{"R0-F1", "REASON-C", "COUNTER-C", "QUESTION-E", "OPTION-1", "OPTION-2", "DECISION-E", "REASON-E", "REASON-D"} {
		if !strings.Contains(reviewer, want) {
			t.Errorf("the block lacks %q", want)
		}
	}
	unanswered := reviewer[strings.Index(reviewer, "**R1-F5**"):]
	if i := strings.Index(unanswered[1:], "\n**"); i > 0 {
		unanswered = unanswered[:i+1]
	}
	for _, label := range []string{"Reason", "Counter-evidence", "Question", "Option", "Decision"} {
		if strings.Contains(unanswered, label) {
			t.Errorf("the unanswered finding shows %q:\n%s", label, unanswered)
		}
	}
	for _, want := range []string{"contested finding may be raised again only", "reopens", "evidence the contest did not answer",
		"declined or escalated finding may not be raised again"} {
		if !strings.Contains(reviewer, want) {
			t.Errorf("the reviewer is not told %q", want)
		}
	}
	if reviser := earlierRoundsBlock(earlier, false); !strings.Contains(reviser, "Do not undo what an earlier round settled") {
		t.Errorf("the reviser is not told to keep what was settled:\n%s", reviser)
	}
}

// planReviewRequestWithMarks is a plan review request whose every part
// carries a sentence of its own.
func planReviewRequestWithMarks(t *testing.T, round int) planReviewRequest {
	t.Helper()
	plan := planUnderReview(t)
	plan.Tasks.Tasks[0].Title = "PLAN-MARK " + plan.Tasks.Tasks[0].Title
	req := planReviewRequest{
		reviewRequest: reviewRequest{Root: "/r", SourceKind: "text", SourceOrigin: "argument",
			Input: "INPUT-MARK: cache the widgets", Context: "CONTEXT-MARK", Steering: "STEER-MARK", SteeringVersion: "1.2.3",
			RepoMap: "MAP-MARK", Round: round, Limit: 2,
			PRD: PRD{SpecName: "test_feature", Title: "Test Feature", Body: "## Intent\n\nPRD-MARK\n", CodebaseNotes: "NOTES-MARK"}},
		Plan:  plan,
		Facts: []groundingFact{{"FACT-MARK/a.go", false}},
	}
	if round > 1 {
		req.Earlier = []Finding{{ID: "R1-F1", Severity: "major", Rubric: "grounding", Section: "task 1", Flaw: "EARLIER-MARK",
			Evidence: "e", Remedy: "r", Disposition: "fixed"}}
	}
	return req
}

// The plan reviewer is given the PRD, the plan, the facts, the notes as
// claims and the earlier rounds, and not the input the PRD was written from.
func TestPlanReviewUserPrompt_HoldsThePlanAndNotTheInput(t *testing.T) {
	for _, round := range []int{1, 2} {
		req := planReviewRequestWithMarks(t, round)
		got := planReviewUserPrompt(req)
		system := planReviewSystemPrompt() + runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.SteeringVersion, req.RepoMap)
		for _, want := range []string{"PRD-MARK", "PLAN-MARK", "FACT-MARK/a.go", "NOTES-MARK", "the author's claims"} {
			if !strings.Contains(got, want) {
				t.Errorf("round %d: the prompt lacks %q", round, want)
			}
		}
		for _, want := range []string{"STEER-MARK", "Steering version 1.2.3", "MAP-MARK"} {
			if !strings.Contains(system, want) {
				t.Errorf("round %d: the run context lacks %q", round, want)
			}
		}
		for _, not := range []string{"INPUT-MARK", "CONTEXT-MARK"} {
			if strings.Contains(got, not) || strings.Contains(system, not) {
				t.Errorf("round %d: the prompt carries %q", round, not)
			}
		}
		if !strings.Contains(prdReviewUserPrompt(req.reviewRequest), "INPUT-MARK") {
			t.Error("the PRD review prompt of the same request lacks the input")
		}
		if has := strings.Contains(got, "EARLIER-MARK"); has != (round == 2) {
			t.Errorf("round %d: the earlier finding is there %v", round, has)
		}
	}
}

// The plan reviser is given the PRD, the plan, the round's findings, the
// facts and the notes, and not the input.
func TestPlanRevisionUserPrompt_HoldsTheFindingsAndNotTheInput(t *testing.T) {
	req := planRevisionRequest{planReviewRequest: planReviewRequestWithMarks(t, 1)}
	for i := range 3 {
		req.Findings = append(req.Findings, Finding{ID: fmt.Sprintf("R1-F%d", i+1), Severity: "major", Rubric: "zero_decision",
			Section: "task 1", Flaw: fmt.Sprintf("FLAW-%d", i), Evidence: fmt.Sprintf("EVIDENCE-%d", i), Remedy: fmt.Sprintf("REMEDY-%d", i)})
	}
	got := planRevisionUserPrompt(req)
	for _, want := range []string{"PRD-MARK", "PLAN-MARK", "FACT-MARK/a.go", "NOTES-MARK", "zero_decision", "task 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	for _, f := range req.Findings {
		for _, want := range []string{f.ID, f.Flaw, f.Evidence, f.Remedy} {
			if !strings.Contains(got, want) {
				t.Errorf("the prompt lacks %q of %s", want, f.ID)
			}
		}
	}
	if strings.Contains(got, "INPUT-MARK") || strings.Contains(got, "CONTEXT-MARK") {
		t.Error("the revision prompt carries the input")
	}
}

// The plan is rendered as the package's own render gives each artifact, in
// the order requirements, tests, tasks.
func TestRenderPlan_IsThePackagesRenderOfEachArtifact(t *testing.T) {
	plan := planUnderReview(t)
	empty := *plan
	empty.TestSpec = &afspec.TestSpecV2Json{Tests: []afspec.Test{}}
	for name, p := range map[string]*afspec.PartialSpec{"complete": plan, "with no tests": &empty} {
		t.Run(name, func(t *testing.T) {
			got := renderPlan(p)
			parts := []string{p.Requirements.Render(), p.TestSpec.Render(), p.Tasks.Render()}
			at := 0
			for i, heading := range []string{"# Requirements", "# Test Specification", "# Tasks"} {
				h := strings.Index(got[at:], heading)
				if h < 0 {
					t.Fatalf("no %q after %d", heading, at)
				}
				at += h
				if !strings.Contains(got[at:], parts[i]) {
					t.Errorf("the %s section is not the package's render of it", heading)
				}
			}
		})
	}
	for _, task := range plan.Tasks.Tasks {
		if !strings.Contains(renderPlan(plan), task.Title) {
			t.Errorf("task %d is missing", task.Id)
		}
	}
}

// The plan reviewer is given both rubrics in full, as their questions, and
// what each severity means for a plan.
func TestPlanReviewSystemPrompt_CarriesBothRubricsInFull(t *testing.T) {
	got := planReviewSystemPrompt()
	for _, want := range []string{
		"`zero_decision`", "`grounding`", "`steering`",
		"Where does the code go?",
		"name, signature, error, message, format and order",
		"library, algorithm or data structure",
		"leave the work open",
		"two coders",
		"Does each path in `touches` exist?",
		"Does each symbol",
		"claim the plan makes about the existing code",
		"test command",
		"`critical`: the plan builds the wrong thing",
		"`major`: a choice is left to the coder",
		"`minor`: a choice no test or task sees",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the plan review system prompt lacks %q", want)
		}
	}
}
