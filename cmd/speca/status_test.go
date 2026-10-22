package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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

// activePackage adds a package, activates it through the library, which records
// the hash of its intent, and gives it the plan.
func activePackage(t *testing.T, root, dirName, id, name string, tasks ...map[string]any) string {
	t.Helper()
	dir := addPackage(t, root, dirName, id, name)
	activate(t, dir)
	if len(tasks) > 0 {
		setTasks(t, dir, tasks...)
	}
	return dir
}

// withFrontmatterKey adds a key of its own to the frontmatter of prd.md, which
// the schema does not allow and a write would drop.
func withFrontmatterKey(t *testing.T, dir string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "prd.md"), "schema_version: 2", "schema_version: 2\nbadkey: \"x\"")
}

// succeeds runs the call and fails the test unless it exits 0 with a result.
func succeeds(t *testing.T, root string, argv ...string) (envelope.AdminEnvelope, *envelope.AdminResult) {
	t.Helper()
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitOK || !env.OK || env.Result == nil {
		t.Fatalf("%v: code %d, error %+v", argv, code, env.Error)
	}
	return env, env.Result
}

func statusOnDisk(t *testing.T, dir string) string {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	return spec.Status
}

// oneWarning returns the only warning with the code, or fails the test.
func oneWarning(t *testing.T, env envelope.AdminEnvelope, code, severity string) envelope.Warning {
	t.Helper()
	ws := warningsOf(env, code)
	if len(ws) != 1 || ws[0].Severity != severity {
		t.Fatalf("warnings %+v; want one %s warning of severity %s", env.Warnings, code, severity)
	}
	return ws[0]
}

// TS-12-20: sealing, archiving and superseding refuse a package that holds keys
// the schema does not allow, in prd.md or in the other artifacts, naming each.
func TestUnknownKeysRefuseSealingAndArchiving_TS12_20(t *testing.T) {
	root := newRepo(t)
	a := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	withFrontmatterKey(t, a)
	s := packageWith(t, root, "04_s", "04", "s", "sealed")
	addUnknownKey(t, s)

	for _, tc := range []struct {
		argv []string
		want []string
	}{
		{[]string{"03_a", "--status", "sealed"}, []string{"prd.md", "badkey"}},
		{[]string{"04_s", "--status", "archived"}, []string{"tasks.json", "traceability"}},
	} {
		e := refusedAt(t, root, "status", envelope.CategoryLifecycle, tc.argv...)
		if e == nil {
			continue
		}
		saysAll(t, tc.argv, e.Message, tc.want...)
		if e.FixHint == nil || e.FixHint.Flag != "--force" {
			t.Errorf("%v: fix_hint %+v; want --force", tc.argv, e.FixHint)
		}
	}
}

// TS-12-21: --force goes ahead past unknown keys, with a high spec_invalid
// warning that names them, and the written package holds none.
func TestForceSealsPastUnknownKeys_TS12_21(t *testing.T) {
	root := newRepo(t)
	a := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	withFrontmatterKey(t, a)

	env, r := succeeds(t, root, "03_a", "--status", "sealed", "--force")
	if r.Status != "sealed" || statusOnDisk(t, a) != "sealed" {
		t.Errorf("status %q, on disk %q; want sealed", r.Status, statusOnDisk(t, a))
	}
	w := oneWarning(t, env, "spec_invalid", "high")
	if !strings.Contains(w.Message, "badkey") || w.Stage != "validate" {
		t.Errorf("warning %+v; want stage validate, naming badkey", w)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "prd.md")); strings.Contains(string(b), "badkey") {
		t.Error("the key is still in prd.md")
	}
}

// TS-12-23: a draft that validates and has an intent is activated: the status
// and the hash of the intent are written, and the package stays where it is.
func TestActivationWritesTheStatusAndTheHash_TS12_23(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")

	_, r := succeeds(t, root, "09", "--status", "active")
	if r.Status != "active" || r.IntentHash == "" || r.SpecDir != ".specs/09_agent_mode" {
		t.Errorf("status %q, intent_hash %q, spec_dir %q; want active, a hash, .specs/09_agent_mode", r.Status, r.IntentHash, r.SpecDir)
	}
	if want := []envelope.Change{{Kind: "status", From: "draft", To: "active"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	spec, err := afspec.LoadSpec(dir)
	if err != nil || spec.Status != "active" || spec.IntentHash == nil || *spec.IntentHash != r.IntentHash {
		t.Errorf("on disk: %+v, %v", spec, err)
	}
}

// TS-12-24: a draft with no ## Intent cannot be activated, and --force does not
// change that.
func TestActivationWithoutAnIntentIsRefused_TS12_24(t *testing.T) {
	root := newRepo(t)
	editFile(t, filepath.Join(pkgDir(root, "09_agent_mode"), "prd.md"), "## Intent", "## Purpose")

	e := refusedAt(t, root, "status", envelope.CategoryLifecycle, "09", "--status", "active", "--force")
	if e != nil {
		saysAll(t, nil, e.Message, "## Intent", "on a line of its own", "prd.md")
	}
}

// breakValidity makes the example fail validation without touching its intent:
// a test the plan names is not in the test specification.
func breakValidity(t *testing.T, root string) {
	t.Helper()
	editFile(t, filepath.Join(pkgDir(root, "09_agent_mode"), "tasks.json"), `"TS-09-6"`, `"TS-09-99"`)
}

// TS-12-25: a draft that does not validate is refused at stage validate, naming
// the failing rules, with --force as the way past.
func TestActivationOfAnInvalidPackageIsRefused_TS12_25(t *testing.T) {
	root := newRepo(t)
	breakValidity(t, root)

	e := refusedAt(t, root, "validate", envelope.CategoryInvalidSpec, "09", "--status", "active")
	if e == nil {
		return
	}
	saysAll(t, nil, e.Message, "09_agent_mode", "does not validate", "C6")
	if e.FixHint == nil || e.FixHint.Flag != "--force" {
		t.Errorf("fix_hint %+v; want --force", e.FixHint)
	}
}

// TS-12-26: --force activates it, with a high spec_invalid warning naming the
// rules.
func TestForceActivatesAnInvalidPackage_TS12_26(t *testing.T) {
	root := newRepo(t)
	breakValidity(t, root)

	env, r := succeeds(t, root, "09", "--status", "active", "--force")
	if r.Status != "active" {
		t.Errorf("status %q; want active", r.Status)
	}
	if w := oneWarning(t, env, "spec_invalid", "high"); !strings.Contains(w.Message, "C6") {
		t.Errorf("warning %q does not name the failing rules", w.Message)
	}
}

// 12-REQ-3.6 applies to the frontmatter of prd.md at activation too: the library
// reports no key of the frontmatter, and the write drops it, so the activation is
// refused as one that does not validate, and --force names the key.
func TestActivationRefusesAFrontmatterKeyTheLibraryDoesNotReport(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	withFrontmatterKey(t, dir)

	e := refusedAt(t, root, "validate", envelope.CategoryInvalidSpec, "09", "--status", "active")
	if e != nil {
		saysAll(t, nil, e.Message, "unknown_field", "prd.md", "badkey")
	}
	env, r := succeeds(t, root, "09", "--status", "active", "--force")
	if w := oneWarning(t, env, "spec_invalid", "high"); !strings.Contains(w.Message, "badkey") || r.Status != "active" {
		t.Errorf("warning %q, status %q; want one naming badkey, and active", w.Message, r.Status)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prd.md")); strings.Contains(string(b), "badkey") {
		t.Error("the key is still in prd.md")
	}
}

// TS-12-28: an active package with no drift and no open task is sealed.
func TestSealingWritesTheStatus_TS12_28(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil), planTask(2, "dropped", []int{}))

	_, r := succeeds(t, root, "03_a", "--status", "sealed")
	if r.Status != "sealed" || r.SpecDir != ".specs/03_a" || statusOnDisk(t, dir) != "sealed" {
		t.Errorf("status %q, spec_dir %q, on disk %q; want sealed in .specs/03_a", r.Status, r.SpecDir, statusOnDisk(t, dir))
	}
	if want := []envelope.Change{{Kind: "status", From: "active", To: "sealed"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
}

// TS-12-29: a package whose intent drifted cannot be sealed, even with --force.
func TestSealingAPackageWhoseIntentDriftedIsRefused_TS12_29(t *testing.T) {
	for name, edit := range map[string]func(t *testing.T, dir string){
		"the intent was edited": func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "prd.md"), "Coding agents drive", "Coding agents might drive")
		},
		"the section was removed": func(t *testing.T, dir string) { editFile(t, filepath.Join(dir, "prd.md"), "## Intent", "## Purpose") },
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
			edit(t, dir)

			e := refusedAt(t, root, "status", envelope.CategoryLifecycle, "03_a", "--status", "sealed", "--force")
			if e == nil {
				return
			}
			saysAll(t, nil, e.Message, "03_a", "restore the intent that was activated", "supersedes this one")
			if e.FixHint != nil {
				t.Errorf("fix_hint %+v; want none", e.FixHint)
			}
		})
	}
}

