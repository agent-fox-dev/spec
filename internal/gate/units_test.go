package gate

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// slowestOf writes the output to a unit reader in pieces, as a command writes
// it, and returns what it read.
func slowestOf(output string) []string {
	var u units
	for len(output) > 0 {
		n := min(7, len(output))
		u.Write([]byte(output[:n]))
		output = output[n:]
	}
	var out []string
	for _, unit := range u.Slowest() {
		out = append(out, unit.Unit+" "+strings.TrimRight(strings.TrimRight(formatSeconds(unit.Seconds), "0"), "."))
	}
	return out
}

func formatSeconds(s float64) string { return strconv.FormatFloat(s, 'f', 3, 64) }

// Go's per-package result lines are units; the five with the most seconds are
// kept, slowest first, equal ones in output order.
func TestUnits_KeepTheFiveSlowestGoPackages(t *testing.T) {
	for name, tc := range map[string]struct {
		output string
		want   []string
	}{
		"no line": {"", nil},
		"three ok lines": {"ok  \tpkg/a\t0.100s\nok  \tpkg/b\t2.500s\nok  \tpkg/c\t1.000s\n",
			[]string{"pkg/b 2.5", "pkg/c 1", "pkg/a 0.1"}},
		"seven lines, one FAIL": {"ok  \tp/1\t1.0s\nok  \tp/2\t7.0s\nFAIL\tp/3\t3.5s\nok  \tp/4\t0.5s\nok  \tp/5\t5.0s\nok  \tp/6\t2.0s\nok  \tp/7\t6.0s\n",
			[]string{"p/2 7", "p/7 6", "p/5 5", "p/3 3.5", "p/6 2"}},
		"cached and no test files":    {"ok  \tpkg/a\t(cached)\n?   \tpkg/b\t[no test files]\n", nil},
		"text after the duration":     {"ok  \tpkg/c\t0.512s\tcoverage: 81.0% of statements\n", []string{"pkg/c 0.512"}},
		"equal seconds":               {"ok  \tpkg/x\t1.000s\nok  \tpkg/y\t1.000s\n", []string{"pkg/x 1", "pkg/y 1"}},
		"a last line with no newline": {"ok  \tpkg/a\t0.3s", []string{"pkg/a 0.3"}},
		"a test's own FAIL line and the summary": {"--- FAIL: TestX (0.50s)\nFAIL\nFAIL\tpkg/f\t0.700s\nFAIL\n",
			[]string{"pkg/f 0.7"}},
		"a build failure": {"FAIL\tpkg/b [build failed]\n", nil},
		"pytest":          {"============ 3 passed in 0.12s ============\ntests/test_x.py::test_a PASSED\n", nil},
		"jest":            {"PASS  src/a.test.js (1.234 s)\nTests:       3 passed, 3 total\nTime:        2.1 s\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := slowestOf(tc.output); !slices.Equal(got, tc.want) {
				t.Errorf("units %q; want %q", got, tc.want)
			}
		})
	}
}

// An output with no unit gives an empty list, not none.
func TestUnits_AreAnEmptyListWhenThereAreNone(t *testing.T) {
	var u units
	u.Write([]byte("nothing here\n"))
	if got := u.Slowest(); got == nil || len(got) != 0 {
		t.Errorf("Slowest() = %#v; want an empty list", got)
	}
}
