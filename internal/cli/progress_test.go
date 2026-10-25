package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Steps of a split's scopes overlap, so each step's end names it: a reader
// can tell which of two running phases finished.
func TestProgress_OverlappingStepsEachNameTheirEnd(t *testing.T) {
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

// Steps always print; detail lines and the phase trace only under --verbose,
// the model's prose only under --show-text, and nothing at all under --quiet.
func TestProgress_PrintsWhatItsFlagsAskFor(t *testing.T) {
	emit := func(p *Progress) {
		p.Step("s", "a step")
		p.Detail("a detail")
		p.PhaseStart("prd", "m", 3, 1)
		p.Turn("prd", 1, 0.5)
		p.PhaseEnd("prd", "tool_terminate", 1, 0.5, time.Second)
		p.Raw("prose")
		p.Begin("long")("")
	}
	for _, tc := range []struct {
		name                 string
		verbose, quiet, show bool
		want, not            []string
	}{
		{"default", false, false, false, []string{"[tool] a step\n", "[tool] long done ("}, []string{"a detail", "prose", "prd:"}},
		{"verbose", true, false, false,
			[]string{"  a detail\n", "  prd: m, at most 3 turns, $1.00\n", "  prd: turn 1, $0.50 so far\n", "  prd: tool_terminate after 1 turns, $0.50, 1s\n"},
			[]string{"prose"}},
		{"show text", false, false, true, []string{"prose"}, []string{"a detail"}},
		{"quiet", true, true, true, nil, []string{"a step", "a detail", "prose", "long"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			emit(NewProgress(&buf, "tool", tc.verbose, tc.quiet, tc.show))
			out := buf.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(out, n) {
					t.Errorf("output has %q:\n%s", n, out)
				}
			}
			if tc.quiet && out != "" {
				t.Errorf("--quiet printed %q", out)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		1234 * time.Microsecond: "1ms",
		0:                       "0s",
		1500 * time.Millisecond: "2s",
		61 * time.Second:        "1m 1s",
		2 * time.Minute:         "2m",
		time.Hour + 2*time.Minute + 3*time.Second: "1h 2m 3s",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	for n, want := range map[int64]string{999: "999", 1_500: "1.5k", 2_340_000: "2.3M"} {
		if got := FormatTokens(n); got != want {
			t.Errorf("FormatTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

// A buffer is not a terminal: a program reads it.
func TestIsTerminal_NotForABuffer(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is a terminal")
	}
}
