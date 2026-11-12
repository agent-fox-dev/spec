package project

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSteering_IgnoresAFileWithNothingToSay(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"bare heading", "# Steering\n"},
		{"placeholder", "# Steering\n<!-- steering:placeholder -->\n"},
		{"over the size bound", "# Steering\n\n" + strings.Repeat("x", maxSteeringBytes)},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "steering.md"), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := Steering(dir); got != "" {
				t.Errorf("Steering = %q", got)
			}
		})
	}
}

func TestSteering_IgnoresAMissingFile(t *testing.T) {
	if got := Steering(t.TempDir()); got != "" {
		t.Errorf("Steering = %q", got)
	}
}

// A symlink could point anywhere on the machine; steering is only read from a
// regular file in the specs directory.
func TestSteering_IgnoresASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(target, []byte("# Steering\n\nUse make check.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "steering.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := Steering(dir); got != "" {
		t.Errorf("Steering followed a symlink: %q", got)
	}
}

func TestSteering_ReturnsTheDirectives(t *testing.T) {
	dir := t.TempDir()
	body := "# Steering\n\nUse make check.\n"
	if err := os.WriteFile(filepath.Join(dir, "steering.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Steering(dir); got != body {
		t.Errorf("Steering = %q, want %q", got, body)
	}
}

// writeSteering writes content as the steering file of a new spec root and
// returns the root.
func writeSteering(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "steering.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const steeringBody = "# Steering\n\nUse make check.\n"

// The record of a steering at 2.0.1, as frontmatter.
const wellFormed = `---
version: 2.0.1
amendments:
  - version: 2.0.1
    date: 2026-11-06
    change: The file carries a version and this record.
  - version: 2.0.0
    date: 2026-10-25
    change: Tests are organised by behaviour.
---
`

func TestReadSteering_SplitsTheFrontmatterFromTheBody(t *testing.T) {
	t.Run("with frontmatter", func(t *testing.T) {
		s := ReadSteering(writeSteering(t, wellFormed+steeringBody))
		if s.Body != steeringBody {
			t.Errorf("Body = %q; want %q", s.Body, steeringBody)
		}
		want := []Amendment{
			{Version: "2.0.1", Date: "2026-11-06", Change: "The file carries a version and this record."},
			{Version: "2.0.0", Date: "2026-10-25", Change: "Tests are organised by behaviour."},
		}
		if s.Version != "2.0.1" || !slices.Equal(s.Amendments, want) || len(s.Problems) != 0 {
			t.Errorf("version %q, amendments %+v, problems %v", s.Version, s.Amendments, s.Problems)
		}
		if got := Steering(writeSteering(t, wellFormed+steeringBody)); got != steeringBody {
			t.Errorf("Steering = %q; want the body alone", got)
		}
	})
	t.Run("without frontmatter", func(t *testing.T) {
		s := ReadSteering(writeSteering(t, steeringBody))
		if s.Body != steeringBody || s.Version != "" || len(s.Amendments) != 0 || len(s.Problems) != 0 {
			t.Errorf("got %+v; want the file whole, unversioned, with no problem", s)
		}
	})
}

// Each rule a frontmatter can break is named once, in a fixed order, and a
// file that breaks any keeps its body and loses its version.
func TestReadSteering_NamesEveryRuleTheFrontmatterBreaks(t *testing.T) {
	amendment := func(version, date, change string) string {
		return "  - version: " + version + "\n    date: " + date + "\n    change: " + change + "\n"
	}
	front := func(version string, amendments ...string) string {
		s := "---\n"
		if version != "" {
			s += "version: " + version + "\n"
		}
		s += "amendments:\n" + strings.Join(amendments, "")
		return s + "---\n"
	}
	two := []string{amendment("2.0.0", "2026-10-25", "B."), amendment("1.0.0", "2026-10-15", "A.")}

	for _, tc := range []struct {
		name    string
		content string
		rules   []string // a phrase of each problem, in order
	}{
		{"well formed", front("2.0.0", two...), nil},
		{"an unknown key", "---\nversion: 2.0.0\nowner: x\namendments:\n" + strings.Join(two, "") + "---\n", []string{"only version, amendments and gate_budget_seconds"}},
		{"no version", front("", two...), []string{"version must be"}},
		{"no amendments key", "---\nversion: 2.0.0\n---\n", []string{"at least one amendment"}},
		{"an empty amendment list", "---\nversion: 2.0.0\namendments: []\n---\n", []string{"at least one amendment"}},
		{"a version with a leading zero", front("02.0.0", two...), []string{"version must be"}},
		{"a version with four numbers", front("2.0.0.1", two...), []string{"version must be"}},
		{"an amendment with no version", front("2.0.0", two[0], "  - date: 2026-10-15\n    change: A.\n"), []string{"every amendment must have a version"}},
		{"an amendment with no date", front("2.0.0", two[0], "  - version: 1.0.0\n    change: A.\n"), []string{"every amendment must have a date"}},
		{"an amendment with an empty change", front("2.0.0", two[0], amendment("1.0.0", "2026-10-15", `""`)), []string{"every amendment must have a change"}},
		{"a date not in YYYY-MM-DD", front("2.0.0", two[0], amendment("1.0.0", "15.10.2026", "A.")), []string{"every amendment must have a date"}},
		{"versions that do not strictly decrease", front("2.0.0", two[0], amendment("2.0.0", "2026-10-15", "A.")), []string{"strictly decrease"}},
		{"dates that increase", front("2.0.0", two[0], amendment("1.0.0", "2026-10-30", "A.")), []string{"must not increase"}},
		{"a first amendment that is not the version", front("2.1.0", two...), []string{"first amendment"}},
		{"three at once", "---\nversion: 02.0.0\nowner: x\namendments:\n" + two[0] + amendment("1.0.0", "2026-10-15", `""`) + "---\n",
			[]string{"only version, amendments and gate_budget_seconds", "version must be", "every amendment must have a change"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 3 { // the order is the same every time
				s := ReadSteering(writeSteering(t, tc.content+steeringBody))
				if len(s.Problems) != len(tc.rules) {
					t.Fatalf("problems %q; want %d naming %q", s.Problems, len(tc.rules), tc.rules)
				}
				for i, rule := range tc.rules {
					if !strings.Contains(s.Problems[i], rule) {
						t.Errorf("problem %d = %q; want it to say %q", i, s.Problems[i], rule)
					}
				}
				if s.Body != steeringBody {
					t.Errorf("Body = %q; want the body after the frontmatter", s.Body)
				}
				if wantVersion := map[bool]string{true: "2.0.0", false: ""}[tc.rules == nil]; s.Version != wantVersion {
					t.Errorf("Version = %q; want %q", s.Version, wantVersion)
				}
			}
		})
	}
}

// A frontmatter that never closes is one problem, and the whole file is the
// body.
func TestReadSteering_TakesAnUnclosedFrontmatterAsBody(t *testing.T) {
	content := "---\nversion: 2.0.0\n" + steeringBody
	s := ReadSteering(writeSteering(t, content))
	if len(s.Problems) != 1 || !strings.Contains(s.Problems[0], "closing") {
		t.Errorf("problems %q; want one naming the missing closing delimiter", s.Problems)
	}
	if s.Body != content || s.Version != "" || len(s.Amendments) != 0 {
		t.Errorf("got %+v; want the whole file as the body and no version", s)
	}
}

func TestCompareVersions_ComparesThreeIntegers(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"2.10.0", "2.9.0", 1},
		{"1.0.1", "1.0.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0.0", 0},
		{"0.0.0", "0.0.1", -1},
	} {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d; want %d", tc.a, tc.b, got, tc.want)
		}
		if got := CompareVersions(tc.b, tc.a); got != -tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d; want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

// The checks for a file with nothing to say are made on the body, and such a
// file has no version, whatever its frontmatter says.
func TestReadSteering_MeasuresTheBodyAlone(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		whole      bool
	}{
		{"empty", "", false},
		{"placeholder", "# Steering\n<!-- steering:placeholder -->\n", false},
		{"bare heading", "# Steering\n", false},
		{"one byte over the bound", strings.Repeat("x", maxSteeringBytes+1), false},
		{"within the bound with the frontmatter over it", "# Steering\n" + strings.Repeat("x", maxSteeringBytes-len("# Steering\n")), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ReadSteering(writeSteering(t, wellFormed+tc.body))
			if len(s.Problems) != 0 {
				t.Errorf("problems %q", s.Problems)
			}
			if tc.whole {
				if s.Body != tc.body || s.Version != "2.0.1" {
					t.Errorf("body of %d bytes, version %q; want the body and 2.0.1", len(s.Body), s.Version)
				}
				return
			}
			if s.Body != "" || s.Version != "" || len(s.Amendments) != 0 {
				t.Errorf("got %+v; want nothing", s)
			}
		})
	}
}

