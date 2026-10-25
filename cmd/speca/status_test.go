package main

import (
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// A package that does not load, one in another format version, and a --by that
// does either, are refused at stage load, also under --force, naming --by only
// when the replacing package is at fault.
func TestStatus_FailsAtLoad(t *testing.T) {
	root := newRepo(t)
	bad := pkgDir(root, "05_bad")
	copyDir(t, fixture("valid_spec"), bad)
	writeFile(t, filepath.Join(bad, "requirements.json"), "{not json")
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
			if e == nil {
				return
			}
			saysAll(t, tc.argv, e.Message, tc.want...)
			if strings.HasPrefix(name, "the input") && strings.Contains(e.Message, "--by") {
				t.Errorf("message %q names --by, and the input is at fault", e.Message)
			}
		})
	}
}

// A move the lifecycle does not allow is refused at stage status, even under
// --force, with the package, its status, the target, the way to get there if
// there is one, and the moves that are allowed.
func TestStatus_RefusesAMoveTheLifecycleDoesNotAllow(t *testing.T) {
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
				t.Errorf("fix_hint %+v; want none, since no move leaves the status", e.FixHint)
			case tc.valid != nil && (e.FixHint == nil || e.FixHint.Flag != "--status" || !slices.Equal(e.FixHint.Valid, tc.valid)):
				t.Errorf("fix_hint %+v; want --status with %v", e.FixHint, tc.valid)
			}
		})
	}
}

var chainInMessage = regexp.MustCompile(`\b(?:draft|active|sealed|superseded|archived)(?: → (?:draft|active|sealed|superseded|archived))+`)

// For any two distinct statuses the lifecycle does not link directly, the
// refusal names a shortest chain of allowed moves, and none when there is no
// chain. The length of a shortest chain is found here by a breadth-first search
// of its own over the library's table.
func TestRefusalMessage_NamesAShortestChainOrNone(t *testing.T) {
	statuses := []string{"draft", "active", "sealed", "superseded", "archived"}
	shortest := func(from, to string) int {
		dist := map[string]int{from: 0}
		for queue := []string{from}; len(queue) > 0; queue = queue[1:] {
			for _, next := range statuses {
				if _, seen := dist[next]; !seen && afspec.ValidTransition(queue[0], next) {
					dist[next] = dist[queue[0]] + 1
					queue = append(queue, next)
				}
			}
		}
		return dist[to]
	}

	pairs := 0
	for _, from := range statuses {
		for _, to := range statuses {
			if from == to || afspec.ValidTransition(from, to) {
				continue
			}
			pairs++
			msg := refusalMessage("pkg", from, to)
			chain := chainInMessage.FindString(msg)
			want := shortest(from, to)
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

// A package that is already at the status asked for, which is archived or
// superseded, and is not under archive/, is not refused as "already there": the
// call only moves it, which resumes a call interrupted after it wrote the
// status. Everywhere else the lifecycle decides.
func TestCheckMove_AMoveOnlyCallIsAPackageAtItsTargetOutsideArchive(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		status, target, dir      string
		under, moveOnly, refused bool
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
			if sc.under != tc.under || sc.moveOnly != tc.moveOnly || (refusal != nil) != tc.refused {
				t.Errorf("under %v, moveOnly %v, refused %v; want %v, %v, %v", sc.under, sc.moveOnly, refusal != nil, tc.under, tc.moveOnly, tc.refused)
			}
		})
	}
}

