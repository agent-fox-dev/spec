package afspec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidTransition(t *testing.T) {
	allowed := []struct{ from, to string }{
		{"draft", "active"},
		{"active", "sealed"},
		{"sealed", "superseded"},
		{"sealed", "archived"},
	}
	for _, tc := range allowed {
		if !ValidTransition(tc.from, tc.to) {
			t.Errorf("%s → %s should be allowed", tc.from, tc.to)
		}
	}
	rejected := []struct{ from, to string }{
		{"draft", "sealed"},
		{"active", "draft"},
		{"archived", "active"},
		{"draft", "nonsense"},
	}
	for _, tc := range rejected {
		if ValidTransition(tc.from, tc.to) {
			t.Errorf("%s → %s should be rejected", tc.from, tc.to)
		}
	}
}

func TestTransitionToActiveHashesTheIntent(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	spec := loadFixture(t, dir)

	activated, err := spec.Transition("active", dir)
	if err != nil {
		t.Fatalf("Transition = %v", err)
	}
	if activated.Status != "active" {
		t.Errorf("status = %q; want active", activated.Status)
	}
	if activated.IntentHash == nil || *activated.IntentHash == "" {
		t.Fatal("the intent hash was not computed at draft → active")
	}
	if spec.Status != "draft" || spec.IntentHash != nil {
		t.Error("Transition mutated the receiver")
	}

	want, err := ComputeIntentHash(spec.PRDBody)
	if err != nil {
		t.Fatal(err)
	}
	if *activated.IntentHash != want {
		t.Errorf("hash = %q; want %q", *activated.IntentHash, want)
	}

	reloaded := loadFixture(t, dir)
	if reloaded.Status != "active" {
		t.Error("the transition was not persisted")
	}
}

func TestTransitionRejectsIllegalTargets(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	spec := loadFixture(t, dir)
	if _, err := spec.Transition("sealed", dir); err == nil {
		t.Fatal("draft → sealed was accepted")
	}
	if loadFixture(t, dir).Status != "draft" {
		t.Error("a rejected transition still wrote to disk")
	}
}

func TestSupersedeAddsADeprecationBanner(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	spec := loadFixture(t, dir)

	active, err := spec.Transition("active", dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := active.Transition("sealed", dir)
	if err != nil {
		t.Fatal(err)
	}

	superseded, err := sealed.Supersede("02", dir)
	if err != nil {
		t.Fatalf("Supersede = %v", err)
	}
	if superseded.Status != "superseded" {
		t.Errorf("status = %q; want superseded", superseded.Status)
	}
	if !strings.Contains(superseded.PRDBody, "02") {
		t.Error("the deprecation banner does not name the superseding spec")
	}
}

func TestMoveToArchive(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "01_test_feature")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := copyFixture(t, fixtureValidSpec)
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(src, e.Name()))
		if err := os.WriteFile(filepath.Join(specDir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	spec := loadFixture(t, specDir)
	active, err := spec.Transition("active", specDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := active.Transition("sealed", specDir); err != nil {
		t.Fatal(err)
	}

	if err := MoveToArchive(specDir, root); err != nil {
		t.Fatalf("MoveToArchive = %v", err)
	}
	if _, err := os.Stat(specDir); !os.IsNotExist(err) {
		t.Error("the spec directory is still in place after archiving")
	}
	archived := filepath.Join(root, "archive", "01_test_feature")
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("the spec is not under archive/: %v", err)
	}
	if loadFixture(t, archived).Status != "archived" {
		t.Error("the archived spec is not in the archived state")
	}
}

func TestComputeIntentHash(t *testing.T) {
	body := "# Title\n\n## Intent\n\nThe intent text.\n\n## Goals\n\n- something\n"
	first, err := ComputeIntentHash(body)
	if err != nil {
		t.Fatalf("ComputeIntentHash = %v", err)
	}
	if len(first) != 64 {
		t.Errorf("hash length = %d; want a 64-character SHA-256 hex digest", len(first))
	}

	// Editing a section other than Intent must not change the hash.
	edited := strings.Replace(body, "- something", "- something else", 1)
	second, err := ComputeIntentHash(edited)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Error("editing a non-Intent section changed the intent hash")
	}

	// Editing Intent must change it.
	drifted, err := ComputeIntentHash(strings.Replace(body, "The intent text.", "Something else.", 1))
	if err != nil {
		t.Fatal(err)
	}
	if drifted == first {
		t.Error("editing the Intent section did not change the hash")
	}
}