// planWithOpenTasks is a plan with a pending task and an optional one in progress.
func planWithOpenTasks() []map[string]any {
	optional := planTask(2, "in_progress", []int{})
	optional["optional"] = true
	return []map[string]any{planTask(1, "pending", nil), optional}
}

// TS-12-30: sealing with a task open, an optional one included, is refused,
// naming each with its state.
func TestSealingWithOpenTasksIsRefused_TS12_30(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...)

	e := refusedAt(t, root, "status", envelope.CategoryLifecycle, "03_a", "--status", "sealed")
	if e == nil {
		return
	}
	saysAll(t, nil, e.Message, "task 1 (pending)", "task 2 (in_progress)", "finish or drop")
	if e.FixHint == nil || e.FixHint.Flag != "--force" {
		t.Errorf("fix_hint %+v; want --force", e.FixHint)
	}
}

// TS-12-31: --force seals despite open tasks, with a high tasks_open warning that
// lists them.
func TestForceSealsWithOpenTasks_TS12_31(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...)

	env, r := succeeds(t, root, "03_a", "--status", "sealed", "--force")
	if r.Status != "sealed" || statusOnDisk(t, dir) != "sealed" {
		t.Errorf("status %q, on disk %q; want sealed", r.Status, statusOnDisk(t, dir))
	}
	w := oneWarning(t, env, "tasks_open", "high")
	saysAll(t, nil, w.Message, "task 1", "pending", "task 2", "in_progress")
	if w.Stage != "status" {
		t.Errorf("warning stage %q; want status", w.Stage)
	}
}