// The checks run in a fixed order, and the first to refuse ends the call: no
// result, nothing written, also under --dry-run.
func TestStatus_ChecksRunInOrderAndTheFirstRefusalEndsTheCall(t *testing.T) {
	for _, dry := range [][]string{nil, {"--dry-run"}} {
		root := newRepo(t)
		// A draft with no intent and a key of its own: refused for the intent,
		// which is the target's check, before the keys are looked at.
		noIntent := pkgDir(root, "09_agent_mode")
		removeIntent(t, noIntent)
		withFrontmatterKey(t, noIntent)
		// An active package with a key of its own and a taken destination: the
		// move comes first, and the lifecycle does not allow it.
		addUnknownKey(t, packageWith(t, root, "03_a", "03", "a", "active"))
		mustMkdir(t, pkgDir(root, "archive/03_a"))
		// A sealed one with the same: the keys come before the destination.
		addUnknownKey(t, packageWith(t, root, "04_s", "04", "s", "sealed"))
		mustMkdir(t, pkgDir(root, "archive/04_s"))
		// A draft with a task open: sealing it is not a move the lifecycle allows,
		// and the sealing check would refuse it for its open tasks.
		packageWith(t, root, "05_d", "05", "d", "draft")
		// An active package with a task open and a key of its own: the target's
		// check comes before the keys.
		addUnknownKey(t, activePackage(t, root, "06_o", "06", "o", planWithOpenTasks()...))

		for name, tc := range map[string]struct {
			argv         []string
			want, notSay []string
		}{
			"the move before the target": {[]string{"05_d", "--status", "sealed"},
				[]string{"activate it first"}, []string{"open tasks"}},
			"the sealing check before the keys": {[]string{"06_o", "--status", "sealed"},
				[]string{"open tasks", "task 1 (pending)"}, []string{"traceability", "holds keys"}},
			"the target before the keys": {[]string{"09", "--status", "active"}, []string{"## Intent"}, []string{"unknown_field", "badkey"}},
			"the move before the keys":   {[]string{"03_a", "--status", "archived"}, []string{"seal it first"}, []string{"traceability"}},
			"the keys before the destination": {[]string{"04_s", "--status", "archived"},
				[]string{"traceability", "--force"}, []string{"already exists"}},
		} {
			argv := append(tc.argv, dry...)
			e := refusedAt(t, root, "status", "lifecycle", argv...)
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

// A taken destination is refused at stage archive before anything is written,
// not resumable, with --force too, and a warning that was planned is not
// raised: a refused call says nothing of what it did not do.
func TestStatus_RefusesATakenArchiveDestination(t *testing.T) {
	root := newRepo(t)
	addUnknownKey(t, packageWith(t, root, "03_x", "03", "x", "sealed")) // --force plans a warning for this key
	mustMkdir(t, pkgDir(root, "archive/03_x"))

	for _, argv := range [][]string{{"03_x", "--status", "archived", "--force"}, {"03_x", "--status", "archived", "--force", "--dry-run"}} {
		before := snapshot(t, root)
		env, code, _ := run(t, append([]string{"--dir", root}, argv...)...)
		if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "archive" || env.Error.Category != "disk" || env.Error.Resumable || env.Result != nil {
			t.Fatalf("%v: code %d, error %+v, result %v; want exit 1 at stage archive, category disk, not resumable, no result", argv, code, env.Error, env.Result != nil)
		}
		saysAll(t, argv, env.Error.Message, "03_x", "already exists")
		if len(env.Warnings) != 0 {
			t.Errorf("%v: warnings %v", argv, warningCodes(env))
		}
		if !maps.Equal(before, snapshot(t, root)) {
			t.Errorf("%v: the repository changed", argv)
		}
	}
	if got := statusOnDisk(t, pkgDir(root, "03_x")); got != "sealed" {
		t.Errorf("status on disk %q; want sealed", got)
	}
}

// A draft that validates and has an intent is activated: the status and the
// hash of the intent are written, and the package stays where it is.
func TestStatus_ActivatesADraft(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")

	_, r := succeeds(t, root, "09", "--status", "active")
	if r.Status != "active" || r.IntentHash == "" || r.SpecDir != ".specs/09_agent_mode" {
		t.Errorf("status %q, intent_hash %q, spec_dir %q; want active, a hash, .specs/09_agent_mode", r.Status, r.IntentHash, r.SpecDir)
	}
	if want := []envelope.Change{{Kind: "status", From: "draft", To: "active"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if spec := loadSpec(t, dir); spec.Status != "active" || spec.IntentHash == nil || *spec.IntentHash != r.IntentHash {
		t.Errorf("on disk: status %q, intent_hash %v", spec.Status, spec.IntentHash)
	}
}

// An active package with no drift and no open task is sealed where it is, and
// its tasks stay as they were.
func TestStatus_SealsAnActivePackage(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil), planTask(2, "dropped", []int{}))

	_, r := succeeds(t, root, "03_a", "--status", "sealed")
	if r.Status != "sealed" || r.SpecDir != ".specs/03_a" || statusOnDisk(t, dir) != "sealed" {
		t.Errorf("status %q, spec_dir %q, on disk %q; want sealed in .specs/03_a", r.Status, r.SpecDir, statusOnDisk(t, dir))
	}
	if want := []envelope.Change{{Kind: "status", From: "active", To: "sealed"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if got := onDisk(t, dir); !maps.Equal(got, map[int]string{1: "done", 2: "dropped"}) {
		t.Errorf("the tasks on disk are %v; want them as they were", got)
	}
}

// A package is archived by writing the status and then, unless it is under
// archive/ already, moving it there, whatever status it leaves; the changes say
// so in display paths.
func TestStatus_Archives(t *testing.T) {
	for _, tc := range []struct {
		dir, status string
		moved       bool
	}{
		{"03_x", "sealed", true},
		{"04_y", "draft", true},
		{"05_z", "superseded", true},
		{"archive/06_w", "sealed", false},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			root := newRepo(t)
			name := filepath.Base(tc.dir)
			packageWith(t, root, tc.dir, name[:2], name[3:], tc.status)

			_, r := succeeds(t, root, ".specs/"+tc.dir, "--status", "archived")
			want := []envelope.Change{{Kind: "status", From: tc.status, To: "archived"}}
			if tc.moved {
				want = append(want, envelope.Change{Kind: "moved", From: ".specs/" + tc.dir, To: ".specs/archive/" + name})
				if exists(pkgDir(root, tc.dir)) {
					t.Errorf("%s is still where it was", tc.dir)
				}
			}
			if !slices.Equal(r.Changes, want) {
				t.Errorf("changes %+v; want %+v", r.Changes, want)
			}
			if r.SpecDir != ".specs/archive/"+name || statusOnDisk(t, pkgDir(root, "archive/"+name)) != "archived" {
				t.Errorf("spec_dir %q; want it archived under archive/", r.SpecDir)
			}
		})
	}
}

// The package goes under archive/ in the directory that holds it, not under
// --specs-dir, so a package reached by a path outside the spec root is archived
// in its own tree.
func TestStatus_ArchivesAPackageOutsideTheSpecRootInItsOwnTree(t *testing.T) {
	root := newRepo(t)
	external := filepath.Join(root, "elsewhere", "03_x")
	copyDir(t, fixture("v2_example"), external)
	setStatus(t, external, "sealed")

	_, r := succeeds(t, root, "elsewhere/03_x", "--status", "archived")
	if r.SpecDir != "elsewhere/archive/03_x" || !exists(filepath.Join(root, "elsewhere", "archive", "03_x")) {
		t.Errorf("spec_dir %q; want it under archive/ next to where it was", r.SpecDir)
	}
	if exists(pkgDir(root, "archive")) || exists(external) {
		t.Error("the spec root has an archive/, or the package is still where it was")
	}
}

// A move-only call rewrites no file, so the bytes and updated_at are as they
// were, and it needs neither --force nor clean keys: it drops none.
func TestStatus_AMoveOnlyCallRewritesNoFile(t *testing.T) {
	root := newRepo(t)
	dir := packageWith(t, root, "03_x", "03", "x", "archived")
	addUnknownKey(t, dir)
	before := treeBytes(t, dir)

	env, r := succeeds(t, root, "03_x", "--status", "archived")
	if want := []envelope.Change{{Kind: "moved", From: ".specs/03_x", To: ".specs/archive/03_x"}}; !slices.Equal(r.Changes, want) {
		t.Errorf("changes %+v; want %+v", r.Changes, want)
	}
	if len(env.Warnings) != 0 {
		t.Errorf("warnings %v", warningCodes(env))
	}
	if after := treeBytes(t, pkgDir(root, "archive/03_x")); !maps.Equal(before, after) {
		t.Error("a file at the new place differs from the one that was moved (bytes or modification time)")
	}
}

// The result is the report of the package read back from where it is now, and
// the changes come in the order they were written.
func TestStatus_TheResultIsReadBackFromWhereThePackageIsNow(t *testing.T) {
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

// Every package the call writes, the replacing one too, gets the time of the
// call as its updated_at.
func TestStatus_SetsUpdatedAtOnEveryPackageItWrites(t *testing.T) {
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

// A write the disk refuses is exit 1 at stage write, category disk, with the
// library's message, no result and nothing to resume; the warnings of a call
// that wrote nothing are not raised.
func TestStatus_AWriteTheDiskRefusesIsAWriteFailure(t *testing.T) {
	root := newRepo(t)
	readOnly(t, activePackage(t, root, "03_a", "03", "a", planWithOpenTasks()...))

	e := refusedAt(t, root, "write", "disk", "03_a", "--status", "sealed", "--force") // --force plans a tasks_open warning
	if e == nil {
		return
	}
	if !strings.HasPrefix(e.Message, "save error: cannot write temp file") || e.Resumable {
		t.Errorf("message %q is not the library's, or resumable %v", e.Message, e.Resumable)
	}
	if env, _, _ := run(t, "--dir", root, "03_a", "--status", "sealed", "--force"); len(env.Warnings) != 0 {
		t.Errorf("warnings %v; the call wrote nothing", warningCodes(env))
	}
}

// A lifecycle or intent error that reaches the write despite the checks is a
// refusal at stage status, for the status and for the banner.
func TestStatus_ALifecycleOrIntentErrorAtTheWriteIsARefusal(t *testing.T) {
	for name, writeErr := range map[string]error{
		"lifecycle": &afspec.LifecycleError{Msg: "cannot do it"},
		"intent":    &afspec.IntentError{Msg: "the intent drifted"},
	} {
		t.Run(name+" at the status", func(t *testing.T) {
			root := newRepo(t)
			activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
			failWith(t, &transitionSpec, func(*afspec.Spec, string, string) (*afspec.Spec, error) { return nil, writeErr })

			if e := refusedAt(t, root, "status", envelope.CategoryLifecycle, "03_a", "--status", "sealed"); e != nil {
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

// A move that fails after the status was written is exit 1 at stage archive,
// resumable and not retryable (a rename that was refused is not made by
// repeating it, but the work goes on from where it stopped), with the package as
// it is now, what was written, and the call that finishes it.
func TestStatus_AMoveThatFailsAfterTheStatusIsResumable(t *testing.T) {
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
			if want := (envelope.Next{Tool: "speca", Input: ".specs/03_x", Flags: tc.flags}); len(env.Next) != 1 || !sameCall(env.Next[0], want) {
				t.Errorf("next %+v; want %+v", env.Next, want)
			}
		})
	}
}

// The same call, run again once the cause is gone, moves the package and
// changes nothing else.
func TestStatus_RunningTheCallAgainOnlyMovesThePackage(t *testing.T) {
	for name, argv := range map[string][]string{
		"archiving":   {"03_x", "--status", "archived"},
		"superseding": {"03_x", "--status", "superseded", "--by", "12_new"},
	} {
		t.Run(name, func(t *testing.T) {
			root := newRepo(t)
			dir := packageWith(t, root, "03_x", "03", "x", "sealed")
			replacing := packageWith(t, root, "12_new", "12", "new", "draft")

			env := interrupted(t, root, argv...)
			writable(t, filepath.Join(root, ".specs"))
			left, replacingLeft := treeBytes(t, dir), treeBytes(t, replacing)

			again, code := followed(t, root, env.Next[0])
			if code != cli.ExitOK || !slices.Equal(kinds(result(t, again).Changes), []string{"moved"}) {
				t.Errorf("code %d, changes %+v; want only the move", code, result(t, again).Changes)
			}
			if after := treeBytes(t, pkgDir(root, "archive/03_x")); !maps.Equal(left, after) {
				t.Error("a file of the package differs from what the failed call left")
			}
			if !maps.Equal(replacingLeft, treeBytes(t, replacing)) {
				t.Error("the replacing package was written again")
			}
		})
	}
}

// A move-only call is the resume of an interrupted one, and its move can fail
// again: it is reported the same way, with nothing written to say.
func TestStatus_AMoveOnlyCallWhoseMoveFailsIsResumableToo(t *testing.T) {
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
	if !maps.Equal(before, treeBytes(t, dir)) {
		t.Error("the package changed")
	}
}

// A package that was written but cannot be read back fails the call at stage
// load, not resumable, and the result still says what was written. The same
// holds when the read-back after a failed move fails too.
func TestStatus_AReadBackFailureKeepsTheChanges(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "done", nil))
	failWith(t, &loadWritten, func(string) (*afspec.Spec, error) { return nil, errors.New("the disk went away") })

	env, code, _ := run(t, "--dir", root, "03_a", "--status", "sealed")
	if code != cli.ExitFailed || env.Error == nil || env.Error.Stage != "load" || env.Error.Category != "input" || env.Error.Resumable {
		t.Fatalf("code %d, error %+v; want exit 1 at stage load, category input, not resumable", code, env.Error)
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

	packageWith(t, root, "04_x", "04", "x", "sealed")
	env = interrupted(t, root, "04_x", "--status", "archived")
	if r := result(t, env); !env.Error.Resumable || r.Status != "archived" || !slices.Equal(kinds(r.Changes), []string{"status"}) {
		t.Errorf("error %+v, result %+v; want the archived status written, resumable", env.Error, r)
	}
}

// A dry run gives the exit code, the refusal, the warnings, the changes and the
// result of the real call, and writes nothing.
func TestStatus_ADryRunIsTheRealCallWithoutTheWrites(t *testing.T) {
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
		// Nothing is rewritten, so the package keeps its key and does not validate,
		// in the dry run as in the call.
		"a move only, on a package with a key": {func(t *testing.T, root string) {
			addUnknownKey(t, packageWith(t, root, "03_x", "03", "x", "archived"))
		}, []string{"03_x", "--status", "archived"}},
		"a move only of a superseded package, with a key": {func(t *testing.T, root string) {
			addUnknownKey(t, packageWith(t, root, "03_old", "03", "old", "superseded"))
			packageWith(t, root, "12_new", "12", "new", "draft")
		}, []string{"03_old", "--status", "superseded", "--by", "12_new"}},
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
			if !maps.Equal(before, snapshot(t, root)) {
				t.Fatal("the dry run changed the repository (a file, a directory or a modification time)")
			}
			real, realCode, _ := run(t, argv...)
			if dryCode != realCode || !reflect.DeepEqual(dry.Error, real.Error) || !slices.Equal(dry.Warnings, real.Warnings) {
				t.Errorf("exit %d, error %+v, warnings %+v under --dry-run; %d, %+v, %+v without", dryCode, dry.Error, dry.Warnings, realCode, real.Error, real.Warnings)
			}
			if (dry.Result == nil) != (real.Result == nil) {
				t.Fatalf("a result under --dry-run: %v, without: %v", dry.Result != nil, real.Result != nil)
			}
			if dry.Result == nil {
				return
			}
			d, r := *dry.Result, *real.Result
			if !d.DryRun || r.DryRun {
				t.Errorf("dry_run is %v under --dry-run and %v without", d.DryRun, r.DryRun)
			}
			// All but when it was written, and whether it was.
			d.UpdatedAt, r.UpdatedAt, d.DryRun, r.DryRun = "", "", false, false
			if !reflect.DeepEqual(d, r) {
				t.Errorf("the result under --dry-run is %+v; without it %+v", d, r)
			}
		})
	}
}

// A dry-run activation reports the hash the library would compute and leaves
// prd.md as it was.
func TestStatus_ADryRunActivationReportsTheHash(t *testing.T) {
	root := newRepo(t)
	dir := pkgDir(root, "09_agent_mode")
	want, err := afspec.ComputeIntentHash(loadSpec(t, dir).PRDBody)
	if err != nil {
		t.Fatal(err)
	}
	before := treeBytes(t, dir)

	_, r := succeeds(t, root, "09", "--status", "active", "--dry-run")
	if r.IntentHash != want || r.Status != "active" || !r.DryRun {
		t.Errorf("intent_hash %q, status %q, dry_run %v; want %q, active, true", r.IntentHash, r.Status, r.DryRun, want)
	}
	if !maps.Equal(before, treeBytes(t, dir)) {
		t.Error("prd.md, or another file, changed (bytes or modification time)")
	}
}

// The summary is one sentence of the form of the move made, and under --dry-run
// ends by saying nothing was written.
func TestStatus_Summary(t *testing.T) {
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
			if dry, _, _ := run(t, append(argv, "--dry-run")...); dry.Summary != tc.want+dryRun {
				t.Errorf("under --dry-run: summary %q; want %q", dry.Summary, tc.want+dryRun)
			}
			if real, _, _ := run(t, argv...); real.Summary != tc.want {
				t.Errorf("summary %q; want %q", real.Summary, tc.want)
			}
		})
	}
}

