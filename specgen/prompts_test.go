package specgen

import (
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
	if got := runContextBlock("", "  \n", ""); got != "" {
		t.Errorf("an empty run context = %q, want nothing", got)
	}
	header := "\n\n# This project\n\nWhat follows is the same for every phase of this run.\n"
	if got, want := runContextBlock("", steering, ""), header+steeringBlock(steering); got != want {
		t.Errorf("without a map = %q, want %q", got, want)
	}

	run := runContextBlock("\n## Project\n\nGo.\n", steering, sampleMap)
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