// treeBytes records what every file under dir holds and when it was written.
func treeBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = info.ModTime().String() + "\x00" + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func kinds(changes []envelope.Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Kind
	}
	return out
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// TS-12-15: the checks run in the order the spec fixes, and the first to refuse
// ends the call: no result, nothing written, also under --dry-run.
func TestTheChecksRunInOrderAndTheFirstRefusalEndsTheCall_TS12_15(t *testing.T) {
	for _, dry := range [][]string{nil, {"--dry-run"}} {
		root := newRepo(t)
		// A draft with no intent and a key of its own: refused for the intent,
		// which is the target's check, before the keys are looked at.
		noIntent := pkgDir(root, "09_agent_mode")
		editFile(t, filepath.Join(noIntent, "prd.md"), "## Intent", "## Purpose")
		withFrontmatterKey(t, noIntent)
		// An active package with a key of its own and a taken destination: the
		// move comes first, and the lifecycle does not allow it.
		packageWith(t, root, "03_a", "03", "a", "active")
		addUnknownKey(t, pkgDir(root, "03_a"))
		mustMkdir(t, pkgDir(root, "archive/03_a"))
		// A sealed one with the same: the keys come before the destination.
		packageWith(t, root, "04_s", "04", "s", "sealed")
		addUnknownKey(t, pkgDir(root, "04_s"))
		mustMkdir(t, pkgDir(root, "archive/04_s"))

		for name, tc := range map[string]struct {
			argv         []string
			stage, cat   string
			want, notSay []string
		}{
			"the target before the keys": {[]string{"09", "--status", "active"}, "status", "lifecycle", []string{"## Intent"}, []string{"unknown_field", "badkey"}},
			"the move before the keys":   {[]string{"03_a", "--status", "archived"}, "status", "lifecycle", []string{"seal it first"}, []string{"traceability"}},
			"the keys before the destination": {[]string{"04_s", "--status", "archived"}, "status", "lifecycle",
				[]string{"traceability", "--force"}, []string{"already exists"}},
		} {
			argv := append(tc.argv, dry...)
			e := refusedAt(t, root, tc.stage, tc.cat, argv...)
			if e == nil {
				continue
			}
			saysAll(t, argv, e.Message, tc.want...)
			for _, w := range tc.notSay {
				if strings.Contains(e.Message, w) {
					t.Errorf("%s %v: message %q says %q, which a later check finds", name, argv, e.Message, w)
				}
			}
		}
		// With the keys forced, the destination is the next to refuse.
		refusedAt(t, root, "archive", "disk", append([]string{"04_s", "--status", "archived", "--force"}, dry...)...)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TS-12-17: a package that is archived or superseded and not under archive/ is
// moved, not refused as "already there": the changes hold a move and no status.
func TestAPackageAtItsTargetOutsideArchiveIsOnlyMoved_TS12_17(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_a", "03", "a", "archived")
	packageWith(t, root, "04_s", "04", "s", "superseded")

	_, r := succeeds(t, root, "03_a", "--status", "archived")
	if !slices.Equal(kinds(r.Changes), []string{"moved"}) {
		t.Errorf("an archived package: changes %+v; want a move only", r.Changes)
	}
	_, r = succeeds(t, root, "04_s", "--status", "superseded", "--by", "09")
	if !slices.Contains(kinds(r.Changes), "moved") || slices.Contains(kinds(r.Changes), "status") {
		t.Errorf("a superseded package: changes %+v; want a move and no status", r.Changes)
	}
	if exists(pkgDir(root, "03_a")) || exists(pkgDir(root, "04_s")) || !exists(pkgDir(root, "archive/03_a")) || !exists(pkgDir(root, "archive/04_s")) {
		t.Error("the packages are not under archive/")
	}
}

// TS-12-22: a taken destination is refused at stage archive before anything is
// written, not resumable, with --force too, and a warning that was planned is
// not raised.
func TestATakenArchiveDestinationIsRefusedBeforeAnythingIsWritten_TS12_22(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_x", "03", "x", "sealed")
	taken := pkgDir(root, "archive/03_x")
	mustMkdir(t, taken)
	// --force plans a warning for this key; the refusal must not raise it.
	addUnknownKey(t, pkgDir(root, "03_x"))

	for _, argv := range [][]string{{"03_x", "--status", "archived", "--force"}, {"03_x", "--status", "archived", "--force", "--dry-run"}} {
		before := snapshot(t, root)
		env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "archive" || env.Error.Category != "disk" || env.Error.Resumable || env.Result != nil {
			t.Fatalf("%v: code %d, error %+v, result %v; want exit 1 at stage archive, category disk, not resumable, no result", argv, code, env.Error, env.Result != nil)
		}
		saysAll(t, argv, env.Error.Message, "03_x", "already exists")
		if len(env.Warnings) != 0 {
			t.Errorf("%v: warnings %v; a refused call says nothing of what it did not do", argv, warningCodes(env))
		}
		if !mapsEqual(before, snapshot(t, root)) {
			t.Errorf("%v: the repository changed", argv)
		}
	}
	if got := statusOnDisk(t, pkgDir(root, "03_x")); got != "sealed" {
		t.Errorf("status on disk %q; want sealed", got)
	}
}

// TS-12-32: a package outside archive/ is archived by writing the status and
// then moving it, whatever status it leaves: changes status, then moved, in
// display paths.
func TestArchivingWritesTheStatusThenMoves_TS12_32(t *testing.T) {
	root := newRepo(t)
	for _, tc := range []struct{ dir, id, status string }{{"03_x", "03", "sealed"}, {"04_y", "04", "draft"}, {"05_z", "05", "superseded"}} {
		packageWith(t, root, tc.dir, tc.id, tc.dir[3:], tc.status)

		_, r := succeeds(t, root, tc.dir, "--status", "archived")
		want := []envelope.Change{
			{Kind: "status", From: tc.status, To: "archived"},
			{Kind: "moved", From: ".specs/" + tc.dir, To: ".specs/archive/" + tc.dir},
		}
		if !slices.Equal(r.Changes, want) {
			t.Errorf("%s: changes %+v; want %+v", tc.dir, r.Changes, want)
		}
		if exists(pkgDir(root, tc.dir)) || statusOnDisk(t, pkgDir(root, "archive/"+tc.dir)) != "archived" {
			t.Errorf("%s: not archived under archive/", tc.dir)
		}
	}
}

// TS-12-33: a package already under archive/ is archived by writing the status:
// it stays where it is.
func TestArchivingAPackageUnderArchiveWritesOnlyTheStatus_TS12_33(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "archive/03_x", "03", "x", "sealed")

	_, r := succeeds(t, root, ".specs/archive/03_x", "--status", "archived")
	if want := []envelope.Change{{Kind: "status", From: "sealed", To: "archived"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if r.SpecDir != ".specs/archive/03_x" || statusOnDisk(t, pkgDir(root, "archive/03_x")) != "archived" {
		t.Errorf("spec_dir %q, status %q; want it archived where it was", r.SpecDir, statusOnDisk(t, pkgDir(root, "archive/03_x")))
	}
}

// TS-12-34: a move-only call rewrites no file, so the bytes and updated_at are
// as they were, and it needs neither --force nor clean keys: it writes none.
func TestAMoveOnlyArchiveCallRewritesNoFile_TS12_34(t *testing.T) {
	root := newRepo(t)
	dir := packageWith(t, root, "03_x", "03", "x", "archived")
	addUnknownKey(t, dir)
	before := treeBytes(t, dir)

	env, r := succeeds(t, root, "03_x", "--status", "archived")
	if want := []envelope.Change{{Kind: "moved", From: ".specs/03_x", To: ".specs/archive/03_x"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings %v; a call that rewrites nothing drops no key", warningCodes(env))
	}
	if after := treeBytes(t, pkgDir(root, "archive/03_x")); !mapsEqual(before, after) {
		t.Error("a file at the new place differs from the one that was moved (bytes or modification time)")
	}
}

// TS-12-47: the result is the report of the package read back from where it is
// now, and the changes come in the order they were written.
func TestTheResultIsReadBackFromWhereThePackageIsNow_TS12_47(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_x", "03", "x", "sealed")

	_, r := succeeds(t, root, "03_x", "--status", "archived")
	if r.SpecDir != ".specs/archive/03_x" || r.Status != "archived" || r.SpecID != "03" {
		t.Errorf("spec_dir %q, status %q, spec_id %q; want .specs/archive/03_x, archived, 03", r.SpecDir, r.Status, r.SpecID)
	}
	if r.Valid == nil || r.TaskCounts == nil || len(r.Tasks) == 0 || r.AllowedStatus == nil || len(r.AllowedStatus) != 0 {
		t.Errorf("the result lacks the fields of the report: valid %v, task_counts %v, %d tasks, allowed_status %v", r.Valid, r.TaskCounts, len(r.Tasks), r.AllowedStatus)
	}
	if !slices.Equal(kinds(r.Changes), []string{"status", "moved"}) {
		t.Errorf("changes %+v; want status, then moved", r.Changes)
	}
	// What the report form says of the package where it is now is what the call said.
	env, _, _ := run(t, "--dir", root, ".specs/archive/03_x")
	again := result(t, env)
	if again.Status != r.Status || again.SpecDir != r.SpecDir || again.UpdatedAt != r.UpdatedAt {
		t.Errorf("the report of the package now: %q %q %q; the call said %q %q %q",
			again.SpecDir, again.Status, again.UpdatedAt, r.SpecDir, r.Status, r.UpdatedAt)
	}
}

// A status call under --dry-run writes nothing, whatever it goes on to say.
func TestAStatusCallUnderDryRunWritesNothing(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_x", "03", "x", "sealed")
	before := snapshot(t, root)
	run(t, "--dir", root, "03_x", "--status", "archived", "--dry-run")
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("a dry run wrote")
	}
}

// supersedesOf is the supersedes list of the package, read from disk.
func supersedesOf(t *testing.T, dir string) []string {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	return spec.Supersedes
}

// TS-12-35: a package that is superseded or archived cannot replace another,
// and --force does not change that.
func TestAReplacingPackageThatIsSupersededOrArchivedIsRefused_TS12_35(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_old", "03", "old", "sealed")
	packageWith(t, root, "11_a", "11", "a", "superseded")
	packageWith(t, root, "archive/12_b", "12", "b", "archived")

	for _, by := range []string{"11_a", ".specs/archive/12_b"} {
		argv := []string{"03_old", "--status", "superseded", "--by", by, "--force"}
		e := refusedAt(t, root, "status", envelope.CategoryLifecycle, argv...)
		if e == nil {
			continue
		}
		saysAll(t, argv, e.Message, "--by", "cannot replace")
		if e.FixHint == nil || e.FixHint.Flag != "--by" {
			t.Errorf("%v: fix_hint %+v; want --by", argv, e.FixHint)
		}
	}
}

// TS-12-36: superseding writes the banner and the status, records the id in the
// replacing package, and moves the package under archive/.
func TestSupersedingWritesTheBannerRecordsTheIDAndMoves_TS12_36(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_old", "03", "old", "sealed")
	replacing := packageWith(t, root, "12_new", "12", "new", "draft")

	_, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
	want := []envelope.Change{
		{Kind: "status", From: "sealed", To: "superseded"},
		{Kind: "supersedes", Spec: "12", Added: "03"},
		{Kind: "moved", From: ".specs/03_old", To: ".specs/archive/03_old"},
	}
	if !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if r.SpecDir != ".specs/archive/03_old" || r.Status != "superseded" || exists(pkgDir(root, "03_old")) {
		t.Errorf("spec_dir %q, status %q; want it superseded under archive/", r.SpecDir, r.Status)
	}
	old := pkgDir(root, "archive/03_old")
	if b, _ := os.ReadFile(filepath.Join(old, "prd.md")); !strings.Contains(string(b), "DEPRECATED") || !strings.Contains(string(b), "superseded by spec 12") {
		t.Errorf("the banner is missing from the prd.md of the superseded package:\n%s", b)
	}
	if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"03"}) {
		t.Errorf("supersedes of the replacing package = %v; want [03]", got)
	}
}

// TS-12-37: a replacing package that cannot be saved is not a failure: the call
// goes through, with a low warning that names it, the id and why. --force does
// not reach it.
func TestAReplacingPackageThatCannotBeSavedIsAWarning_TS12_37(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(t *testing.T, root string) string
		reason  string
	}{
		"sealed": {func(t *testing.T, root string) string {
			return packageWith(t, root, "12_new", "12", "new", "sealed")
		}, "sealed"},
		"active with drift": {func(t *testing.T, root string) string {
			dir := activePackage(t, root, "12_new", "12", "new")
			editFile(t, filepath.Join(dir, "prd.md"), "Coding agents drive", "Coding agents might drive")
			return dir
		}, "Intent"},
		"unknown keys": {func(t *testing.T, root string) string {
			dir := packageWith(t, root, "12_new", "12", "new", "draft")
			addUnknownKey(t, dir)
			return dir
		}, "traceability"},
		"a save that fails": {func(t *testing.T, root string) string {
			dir := packageWith(t, root, "12_new", "12", "new", "draft")
			readOnly(t, dir)
			return dir
		}, "save"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			replacing := tc.prepare(t, root)
			before := treeBytes(t, replacing)

			env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new", "--force")
			w := oneWarning(t, env, "supersedes_not_recorded", "low")
			saysAll(t, nil, w.Message, ".specs/12_new", "03", tc.reason)
			if w.Stage != "status" {
				t.Errorf("warning stage %q; want status", w.Stage)
			}
			if !slices.Equal(kinds(r.Changes), []string{"status", "moved"}) {
				t.Errorf("changes %+v; want status and moved, no supersedes", r.Changes)
			}
			if r.Status != "superseded" || r.SpecDir != ".specs/archive/03_old" {
				t.Errorf("status %q, spec_dir %q; want superseded under archive/", r.Status, r.SpecDir)
			}
			if !mapsEqual(before, treeBytes(t, replacing)) {
				t.Error("a file of the replacing package changed")
			}
		})
	}
}

