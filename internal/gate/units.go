package gate

import (
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/spec/envelope"
)

// SlowestUnits is how many units a full gate keeps.
const SlowestUnits = 5

// units reads a test tool's output for its units as it is written: for Go,
// the per-package result lines "ok <package> <n>s" and "FAIL <package> <n>s",
// with or without text after the duration. A line with no duration ((cached),
// [no test files], [build failed]) is not a unit, and the output of any other
// tool holds none.
type units struct {
	lineSplitter
	read []envelope.GateUnit
}

func (u *units) Write(p []byte) { u.split(p, u.line) }

func (u *units) line(line string) {
	f := strings.Fields(line)
	if len(f) < 3 || (f[0] != "ok" && f[0] != "FAIL") || !strings.HasSuffix(f[2], "s") {
		return
	}
	s, err := strconv.ParseFloat(strings.TrimSuffix(f[2], "s"), 64)
	if err != nil || s < 0 {
		return
	}
	u.read = append(u.read, envelope.GateUnit{Unit: f[1], Seconds: s})
}

// Slowest is the SlowestUnits units with the most seconds, slowest first,
// equal ones in output order; an empty list when the output held none.
func (u *units) Slowest() []envelope.GateUnit {
	if len(u.partial) > 0 {
		u.line(string(u.partial))
		u.partial = nil
	}
	out := slices.Clone(u.read)
	slices.SortStableFunc(out, func(a, b envelope.GateUnit) int {
		switch {
		case a.Seconds > b.Seconds:
			return -1
		case a.Seconds < b.Seconds:
			return 1
		}
		return 0
	})
	if out == nil {
		out = []envelope.GateUnit{}
	}
	return out[:min(len(out), SlowestUnits)]
}
