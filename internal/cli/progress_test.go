package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Steps of a split's scopes overlap, so each step's end names it: a reader
// can tell which of two running phases finished.
func TestOverlappingStepsEachNameTheirEnd(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, "spec", false, false, false)
	endA := p.Begin("scope 1 of 2 (a): generating tasks.json")
	endB := p.Begin("scope 2 of 2 (b): writing the PRD")
	endA("· 3 turns")
	endB("· 5 turns")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[2], "[spec] scope 1 of 2 (a): generating tasks.json done (") || !strings.HasSuffix(lines[2], "· 3 turns") {
		t.Errorf("the first step's end = %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "[spec] scope 2 of 2 (b): writing the PRD done (") {
		t.Errorf("the second step's end = %q", lines[3])
	}
}

func TestQuietPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, "spec", true, true, true)
	p.Begin("x")("y")
	p.Step("s", "z")
	p.Detail("d")
	p.Raw("r")
	if buf.Len() != 0 {
		t.Errorf("--quiet printed %q", buf.String())
	}
}