// readOnly makes the directory unwritable for the rest of the test, and
// writable again when it ends so that its temporary directory can be removed.
// It skips as root, who is stopped by nothing.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory stops nothing as root")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
}

// TS-12-38: a move-only supersede call skips the banner and the status, adds the
// id the replacing package lacks, and moves the package: nothing of the old one
// is rewritten.
func TestAMoveOnlySupersedeCallRecordsTheMissingIDAndMoves_TS12_38(t *testing.T) {
	root := newRepo(t)
	old := packageWith(t, root, "03_old", "03", "old", "superseded")
	replacing := packageWith(t, root, "12_new", "12", "new", "draft")
	before := treeBytes(t, old)

	_, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
	if !slices.Equal(kinds(r.Changes), []string{"supersedes", "moved"}) {
		t.Errorf("changes %+v; want supersedes, then moved", r.Changes)
	}
	if after := treeBytes(t, pkgDir(root, "archive/03_old")); !mapsEqual(before, after) {
		t.Error("a file of the old package differs after the move")
	}
	if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"03"}) {
		t.Errorf("supersedes of the replacing package = %v; want [03]", got)
	}
}

// 12-REQ-7.4: when the replacing package has the id already, the move-only call
// only moves, and says nothing of a record that was not needed.
func TestAMoveOnlySupersedeCallWithTheIDRecordedOnlyMoves(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_old", "03", "old", "superseded")
	replacing := packageWith(t, root, "12_new", "12", "new", "draft")
	editFile(t, filepath.Join(replacing, "prd.md"), "schema_version: 2", "schema_version: 2\nsupersedes: [\"03\"]")
	before := treeBytes(t, replacing)

	env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
	if !slices.Equal(kinds(r.Changes), []string{"moved"}) || len(env.Warnings) != 0 {
		t.Errorf("changes %+v, warnings %v; want a move and no warning", r.Changes, warningCodes(env))
	}
	if !mapsEqual(before, treeBytes(t, replacing)) {
		t.Error("the replacing package was rewritten")
	}
}

