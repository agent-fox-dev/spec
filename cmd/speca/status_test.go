package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// packageWith adds a package to .specs (dirName may name a directory under
// archive/) in the given lifecycle status, by the text of its frontmatter: a
// refusal needs the status and nothing the library would compute.
func packageWith(t *testing.T, root, dirName, id, name, status string) string {
	t.Helper()
	dir := addPackage(t, root, dirName, id, name)
	if status != "draft" {
		setStatus(t, dir, status)
	}
	return dir
}

// refusedAt runs the call and fails the test unless it is exit 1 at the stage,
// in the category, with no result and nothing written. It returns the error.
func refusedAt(t *testing.T, root string, stage, category string, argv ...string) *cli.ErrorInfo {
	t.Helper()
	before := snapshot(t, root)
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != stage || env.Error.Category != category || env.Result != nil || env.OK {
		t.Errorf("%v: code %d, error %+v, result %v; want exit 1 at stage %s, category %s, no result", argv, code, env.Error, env.Result != nil, stage, category)
		return env.Error
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Errorf("%v: the repository changed", argv)
	}
	return env.Error
}

func saysAll(t *testing.T, argv []string, message string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(message, w) {
			t.Errorf("%v: message %q does not say %q", argv, message, w)
		}
	}
}

// TS-12-16: a package that does not load, one in another format version, and a
// --by that does either, are refused at stage load, also under --force.
func TestLoadFailuresAndOtherFormatVersionsAreRefusedAtLoad_TS12_16(t *testing.T) {
	root := newRepo(t)
	bad := pkgDir(root, "05_bad")
	copyDir(t, filepath.Join("..", "..", "afspec", "testdata", "valid_spec"), bad)
	if err := os.WriteFile(filepath.Join(bad, "requirements.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	addLegacyPackage(t, root)

	for name, tc := range map[string]struct {
		argv []string
		want []string
	}{
		"the input does not load":  {[]string{"05_bad", "--status", "sealed", "--force"}, []string{"05_bad", "could not be read"}},
		"the --by does not load":   {[]string{"09_agent_mode", "--status", "superseded", "--by", "05_bad", "--force"}, []string{"--by", "05_bad", "could not be read"}},
		"the input is version 1":   {[]string{"07_legacy", "--status", "sealed", "--force"}, []string{"07_legacy", "format version 1", "format version 2 packages only"}},
		"the --by is version 1":    {[]string{"09_agent_mode", "--status", "superseded", "--by", "07_legacy", "--force"}, []string{"--by", "07_legacy", "format version 1", "format version 2 packages only"}},
		"both, the input is first": {[]string{"05_bad", "--status", "superseded", "--by", "07_legacy"}, []string{"05_bad", "could not be read"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := refusedAt(t, root, "load", "input", tc.argv...)
			if e != nil {
				saysAll(t, tc.argv, e.Message, tc.want...)
				if strings.HasPrefix(name, "the input") && strings.Contains(e.Message, "--by") {
					t.Errorf("%v: message %q names --by, and the input is at fault", tc.argv, e.Message)
				}
			}
		})
	}
}

// TS-12-18: a move the lifecycle does not allow is refused at stage status, with
// the package, its status, the target, the way to get there if there is one, and
// the moves that are allowed.
func TestADisallowedMoveIsRefusedWithThePathAndTheAllowedMoves_TS12_18(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_a", "03", "a", "active")
	packageWith(t, root, "04_s", "04", "s", "sealed")
	packageWith(t, root, "archive/05_z", "05", "z", "archived")
	packageWith(t, root, "06_d", "06", "d", "draft")

	for name, tc := range map[string]struct {
		argv  []string
		want  []string
		valid []string
	}{
		"active to archived": {[]string{"03_a", "--status", "archived", "--force"},
			[]string{"03_a", "is active", "archived", "seal it first (`speca .specs/03_a --status sealed`), then archive it"}, []string{"sealed"}},
		"sealed to sealed": {[]string{"04_s", "--status", "sealed", "--force"},
			[]string{"04_s", "already sealed"}, []string{"superseded", "archived"}},
		"archived to active": {[]string{".specs/archive/05_z", "--status", "active", "--force"},
			[]string{"05_z", "is archived", "active"}, nil},
		"draft to sealed": {[]string{"06_d", "--status", "sealed", "--force"},
			[]string{"06_d", "is draft", "sealed", "activate it first (`speca .specs/06_d --status active`), then seal it"}, []string{"active", "archived"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := refusedAt(t, root, "status", envelope.CategoryLifecycle, tc.argv...)
			if e == nil {
				return
			}
			saysAll(t, tc.argv, e.Message, tc.want...)
			switch {
			case tc.valid == nil && e.FixHint != nil:
				t.Errorf("%v: fix_hint %+v; want none, since no move leaves the status", tc.argv, e.FixHint)
			case tc.valid != nil && (e.FixHint == nil || e.FixHint.Flag != "--status" || !slices.Equal(e.FixHint.Valid, tc.valid)):
				t.Errorf("%v: fix_hint %+v; want --status with %v", tc.argv, e.FixHint, tc.valid)
			}
		})
	}
}

var statusNames = []string{"draft", "active", "sealed", "superseded", "archived"}

// shortestChain is the length of a shortest chain of allowed moves from one
// status to another, by breadth-first search over the library's table; 0 when
// there is none.
func shortestChain(from, to string) int {
	dist := map[string]int{from: 0}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range statusNames {
			if _, seen := dist[next]; !seen && afspec.ValidTransition(at, next) {
				dist[next] = dist[at] + 1
				queue = append(queue, next)
			}
		}
	}
	return dist[to]
}

var chainInMessage = regexp.MustCompile(`\b(?:draft|active|sealed|superseded|archived)(?: → (?:draft|active|sealed|superseded|archived))+`)

// TS-12-19: for any two distinct statuses the lifecycle does not link directly,
// the refusal names a shortest chain of allowed moves, and none when there is no
// chain.
func TestTheRefusalNamesAShortestChainOrNone_TS12_19(t *testing.T) {
	pairs := 0
	for _, from := range statusNames {
		for _, to := range statusNames {
			if from == to || afspec.ValidTransition(from, to) {
				continue
			}
			pairs++
			msg := refusalMessage("pkg", from, to)
			chain := chainInMessage.FindString(msg)
			want := shortestChain(from, to)
			if want == 0 {
				if chain != "" || strings.Contains(msg, "→") {
					t.Errorf("%s → %s: no chain exists, and the message names one: %q", from, to, msg)
				}
				continue
			}
			steps := strings.Split(chain, " → ")
			if len(steps)-1 != want || steps[0] != from || steps[len(steps)-1] != to {
				t.Errorf("%s → %s: the message names %q; want a chain of %d moves from %s to %s: %q", from, to, chain, want, from, to, msg)
				continue
			}
			for i := 1; i < len(steps); i++ {
				if !afspec.ValidTransition(steps[i-1], steps[i]) {
					t.Errorf("%s → %s: the chain %q holds %s → %s, which the lifecycle does not allow", from, to, chain, steps[i-1], steps[i])
				}
			}
		}
	}
	if pairs == 0 {
		t.Fatal("no pair was checked")
	}
}

// 12-REQ-3.3: a package that is already at the status asked for, which is
// archived or superseded, and is not under archive/, is not refused as "already
// there": the call only moves it. Everywhere else the lifecycle decides.
func TestAMoveOnlyCallIsAPackageAtItsTargetOutsideArchive(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status, target, dir    string
		under, moveOnly, refus bool
	}{
		{"archived, outside", "archived", "archived", "/r/.specs/03_x", false, true, false},
		{"superseded, outside", "superseded", "superseded", "/r/.specs/03_x", false, true, false},
		{"archived, under archive/", "archived", "archived", "/r/.specs/archive/03_x", true, false, true},
		{"superseded, under archive/", "superseded", "superseded", "/r/.specs/archive/03_x", true, false, true},
		{"sealed, already", "sealed", "sealed", "/r/.specs/03_x", false, false, true},
		{"a status that is not the target", "archived", "superseded", "/r/.specs/03_x", false, false, true},
		{"an allowed move", "sealed", "archived", "/r/.specs/03_x", false, false, false},
		{"an allowed move under archive/", "superseded", "archived", "/r/.specs/archive/03_x", true, false, false},
		{"a directory merely named like archive", "archived", "archived", "/r/.specs/archived/03_x", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := &statusCall{dir: tc.dir, name: "03_x", target: tc.target, spec: &afspec.Spec{Status: tc.status}}
			refusal := checkMove(sc)
			if sc.under != tc.under || sc.moveOnly != tc.moveOnly || (refusal != nil) != tc.refus {
				t.Errorf("under %v, moveOnly %v, refused %v; want %v, %v, %v", sc.under, sc.moveOnly, refusal != nil, tc.under, tc.moveOnly, tc.refus)
			}
		})
	}
}