func TestComputeIntentHashRequiresTheSection(t *testing.T) {
	_, err := ComputeIntentHash("# Title\n\nNo intent section here.\n")
	if err == nil {
		t.Fatal("a body with no ## Intent section produced a hash")
	}
}

// packageIn builds the package root/name in the given status: the valid
// fixture, brought there by the library's own moves.
func packageIn(t *testing.T, root, name, status string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := copyFixture(t, fixtureValidSpec)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	step := func(s *Spec, target string) *Spec {
		t.Helper()
		next, err := s.Transition(target, dir)
		if err != nil {
			t.Fatalf("Transition(%q) = %v", target, err)
		}
		return next
	}
	spec := loadFixture(t, dir)
	switch status {
	case "draft":
	case "active":
		step(spec, "active")
	case "sealed":
		step(step(spec, "active"), "sealed")
	case "superseded":
		if _, err := step(step(spec, "active"), "sealed").Supersede("02", dir); err != nil {
			t.Fatalf("Supersede = %v", err)
		}
	case "archived":
		step(spec, "archived")
	default:
		t.Fatalf("no such status %q", status)
	}
	return dir
}

// fileState is what a file holds and when it was last written.
type fileState struct {
	data    string
	modTime time.Time
}

// treeOf records every file under dir, by its path relative to dir.
func treeOf(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = fileState{string(data), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(a, b map[string]fileState) bool {
	if len(a) != len(b) {
		return false
	}
	for name, x := range a {
		y, ok := b[name]
		if !ok || x.data != y.data || !x.modTime.Equal(y.modTime) {
			return false
		}
	}
	return true
}

// entriesOf lists the names directly under dir.
func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// readOnly makes dir unwritable for the rest of the test, and writable again
// when it ends so that its temporary directory can be removed. The tests that
// use it cannot fail a write as root.
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

// TS-12-1: ArchiveDestination names the free path and writes nothing.
func TestArchiveDestinationReturnsTheFreePathAndWritesNothing_TS12_1(t *testing.T) {
	root := t.TempDir()
	dir := packageIn(t, root, "03_x", "sealed")
	before := entriesOf(t, root)
	tree := treeOf(t, root)

	got, err := ArchiveDestination(dir, root)
	if err != nil {
		t.Fatalf("ArchiveDestination = %v", err)
	}
	if want := filepath.Join(root, "archive", "03_x"); got != want {
		t.Errorf("destination = %q; want %q", got, want)
	}
	if exists(filepath.Join(root, "archive")) {
		t.Error("ArchiveDestination created archive/")
	}
	if after := entriesOf(t, root); strings.Join(after, ",") != strings.Join(before, ",") || !sameTree(tree, treeOf(t, root)) {
		t.Errorf("the tree of root changed: %v then %v", before, after)
	}
}

// TS-12-2: a taken destination, and one that cannot be checked, are a
// *SaveError, and nothing is written.
func TestArchiveDestinationRefusesATakenOrUnreadableDestination_TS12_2(t *testing.T) {
	t.Run("taken", func(t *testing.T) {
		root := t.TempDir()
		dir := packageIn(t, root, "03_x", "sealed")
		taken := filepath.Join(root, "archive", "03_x")
		if err := os.MkdirAll(taken, 0o755); err != nil {
			t.Fatal(err)
		}
		tree := treeOf(t, root)

		got, err := ArchiveDestination(dir, root)
		var se *SaveError
		if got != "" || !errors.As(err, &se) {
			t.Fatalf("ArchiveDestination = %q, %v; want \"\" and a *SaveError", got, err)
		}
		if want := "archive conflict: " + taken + " already exists"; !strings.Contains(se.Msg, want) {
			t.Errorf("message %q does not say %q", se.Msg, want)
		}
		if !sameTree(tree, treeOf(t, root)) {
			t.Error("the tree of root changed")
		}
	})
	t.Run("a dangling link", func(t *testing.T) {
		// A rename would replace the link, so it is taken.
		root := t.TempDir()
		dir := packageIn(t, root, "03_x", "sealed")
		if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "archive", "03_x")); err != nil {
			t.Fatal(err)
		}

		var se *SaveError
		if got, err := ArchiveDestination(dir, root); got != "" || !errors.As(err, &se) || !strings.Contains(se.Msg, "already exists") {
			t.Fatalf("ArchiveDestination = %q, %v; want the conflict *SaveError", got, err)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		root := t.TempDir()
		dir := packageIn(t, root, "03_x", "sealed")
		// archive is a file, so archive/03_x cannot be looked at (ENOTDIR).
		if err := os.WriteFile(filepath.Join(root, "archive"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		tree := treeOf(t, root)

		got, err := ArchiveDestination(dir, root)
		var se *SaveError
		if got != "" || !errors.As(err, &se) {
			t.Fatalf("ArchiveDestination = %q, %v; want \"\" and a *SaveError", got, err)
		}
		if strings.Contains(se.Msg, "already exists") {
			t.Errorf("message %q says the destination exists, which is not what was found", se.Msg)
		}
		if !sameTree(tree, treeOf(t, root)) {
			t.Error("the tree of root changed")
		}
	})
}

// TS-12-3: MoveUnderArchive moves an archived or a superseded package and
// rewrites no file.
func TestMoveUnderArchiveMovesWithoutRewritingAnyFile_TS12_3(t *testing.T) {
	for _, status := range []string{"archived", "superseded"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", status)
			tree := treeOf(t, dir)

			got, err := MoveUnderArchive(dir, root)
			want := filepath.Join(root, "archive", "03_x")
			if err != nil || got != want {
				t.Fatalf("MoveUnderArchive = %q, %v; want %q", got, err, want)
			}
			// The mode is 0755 before the umask, which a directory made the same way
			// is subject to as well.
			info, err := os.Stat(filepath.Join(root, "archive"))
			sample := filepath.Join(t.TempDir(), "sample")
			if err := os.Mkdir(sample, 0o755); err != nil {
				t.Fatal(err)
			}
			sampleInfo, serr := os.Stat(sample)
			if err != nil || serr != nil || info.Mode().Perm() != sampleInfo.Mode().Perm() || info.Mode().Perm()&0o700 != 0o700 {
				t.Errorf("archive/ = %v, %v; want the mode of a directory made with 0755, %v", info, err, sampleInfo)
			}
			if exists(dir) {
				t.Error("the source directory is still there")
			}
			if !sameTree(tree, treeOf(t, got)) {
				t.Error("a file at the destination differs from the one that was moved (bytes or modification time)")
			}
			if loadFixture(t, got).Status != status {
				t.Errorf("the status is no longer %s", status)
			}
		})
	}
}

// TS-12-4: a package that does not load is the *LoadError, and nothing moves.
func TestMoveUnderArchiveReturnsTheLoadErrorAndMovesNothing_TS12_4(t *testing.T) {
	root := t.TempDir()
	dir := packageIn(t, root, "03_x", "archived")
	if err := os.WriteFile(filepath.Join(dir, "requirements.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := treeOf(t, dir)

	got, err := MoveUnderArchive(dir, root)
	var le *LoadError
	if got != "" || !errors.As(err, &le) {
		t.Fatalf("MoveUnderArchive = %q, %v; want \"\" and a *LoadError", got, err)
	}
	if !sameTree(tree, treeOf(t, dir)) || exists(filepath.Join(root, "archive")) {
		t.Error("something moved, or archive/ was created")
	}
}

// TS-12-5: only an archived or a superseded package lives under archive/.
func TestMoveUnderArchiveRefusesAPackageThatBelongsOutside_TS12_5(t *testing.T) {
	for _, status := range []string{"draft", "active", "sealed"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", status)
			tree := treeOf(t, dir)

			got, err := MoveUnderArchive(dir, root)
			var lce *LifecycleError
			if got != "" || !errors.As(err, &lce) {
				t.Fatalf("MoveUnderArchive = %q, %v; want \"\" and a *LifecycleError", got, err)
			}
			if exists(filepath.Join(root, "archive")) || !sameTree(tree, treeOf(t, dir)) {
				t.Error("archive/ was created, or the package changed")
			}
		})
	}
}

// TS-12-6: a taken destination, or a filesystem that refuses, leaves the package
// where it was, with its files as they were.
func TestMoveUnderArchiveLeavesThePackageWhenItCannotMoveIt_TS12_6(t *testing.T) {
	t.Run("taken", func(t *testing.T) {
		root := t.TempDir()
		dir := packageIn(t, root, "03_x", "archived")
		taken := filepath.Join(root, "archive", "03_x")
		if err := os.MkdirAll(taken, 0o755); err != nil {
			t.Fatal(err)
		}
		tree := treeOf(t, dir)

		got, err := MoveUnderArchive(dir, root)
		var se *SaveError
		if got != "" || !errors.As(err, &se) || !strings.Contains(se.Msg, "archive conflict: "+taken+" already exists") {
			t.Fatalf("MoveUnderArchive = %q, %v; want \"\" and the conflict *SaveError", got, err)
		}
		if !sameTree(tree, treeOf(t, dir)) {
			t.Error("the package changed")
		}
	})
	t.Run("read-only root", func(t *testing.T) {
		root := t.TempDir()
		dir := packageIn(t, root, "03_x", "archived")
		tree := treeOf(t, dir)
		readOnly(t, root)

		got, err := MoveUnderArchive(dir, root)
		var se *SaveError
		if got != "" || !errors.As(err, &se) {
			t.Fatalf("MoveUnderArchive = %q, %v; want \"\" and a *SaveError", got, err)
		}
		if !sameTree(tree, treeOf(t, dir)) {
			t.Error("the package changed")
		}
	})
}

// TS-12-7: MoveToArchive archives any package that is not archived yet, a
// superseded one included, and moves it.
func TestMoveToArchiveArchivesAndMovesAnyPackage_TS12_7(t *testing.T) {
	for _, status := range []string{"sealed", "superseded", "archived"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", status)

			if err := MoveToArchive(dir, root); err != nil {
				t.Fatalf("MoveToArchive = %v", err)
			}
			moved := filepath.Join(root, "archive", "03_x")
			if exists(dir) {
				t.Error("the package is still in place")
			}
			if got := loadFixture(t, moved).Status; got != "archived" {
				t.Errorf("status under archive/ = %q; want archived", got)
			}
		})
	}
}

// TS-12-8: a taken destination is found before the transition writes anything.
func TestMoveToArchiveWithATakenDestinationWritesNothing_TS12_8(t *testing.T) {
	root := t.TempDir()
	dir := packageIn(t, root, "03_x", "sealed")
	if err := os.MkdirAll(filepath.Join(root, "archive", "03_x"), 0o755); err != nil {
		t.Fatal(err)
	}
	tree := treeOf(t, dir)

	err := MoveToArchive(dir, root)
	var se *SaveError
	if !errors.As(err, &se) {
		t.Fatalf("MoveToArchive = %v; want a *SaveError", err)
	}
	if got := loadFixture(t, dir).Status; got != "sealed" {
		t.Errorf("status = %q; want sealed", got)
	}
	if after := treeOf(t, dir); !sameTree(tree, after) {
		t.Errorf("these files of the package changed: %v", changedFiles(tree, after))
	}
}

// changedFiles names the files that differ between two trees, or exist in only
// one.
func changedFiles(a, b map[string]fileState) []string {
	var out []string
	for name, x := range a {
		if y, ok := b[name]; !ok || x.data != y.data || !x.modTime.Equal(y.modTime) {
			out = append(out, name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}
