package afspec

import (
	"strings"
	"testing"
)

func TestComputeIntentHash_ChangesOnlyWithTheIntentSection(t *testing.T) {
	body := "# Title\n\n## Intent\n\nThe intent text.\n\n## Goals\n\n- something\n"
	first, err := ComputeIntentHash(body)
	if err != nil {
		t.Fatalf("ComputeIntentHash = %v", err)
	}
	if len(first) != 64 {
		t.Errorf("hash length = %d; want a 64-character SHA-256 hex digest", len(first))
	}

	second, err := ComputeIntentHash(strings.Replace(body, "- something", "- something else", 1))
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Error("editing a non-Intent section changed the intent hash")
	}

	drifted, err := ComputeIntentHash(strings.Replace(body, "The intent text.", "Something else.", 1))
	if err != nil {
		t.Fatal(err)
	}
	if drifted == first {
		t.Error("editing the Intent section did not change the hash")
	}
}

func TestComputeIntentHash_RequiresTheSection(t *testing.T) {
	if _, err := ComputeIntentHash("# Title\n\nNo intent section here.\n"); err == nil {
		t.Fatal("a body with no ## Intent section produced a hash")
	}
}

// HasIntent accepts a body exactly when ComputeIntentHash can hash it, so a
// PRD that passes the check can be activated.
func TestHasIntent_AgreesWithComputeIntentHash(t *testing.T) {
	for _, body := range []string{
		"## Intent\n\nWhy it exists.\n",
		"## Intent  \nWhy.\n",
		"## intent\n\nWhy.\n",
		"##  Intent\n\nWhy.\n",
		"## INTENT\nWhy.\n",
		"# Intent\nWhy.\n",
		"No intent here.\n",
	} {
		_, err := ComputeIntentHash(body)
		if got := HasIntent(body); got != (err == nil) {
			t.Errorf("HasIntent(%q) = %v, but ComputeIntentHash says err=%v", body, got, err)
		}
	}
	if HasIntent("## intent\n\nWhy.\n") {
		t.Error("a lower-case heading is accepted, and activation would refuse it")
	}
}