// 12-REQ-7.2: an active replacing package with no drift is saved too.
func TestAnActiveReplacingPackageIsGivenTheID(t *testing.T) {
	root := newRepo(t)
	packageWith(t, root, "03_old", "03", "old", "sealed")
	replacing := activePackage(t, root, "12_new", "12", "new")

	env, r := succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new")
	if len(env.Warnings) != 0 || !slices.Equal(kinds(r.Changes), []string{"status", "supersedes", "moved"}) {
		t.Errorf("changes %+v, warnings %v; want all three and no warning", r.Changes, warningCodes(env))
	}
	if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"03"}) || statusOnDisk(t, replacing) != "active" {
		t.Errorf("supersedes %v, status %q; want [03] and still active", got, statusOnDisk(t, replacing))
	}
}

// TS-12-39: however many times the call is made, and wherever an interrupted move
// left it, the old id is in the replacing package's supersedes once, after the
// ids that were there.
func TestTheOldIDIsInTheReplacingPackageExactlyOnce_TS12_39(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		for calls := 1; calls <= 5; calls++ {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			replacing := packageWith(t, root, "12_new", "12", "new", "draft")
			editFile(t, filepath.Join(replacing, "prd.md"), "schema_version: 2", "schema_version: 2\nsupersedes: [\"01\", \"02\"]")

			if interrupted {
				// The status and the record are written; the move is not.
				readOnly(t, filepath.Join(root, ".specs"))
				run(t, "--dir", root, ".specs/03_old", "--status", "superseded", "--by", "12_new")
				if err := os.Chmod(filepath.Join(root, ".specs"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < calls; i++ {
				path := ".specs/03_old"
				if exists(pkgDir(root, "archive/03_old")) {
					path = ".specs/archive/03_old"
				}
				run(t, "--dir", root, path, "--status", "superseded", "--by", "12_new")
			}
			if got := supersedesOf(t, replacing); !slices.Equal(got, []string{"01", "02", "03"}) {
				t.Errorf("interrupted %v, %d calls: supersedes = %v; want [01 02 03]", interrupted, calls, got)
			}
			if !exists(pkgDir(root, "archive/03_old")) || statusOnDisk(t, pkgDir(root, "archive/03_old")) != "superseded" {
				t.Errorf("interrupted %v, %d calls: the old package is not superseded under archive/", interrupted, calls)
			}
		}
	}
}

// updatedAtIn is the updated_at of the prd.md in dir.
func updatedAtIn(t *testing.T, dir string) time.Time {
	t.Helper()
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.Parse(time.RFC3339, spec.UpdatedAt)
	if err != nil || !strings.HasSuffix(spec.UpdatedAt, "Z") {
		t.Fatalf("updated_at %q is not a UTC time in RFC 3339 (%v)", spec.UpdatedAt, err)
	}
	return ts
}

// TS-12-40: every package the call writes, the replacing one too, gets the time
// of the call as its updated_at.
func TestEveryWrittenPackageGetsTheTimeOfTheCall_TS12_40(t *testing.T) {
	root := newRepo(t)
	start := time.Now().UTC().Truncate(time.Second)
	activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	packageWith(t, root, "04_old", "04", "old", "sealed")
	replacing := packageWith(t, root, "12_new", "12", "new", "draft")
	editFile(t, filepath.Join(replacing, "prd.md"), `updated_at: "2026-09-08T09:00:00Z"`, `updated_at: "2000-01-01T00:00:00Z"`)

	succeeds(t, root, "03_a", "--status", "sealed")
	succeeds(t, root, "04_old", "--status", "superseded", "--by", "12_new")
	for name, dir := range map[string]string{"the sealed package": pkgDir(root, "03_a"), "the superseded package": pkgDir(root, "archive/04_old"), "the replacing package": replacing} {
		if got := updatedAtIn(t, dir); got.Before(start) {
			t.Errorf("%s: updated_at %s is before the call began (%s)", name, got.Format(time.RFC3339), start.Format(time.RFC3339))
		}
	}
}

// TS-12-41: a write the disk refuses is exit 1 at stage write, category disk,
// with the library's message and no result; the warnings of a call that wrote
// nothing are not raised.
func TestAWriteTheDiskRefusesIsAWriteFailure_TS12_41(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...)
	readOnly(t, dir)

	argv := []string{"03_a", "--status", "sealed", "--force"} // --force plans a tasks_open warning
	before := snapshot(t, root)
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "write" || env.Error.Category != "disk" || env.Result != nil {
		t.Fatalf("code %d, error %+v, result %v; want exit 1 at stage write, category disk, no result", code, env.Error, env.Result != nil)
	}
	if !strings.HasPrefix(env.Error.Message, "save error: cannot write temp file") {
		t.Errorf("message %q is not the library's", env.Error.Message)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings %v; the call wrote nothing", warningCodes(env))
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the repository changed")
	}
}

// failWith replaces a library call of the status form for the rest of the test.
func failWith[T any](t *testing.T, call *T, replacement T) {
	t.Helper()
	was := *call
	t.Cleanup(func() { *call = was })
	*call = replacement
}

// TS-12-42: a lifecycle or intent error that reaches the write despite the
// checks is a refusal at stage status, for the status and for the banner.
func TestALifecycleOrIntentErrorAtTheWriteIsARefusal_TS12_42(t *testing.T) {
	for name, writeErr := range map[string]error{
		"lifecycle": &afspec.LifecycleError{Msg: "cannot do it"},
		"intent":    &afspec.IntentError{Msg: "the intent drifted"},
	} {
		t.Run(name+" at the status", func(t *testing.T) {
			root := newRepo(t)
			activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
			failWith(t, &transitionSpec, func(*afspec.Spec, string, string) (*afspec.Spec, error) { return nil, writeErr })

			e := refusedAt(t, root, "status", envelope.CategoryLifecycle, "03_a", "--status", "sealed")
			if e != nil {
				saysAll(t, nil, e.Message, "03_a", writeErr.Error())
			}
		})
		t.Run(name+" at the banner", func(t *testing.T) {
			root := newRepo(t)
			packageWith(t, root, "03_old", "03", "old", "sealed")
			packageWith(t, root, "12_new", "12", "new", "draft")
			failWith(t, &supersedeSpec, func(*afspec.Spec, string, string) (*afspec.Spec, error) { return nil, writeErr })

			refusedAt(t, root, "status", envelope.CategoryLifecycle, "03_old", "--status", "superseded", "--by", "12_new")
		})
	}
}

// interrupted runs the call with .specs made read-only, so that the status is
// written and the move under archive/ is not, and returns the envelope.
func interrupted(t *testing.T, root string, argv ...string) envelope.AdminEnvelope {
	t.Helper()
	readOnly(t, filepath.Join(root, ".specs"))
	env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
	if code != cli.ExitFailed {
		t.Fatalf("%v: code %d, error %+v; want exit 1", argv, code, env.Error)
	}
	return env
}

// TS-12-43: a move that fails after the status was written is exit 1 at stage
// archive, resumable, with the package as it is now, what was written, and the
// call that finishes it.
func TestAMoveThatFailsAfterTheStatusIsResumable_TS12_43(t *testing.T) {
	for name, tc := range map[string]struct {
		argv    []string
		status  string
		changes []string
		flags   []string
	}{
		"archiving": {[]string{"03_x", "--status", "archived"}, "archived", []string{"status"}, []string{"--status", "archived"}},
		"superseding": {[]string{"03_x", "--status", "superseded", "--by", "12_new"}, "superseded", []string{"status", "supersedes"},
			[]string{"--status", "superseded", "--by", ".specs/12_new"}},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			packageWith(t, root, "03_x", "03", "x", "sealed")
			packageWith(t, root, "12_new", "12", "new", "draft")

			env := interrupted(t, root, tc.argv...)
			e := env.Error
			if e == nil || e.Stage != "archive" || e.Category != "disk" || !e.Resumable || e.Retryable {
				t.Fatalf("error %+v; want stage archive, category disk, resumable and not retryable", e)
			}
			saysAll(t, tc.argv, e.Message, ".specs/03_x", tc.status, "again")
			if !strings.Contains(env.Summary, "status was written") || !strings.Contains(env.Summary, "move was not") {
				t.Errorf("summary %q does not say that the status was written and the move was not", env.Summary)
			}
			r := result(t, env)
			if r.SpecDir != ".specs/03_x" || r.Status != tc.status || !slices.Equal(kinds(r.Changes), tc.changes) {
				t.Errorf("spec_dir %q, status %q, changes %+v; want .specs/03_x, %s, %v", r.SpecDir, r.Status, r.Changes, tc.status, tc.changes)
			}
			if len(env.Next) != 1 || env.Next[0].Tool != "speca" || env.Next[0].Input != ".specs/03_x" || !slices.Equal(env.Next[0].Flags, tc.flags) {
				t.Errorf("next %+v; want the same call against .specs/03_x with %v", env.Next, tc.flags)
			}
		})
	}
}

