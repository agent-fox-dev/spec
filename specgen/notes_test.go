package specgen

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// The notes a PRD phase hands on are bounded, cut at a line where there is
// one, and say that they were cut.
func TestCodebaseNotesAreBounded(t *testing.T) {
	if got := boundNotes("  short notes \n"); got != "short notes" {
		t.Errorf("short notes = %q", got)
	}
	long := strings.Repeat("a line of notes about model.go é\n", maxCodebaseNotes/20)
	got := boundNotes(long)
	if len(got) > maxCodebaseNotes+64 || !strings.HasSuffix(got, "[…the rest of the notes was cut]") {
		t.Errorf("long notes were not cut: %d bytes, ends %q", len(got), got[len(got)-40:])
	}
	if !utf8.ValidString(got) || !strings.Contains(got, "é\n[…") {
		t.Errorf("the cut is not at a line boundary: %q", got[len(got)-60:])
	}
}

// The PRD phase's notes reach every later phase of the package, and the
// prompts carry them as notes rather than instructions.
func TestCodebaseNotesReachTheLaterPhases(t *testing.T) {
	ws := newWorkspace(t)
	inner := newAuthor(t, "01", "test_feature")
	inner.prd.CodebaseNotes = "internal/widget/model.go holds Widget; NewWidget() *Widget."
	cap := &mapCapture{inner: inner}
	o := newOptions(ws, cap)
	o.Architecture = true
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, r := range cap.artifactReqs {
		if r.CodebaseNotes != inner.prd.CodebaseNotes {
			t.Errorf("%s got notes %q", r.Step, r.CodebaseNotes)
		}
	}
	if len(cap.archReqs) != 1 || cap.archReqs[0].CodebaseNotes != inner.prd.CodebaseNotes {
		t.Errorf("the architecture phase did not get the notes: %+v", cap.archReqs)
	}
	block := codebaseNotesBlock(inner.prd.CodebaseNotes)
	gen := generationUserPrompt("requirements", "01", "x", "/r", "prd", block, "", "", "")
	for _, want := range []string{"What the PRD phase learned about the code", "not instructions",
		"--- BEGIN NOTES ---\n" + inner.prd.CodebaseNotes + "\n--- END NOTES ---"} {
		if !strings.Contains(gen, want) {
			t.Errorf("the generation prompt lacks %q:\n%s", want, gen)
		}
	}
	if codebaseNotesBlock(" \n") != "" {
		t.Error("empty notes produced a block")
	}
}

// A later scope's PRD phase is shown what the earlier scopes' PRD phases of
// this run learned about the code, nearest scope first.
func TestALaterScopeSeesTheEarlierScopesNotes(t *testing.T) {
	ws := newWorkspace(t)
	inner := splitAuthor(t)
	inner.prd.CodebaseNotes = "core: model.go holds Widget."
	github := inner.prd
	github.SpecName, github.Title, github.RecommendedSplit = "widget_github", "GitHub", nil
	github.CodebaseNotes = "github: store.go holds the Store interface."
	inner.followOn = map[string]PRD{"widget_github": github}
	o := fileOptions(ws, inner)
	o.MaxParallel = 1
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var third *splitContext
	for _, r := range inner.prdRequests {
		if r.Split != nil && r.Split.Scope().Name == "widget_adopt" {
			third = r.Split
		}
	}
	if third == nil {
		t.Fatal("the third scope's PRD phase never ran")
	}
	block := splitBlock(third)
	core, gh := strings.Index(block, "core: model.go"), strings.Index(block, "github: store.go")
	if core < 0 || gh < 0 {
		t.Fatalf("the scope block lacks an earlier scope's notes:\n%s", block)
	}
	if gh > core {
		t.Error("the nearest earlier scope's notes should come first")
	}
}