// gate_budget_seconds is a whole number above zero; anything else is a
// problem naming the key, and leaves the budget unset.
func TestReadSteering_ReadsTheGateBudget(t *testing.T) {
	front := "---\nversion: 1.0.0\n%samendments:\n  - version: 1.0.0\n    date: 2026-10-15\n    change: A.\n---\n"
	for name, tc := range map[string]struct {
		line    string
		budget  int
		problem bool
	}{
		"90":     {"gate_budget_seconds: 90\n", 90, false},
		"90.0":   {"gate_budget_seconds: 90.0\n", 90, false},
		"absent": {"", 0, false},
		"0":      {"gate_budget_seconds: 0\n", 0, true},
		"-5":     {"gate_budget_seconds: -5\n", 0, true},
		"1.5":    {"gate_budget_seconds: 1.5\n", 0, true},
		"fast":   {"gate_budget_seconds: fast\n", 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			s := ReadSteering(writeSteering(t, fmt.Sprintf(front, tc.line)+steeringBody))
			if s.GateBudgetSeconds != tc.budget {
				t.Errorf("budget %d; want %d", s.GateBudgetSeconds, tc.budget)
			}
			if tc.problem != (len(s.Problems) == 1 && strings.Contains(s.Problems[0], "gate_budget_seconds")) || (!tc.problem && len(s.Problems) > 0) {
				t.Errorf("problems %q; want one naming gate_budget_seconds: %v", s.Problems, tc.problem)
			}
		})
	}
}
