package specgen

import (
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// steering.md reaches the PRD and generation prompts without the model having
// to go looking for it (#60).
func TestSteeringReachesThePRDAndGenerationPrompts(t *testing.T) {
	block := steeringBlock("# Steering\n\nAlways write tests first.\n")
	if !strings.Contains(block, "BEGIN STEERING") || !strings.Contains(block, "Always write tests first.") {
		t.Fatalf("block = %q", block)
	}
	prd := prdUserPrompt("/r", "text", "", "idea", "", "", "", block, "", "")
	if !strings.Contains(prd, "Always write tests first.") {
		t.Error("the PRD prompt lacks the steering")
	}
	gen := generationUserPrompt("requirements", "01", "x", "/r", "prd", "", block, "", "", "", "")
	if !strings.Contains(gen, "Always write tests first.") {
		t.Error("the generation prompt lacks the steering")
	}
	if steeringBlock("  \n") != "" {
		t.Error("an empty steering produced a block")
	}
	if !strings.Contains(prdSystemPrompt(), "Steering section") {
		t.Error("the PRD system prompt does not mention steering")
	}
}

// The artifacts a later phase is shown are compact JSON: one line per
// artifact, every id still there.
func TestPriorArtifactsAreCompactJSON(t *testing.T) {
	artifacts, _ := loadFixture(t, "01", "test_feature")
	partial := afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
	for _, step := range []afspec.GenerationStep{afspec.StepRequirements, afspec.StepTestSpec} {
		if err := validateArtifactContent(artifacts[step], step, &partial); err != nil {
			t.Fatal(err)
		}
	}
	block := priorArtifactsBlock(partial, afspec.StepTasks)
	for _, fence := range strings.Split(block, "```json\n")[1:] {
		body := fence[:strings.Index(fence, "\n```")]
		if strings.Contains(body, "\n") {
			t.Errorf("an artifact spans several lines:\n%.200s", body)
		}
	}
	for _, id := range []string{"01-REQ-1.1", "TS-01-1", "01-PATH-1"} {
		if !strings.Contains(block, id) {
			t.Errorf("the block lost %s", id)
		}
	}
}