// TS-12-44: the same call, run again once the cause is gone, moves the package
// and changes nothing else.
func TestRunningTheCallAgainOnlyMovesThePackage_TS12_44(t *testing.T) {
	for name, argv := range map[string][]string{
		"archiving":   {"03_x", "--status", "archived"},
		"superseding": {"03_x", "--status", "superseded", "--by", "12_new"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := packageWith(t, root, "03_x", "03", "x", "sealed")
			replacing := packageWith(t, root, "12_new", "12", "new", "draft")

			env := interrupted(t, root, argv...)
			if err := os.Chmod(filepath.Join(root, ".specs"), 0o755); err != nil {
				t.Fatal(err)
			}
			left, replacingLeft := treeBytes(t, dir), treeBytes(t, replacing)
			next := env.Next[0]

			_, r := succeeds(t, root, append([]string{next.Input}, next.Flags...)...)
			if !slices.Equal(kinds(r.Changes), []string{"moved"}) {
				t.Errorf("changes %+v; want only the move", r.Changes)
			}
			if after := treeBytes(t, pkgDir(root, "archive/03_x")); !mapsEqual(left, after) {
				t.Error("a file of the package differs from what the failed call left")
			}
			if !mapsEqual(replacingLeft, treeBytes(t, replacing)) {
				t.Error("the replacing package was written again")
			}
		})
	}
}

// TS-12-45: a package that was written but cannot be read back fails the call at
// stage load, and the result still says what was written.
func TestAReadBackFailureKeepsTheChanges_TS12_45(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	failWith(t, &loadWritten, func(string) (*afspec.Spec, error) { return nil, errors.New("the disk went away") })

	env, code, _ := run(t, "--dir", root, "03_a", "--status", "sealed")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Error.Resumable {
		t.Fatalf("code %d, error %+v; want exit 1 at stage load, category input", code, env.Error)
	}
	if want := []envelope.Change{{Kind: "status", From: "active", To: "sealed"}}; env.Result == nil || !slices.Equal(env.Result.Changes, want) {
		t.Errorf("result %+v; want it to carry %+v", env.Result, want)
	}
	if env.Result != nil && (env.Result.Status != "sealed" || env.Result.SpecDir != ".specs/03_a") {
		t.Errorf("result status %q, spec_dir %q; want sealed in .specs/03_a", env.Result.Status, env.Result.SpecDir)
	}
	if statusOnDisk(t, dir) != "sealed" {
		t.Error("the status was not written")
	}
}

// TS-12-46: Resumable is true for the failure to move after the status was
// written, and for nothing else, in the result and in the envelope.
func TestResumableIsTrueOnlyForTheArchiveStageFailure_TS12_46(t *testing.T) {
	if (&Result{}).Resumable() || !(&Result{resumable: true}).Resumable() {
		t.Error("Result.Resumable does not follow the field that says the failure can be resumed")
	}

	resumable := func(env envelope.AdminEnvelope) bool { return env.Error != nil && env.Error.Resumable }
	root := newRepo(t)
	activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	packageWith(t, root, "04_s", "04", "s", "sealed")
	packageWith(t, root, "05_s", "05", "s", "sealed")

	success, _, _ := run(t, "--dir", root, "03_a", "--status", "sealed")
	refusal, _, _ := run(t, "--dir", root, "04_s", "--status", "active")
	if resumable(success) || resumable(refusal) {
		t.Error("a success or a refusal is resumable")
	}
	func() {
		failWith(t, &loadWritten, func(string) (*afspec.Spec, error) { return nil, errors.New("gone") })
		if env, _, _ := run(t, "--dir", root, "04_s", "--status", "sealed"); resumable(env) {
			t.Error("a failure to read back is resumable")
		}
	}()
	if got := interrupted(t, root, "05_s", "--status", "archived"); !resumable(got) {
		t.Error("the failure to move after the status was written is not resumable")
	}
}