// After an activation next is the claim of the next task when it is ready;
// after any other call, and after a refusal, there is nothing to suggest.
func TestStatus_Next(t *testing.T) {
	root := newRepo(t)
	env, _ := succeeds(t, root, "09", "--status", "active")
	if len(env.Next) != 1 || !sameCall(env.Next[0], claim(".specs/09_agent_mode", 1)) {
		t.Errorf("after activation next is %+v; want the claim of task 1", env.Next)
	}
	if env, _, _ = run(t, "--dir", root, "09", "--status", "active"); env.Error == nil || len(env.Next) != 0 { // active now: a refusal
		t.Errorf("a refusal: error %+v, next %+v", env.Error, env.Next)
	}

	for name, tc := range map[string]struct {
		setup func(t *testing.T, root string)
		argv  []string
	}{
		// The plans do not validate, which is not what is looked at here.
		"activating, the next task in progress": {func(t *testing.T, root string) {
			setTasks(t, pkgDir(root, "09_agent_mode"), planTask(1, "in_progress", nil))
		}, []string{"09", "--status", "active", "--force"}},
		"activating, no task": {func(t *testing.T, root string) { setTasks(t, pkgDir(root, "09_agent_mode")) },
			[]string{"09", "--status", "active", "--force"}},
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

// A dry run that passes its checks suggests the same call without --dry-run,
// keeping --force and --by.
func TestStatus_ADryRunSuggestsTheSameCallWithoutDryRun(t *testing.T) {
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
		if want := (envelope.Next{Tool: "speca", Input: tc.input, Flags: tc.flags}); len(env.Next) != 1 || !sameCall(env.Next[0], want) {
			t.Errorf("%v: next %+v; want %+v", tc.argv, env.Next, want)
		}
	}
}