// A move-only call is the resume of an interrupted one, and its move can fail
// again: it is reported the same way, with nothing written to say.
func TestAMoveOnlyCallWhoseMoveFailsIsResumableToo(t *testing.T) {
	root := newRepo(t)
	dir := packageWith(t, root, "03_x", "03", "x", "archived")
	before := treeBytes(t, dir)

	env := interrupted(t, root, "03_x", "--status", "archived")
	e := env.Error
	if e == nil || e.Stage != "archive" || e.Category != "disk" || !e.Resumable || e.Retryable {
		t.Fatalf("error %+v; want stage archive, category disk, resumable and not retryable", e)
	}
	r := result(t, env)
	if r.SpecDir != ".specs/03_x" || r.Status != "archived" || len(r.Changes) != 0 {
		t.Errorf("spec_dir %q, status %q, changes %+v; want the package where it is, with no change", r.SpecDir, r.Status, r.Changes)
	}
	if !strings.Contains(env.Summary, "not moved") || len(env.Next) != 1 || !slices.Equal(env.Next[0].Flags, []string{"--status", "archived"}) {
		t.Errorf("summary %q, next %+v; want a package that was not moved, and the same call", env.Summary, env.Next)
	}
	if !mapsEqual(before, treeBytes(t, dir)) {
		t.Error("the package changed")
	}
}

// TS-12-9: a status call reaches the status form, whose result has changes and
// not only a report, honours --dry-run, and accepts --force.
func TestStatusCallsReachTheStatusFormAndHonourDryRunAndForce_TS12_9(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "04_a", "04", "a", planTask(1, "done", nil))
	packageWith(t, root, "03_old", "03", "old", "sealed")
	packageWith(t, root, "12_new", "12", "new", "draft")

	before := snapshot(t, root)
	_, r := succeeds(t, root, "09", "--status", "active", "--dry-run")
	if !r.DryRun || len(r.Changes) != 1 {
		t.Errorf("dry_run %v, changes %+v; want a dry run with one change", r.DryRun, r.Changes)
	}
	if !mapsEqual(before, snapshot(t, root)) {
		t.Error("the dry run wrote")
	}
	if _, r = succeeds(t, root, "04_a", "--status", "sealed", "--force"); len(r.Changes) != 1 || r.DryRun {
		t.Errorf("sealing with --force: changes %+v, dry_run %v", r.Changes, r.DryRun)
	}
	if _, r = succeeds(t, root, "03_old", "--status", "superseded", "--by", "12_new"); len(r.Changes) == 0 {
		t.Error("superseding has no changes")
	}
}

// TS-12-27: a dry-run activation reports the hash the library would compute and
// leaves prd.md as it was.
func TestADryRunActivationReportsTheHashAndWritesNothing_TS12_27(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := afspec.ComputeIntentHash(spec.PRDBody)
	if err != nil {
		t.Fatal(err)
	}
	before := treeBytes(t, dir)

	_, r := succeeds(t, root, "09", "--status", "active", "--dry-run")
	if r.IntentHash != want || r.Status != "active" || !r.DryRun {
		t.Errorf("intent_hash %q, status %q, dry_run %v; want %q, active, true", r.IntentHash, r.Status, r.DryRun, want)
	}
	if !mapsEqual(before, treeBytes(t, dir)) {
		t.Error("prd.md, or another file, changed (bytes or modification time)")
	}
}

// sameOutcome fails the test unless the dry run and the real call agree on
// everything the spec says they agree on.
func sameOutcome(t *testing.T, name string, dry envelope.AdminEnvelope, dryCode int, real envelope.AdminEnvelope, realCode int) {
	t.Helper()
	if dryCode != realCode {
		t.Errorf("%s: exit %d under --dry-run, %d without", name, dryCode, realCode)
	}
	if (dry.Error == nil) != (real.Error == nil) || dry.Error != nil && (dry.Error.Stage != real.Error.Stage ||
		dry.Error.Category != real.Error.Category || dry.Error.Message != real.Error.Message) {
		t.Errorf("%s: error %+v under --dry-run, %+v without", name, dry.Error, real.Error)
	}
	if !slices.Equal(dry.Warnings, real.Warnings) {
		t.Errorf("%s: warnings %+v under --dry-run, %+v without", name, dry.Warnings, real.Warnings)
	}
	if (dry.Result == nil) != (real.Result == nil) {
		t.Errorf("%s: a result under --dry-run: %v, without: %v", name, dry.Result != nil, real.Result != nil)
		return
	}
	if dry.Result == nil {
		return
	}
	d, r := dry.Result, real.Result
	if !slices.Equal(d.Changes, r.Changes) || d.SpecDir != r.SpecDir || d.Status != r.Status || d.IntentHash != r.IntentHash ||
		d.IntentDrift != r.IntentDrift || !slices.Equal(d.AllowedStatus, r.AllowedStatus) || !slices.Equal(d.FailingRules, r.FailingRules) ||
		(d.Valid == nil) != (r.Valid == nil) || d.Valid != nil && *d.Valid != *r.Valid {
		t.Errorf("%s: the result under --dry-run is %+v; without it %+v", name, d, r)
	}
	if !d.DryRun || r.DryRun {
		t.Errorf("%s: dry_run is %v under --dry-run and %v without", name, d.DryRun, r.DryRun)
	}
}

// TS-12-48: a dry run gives the exit code, the refusal, the warnings, the
// changes and the result of the real call, and writes nothing.
func TestADryRunIsTheRealCallWithoutTheWrites_TS12_48(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, root string)
		argv  []string
	}{
		"activation": {func(*testing.T, string) {}, []string{"09", "--status", "active"}},
		"sealing with open tasks, forced": {func(t *testing.T, root string) {
			activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...)
		}, []string{"03_a", "--status", "sealed", "--force"}},
		"archiving": {func(t *testing.T, root string) { packageWith(t, root, "03_x", "03", "x", "sealed") },
			[]string{"03_x", "--status", "archived"}},
		"archiving a draft with a key, forced": {func(t *testing.T, root string) {
			addUnknownKey(t, packageWith(t, root, "03_x", "03", "x", "draft"))
		}, []string{"03_x", "--status", "archived", "--force"}},
		"superseding": {func(t *testing.T, root string) {
			packageWith(t, root, "03_old", "03", "old", "sealed")
			packageWith(t, root, "12_new", "12", "new", "draft")
		}, []string{"03_old", "--status", "superseded", "--by", "12_new"}},
		"superseding with a replacement that is sealed": {func(t *testing.T, root string) {
			packageWith(t, root, "03_old", "03", "old", "sealed")
			packageWith(t, root, "12_new", "12", "new", "sealed")
		}, []string{"03_old", "--status", "superseded", "--by", "12_new"}},
		"a move only": {func(t *testing.T, root string) { packageWith(t, root, "03_x", "03", "x", "archived") },
			[]string{"03_x", "--status", "archived"}},
		"a refusal": {func(t *testing.T, root string) { packageWith(t, root, "03_x", "03", "x", "sealed") },
			[]string{"03_x", "--status", "sealed"}},
		"a taken destination": {func(t *testing.T, root string) {
			packageWith(t, root, "03_x", "03", "x", "sealed")
			mustMkdir(t, pkgDir(root, "archive/03_x"))
		}, []string{"03_x", "--status", "archived"}},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, root)
			before := snapshot(t, root)
			argv := append([]string{"--dir", root}, tc.argv...)

			dry, dryCode, _ := run(t, append(argv, "--dry-run")...)
			if !mapsEqual(before, snapshot(t, root)) {
				t.Fatal("the dry run changed the repository (a file, a directory or a modification time)")
			}
			real, realCode, _ := run(t, argv...)
			sameOutcome(t, name, dry, dryCode, real, realCode)
		})
	}
}

// TS-12-49: the summary is one sentence of the form the spec gives for each move,
// and under --dry-run ends by saying nothing was written.
func TestTheSummaryHasTheFormOfTheMoveMade_TS12_49(t *testing.T) {
	const dryRun = " (dry run; nothing written)"
	for name, tc := range map[string]struct {
		setup func(t *testing.T, root string)
		argv  []string
		want  string
	}{
		"activating": {func(*testing.T, string) {}, []string{"09", "--status", "active"}, "speca: activated 09_agent_mode"},
		"sealing": {func(t *testing.T, root string) { activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil)) },
			[]string{"03_a", "--status", "sealed"}, "speca: sealed 03_a"},
		"archiving": {func(t *testing.T, root string) { packageWith(t, root, "04_s", "04", "s", "sealed") },
			[]string{"04_s", "--status", "archived"}, "speca: archived 04_s, moved to .specs/archive/04_s"},
		"archiving under archive/": {func(t *testing.T, root string) { packageWith(t, root, "archive/04_s", "04", "s", "sealed") },
			[]string{".specs/archive/04_s", "--status", "archived"}, "speca: archived 04_s"},
		"superseding": {func(t *testing.T, root string) {
			packageWith(t, root, "05_old", "05", "old", "sealed")
			packageWith(t, root, "12_new", "12", "new", "draft")
		}, []string{"05_old", "--status", "superseded", "--by", "12_new"}, "speca: superseded 05_old by 12, moved to .specs/archive/05_old"},
		"a move only": {func(t *testing.T, root string) { packageWith(t, root, "06_m", "06", "m", "archived") },
			[]string{"06_m", "--status", "archived"}, "speca: moved 06_m to .specs/archive/06_m"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			tc.setup(t, root)
			argv := append([]string{"--dir", root}, tc.argv...)
			dry, _, _ := run(t, append(argv, "--dry-run")...)
			if dry.Summary != tc.want+dryRun {
				t.Errorf("under --dry-run: summary %q; want %q", dry.Summary, tc.want+dryRun)
			}
			real, _, _ := run(t, argv...)
			if real.Summary != tc.want {
				t.Errorf("summary %q; want %q", real.Summary, tc.want)
			}
		})
	}
}

// TS-12-50: after activation next is the claim of the next task when it is
// ready; after any other call, and after a refusal, there is nothing to suggest.
func TestAfterACallNextIsTheClaimAfterActivationAndNothingElse_TS12_50(t *testing.T) {
	root := newRepo(t)
	_, _, _ = run(t, "--dir", root, "09", "--status", "active")
	env, _, _ := run(t, "--dir", root, "09", "--status", "active") // active now: a refusal
	if len(env.Next) != 0 {
		t.Errorf("a refusal suggests %+v", env.Next)
	}

	root = newRepo(t)
	env, _ = succeeds(t, root, "09", "--status", "active")
	want := []envelope.Next{{Tool: "speca", Input: ".specs/09_agent_mode", Flags: []string{"--task", "1", "--state", "in_progress"}}}
	if len(env.Next) != 1 || env.Next[0].Tool != want[0].Tool || env.Next[0].Input != want[0].Input || !slices.Equal(env.Next[0].Flags, want[0].Flags) {
		t.Errorf("after activation next is %+v; want the claim of task 1: %+v", env.Next, want)
	}

	// Not ready, or none: there is nothing to claim.
	for name, tasks := range map[string][]map[string]any{
		"the next task is already in progress": {planTask(1, "in_progress", nil)},
		"there is no task":                     {},
	} {
		root := newRepo(t)
		setTasks(t, pkgDir(root, "09_agent_mode"), tasks...)
		// The plan does not validate, which is not what is looked at here.
		if env, _ = succeeds(t, root, "09", "--status", "active", "--force"); len(env.Next) != 0 {
			t.Errorf("%s: next is %+v; want none", name, env.Next)
		}
	}

	for name, tc := range map[string]struct {
		setup func(t *testing.T, root string)
		argv  []string
	}{
		"sealing":   {func(t *testing.T, root string) { activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil)) }, []string{"03_a", "--status", "sealed"}},
		"archiving": {func(t *testing.T, root string) { packageWith(t, root, "04_s", "04", "s", "sealed") }, []string{"04_s", "--status", "archived"}},
		"superseding": {func(t *testing.T, root string) {
			packageWith(t, root, "05_old", "05", "old", "sealed")
			packageWith(t, root, "12_new", "12", "new", "draft")
		}, []string{"05_old", "--status", "superseded", "--by", "12_new"}},
		"a move only": {func(t *testing.T, root string) { packageWith(t, root, "06_m", "06", "m", "archived") }, []string{"06_m", "--status", "archived"}},
	} {
		root := newRepo(t)
		tc.setup(t, root)
		if env, _ = succeeds(t, root, tc.argv...); len(env.Next) != 0 {
			t.Errorf("%s: next is %+v; want none", name, env.Next)
		}
	}
}

// TS-12-51: a dry run that passes its checks suggests the same call without
// --dry-run, keeping --force and --by.
func TestADryRunSuggestsTheSameCallWithoutDryRun_TS12_51(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	packageWith(t, root, "05_old", "05", "old", "sealed")
	packageWith(t, root, "12_new", "12", "new", "draft")

	for _, tc := range []struct {
		argv  []string
		input string
		flags []string
	}{
		{[]string{"03_a", "--status", "sealed"}, ".specs/03_a", []string{"--status", "sealed"}},
		{[]string{"03_a", "--status", "sealed", "--force"}, ".specs/03_a", []string{"--status", "sealed", "--force"}},
		{[]string{"05_old", "--status", "superseded", "--by", "12"}, ".specs/05_old", []string{"--status", "superseded", "--by", ".specs/12_new"}},
	} {
		env, _ := succeeds(t, root, append(tc.argv, "--dry-run")...)
		if len(env.Next) != 1 || env.Next[0].Tool != "speca" || env.Next[0].Input != tc.input || !slices.Equal(env.Next[0].Flags, tc.flags) {
			t.Errorf("%v: next %+v; want speca %s %v", tc.argv, env.Next, tc.input, tc.flags)
		}
	}
}
