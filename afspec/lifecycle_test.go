package afspec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidTransition(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"draft", "active", true},
		{"draft", "archived", true},
		{"active", "sealed", true},
		{"sealed", "superseded", true},
		{"sealed", "archived", true},
		{"superseded", "archived", true},
		{"draft", "sealed", false},
		{"active", "draft", false},
		{"archived", "active", false},
		{"draft", "nonsense", false},
		{"nonsense", "active", false},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			if got := ValidTransition(tc.from, tc.to); got != tc.want {
				t.Errorf("ValidTransition(%q, %q) = %v; want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestTransition_ToActiveHashesTheIntentAndPersists(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	spec := loadFixture(t, dir)

	activated, err := spec.Transition("active", dir)
	if err != nil {
		t.Fatalf("Transition = %v", err)
	}
	if activated.Status != "active" {
		t.Errorf("status = %q; want active", activated.Status)
	}
	if spec.Status != "draft" || spec.IntentHash != nil {
		t.Error("Transition mutated the receiver")
	}
	want, err := ComputeIntentHash(spec.PRDBody)
	if err != nil {
		t.Fatal(err)
	}
	if activated.IntentHash == nil || *activated.IntentHash != want {
		t.Errorf("hash = %v; want %q", activated.IntentHash, want)
	}
	if loadFixture(t, dir).Status != "active" {
		t.Error("the transition was not persisted")
	}
}

func TestTransition_RefusesAndWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  string
		prdBody string
		want    any
	}{
		{"an illegal target", "sealed", "", new(*LifecycleError)},
		{"activation without an intent", "active", "# Test Feature\n\nNo intent.\n", new(*IntentError)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, fixtureValidSpec)
			spec := loadFixture(t, dir)
			if tc.prdBody != "" {
				spec.PRDBody = tc.prdBody
			}
			tree := treeOf(t, dir)

			got, err := spec.Transition(tc.target, dir)
			if got != nil || !errors.As(err, tc.want) {
				t.Fatalf("Transition(%q) = %v, %v; want nil and a %T", tc.target, got, err, tc.want)
			}
			if !sameTree(tree, treeOf(t, dir)) {
				t.Error("a refused transition still wrote to disk")
			}
		})
	}
}

func TestSupersede_AddsADeprecationBannerToASealedSpec(t *testing.T) {
	dir := copyFixture(t, fixtureValidSpec)
	active, err := loadFixture(t, dir).Transition("active", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := active.Supersede("02", dir); !errors.As(err, new(*LifecycleError)) {
		t.Errorf("Supersede of an active spec = %v; want a *LifecycleError", err)
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
	if !strings.Contains(superseded.PRDBody, "superseded by spec 02") {
		t.Error("the deprecation banner does not name the superseding spec")
	}
}

func TestArchiveDestination_ReturnsTheFreePathAndWritesNothing(t *testing.T) {
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

// A taken destination, and one that cannot be checked, are a *SaveError, and
// nothing is written.
func TestArchiveDestination_RefusesATakenOrUnreadableDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		// occupy puts something at or around root/archive/03_x.
		occupy func(t *testing.T, root string)
		// conflict says whether the message reports the path as taken.
		conflict bool
	}{
		{"taken", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "archive", "03_x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, true},
		// A rename would replace the link, so it is taken.
		{"a dangling link", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "archive", "03_x")); err != nil {
				t.Fatal(err)
			}
		}, true},
		// archive is a file, so archive/03_x cannot be looked at (ENOTDIR).
		{"unreadable", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "archive"), []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", "sealed")
			tc.occupy(t, root)
			tree := treeOf(t, root)

			got, err := ArchiveDestination(dir, root)
			var se *SaveError
			if got != "" || !errors.As(err, &se) {
				t.Fatalf("ArchiveDestination = %q, %v; want \"\" and a *SaveError", got, err)
			}
			want := "archive conflict: " + filepath.Join(root, "archive", "03_x") + " already exists"
			if tc.conflict && !strings.Contains(se.Msg, want) {
				t.Errorf("message %q does not say %q", se.Msg, want)
			}
			if !tc.conflict && strings.Contains(se.Msg, "already exists") {
				t.Errorf("message %q says the destination exists, which is not what was found", se.Msg)
			}
			if !sameTree(tree, treeOf(t, root)) {
				t.Error("the tree of root changed")
			}
		})
	}
}

// MoveUnderArchive moves an archived or a superseded package and rewrites no
// file.
func TestMoveUnderArchive_MovesWithoutRewritingAnyFile(t *testing.T) {
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

// MoveUnderArchive refuses a package that does not load, one that belongs
// outside archive/, and one it cannot move, and leaves it as it was.
func TestMoveUnderArchive_RefusesAndLeavesThePackage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		// prepare breaks the package at dir or its root.
		prepare func(t *testing.T, root, dir string)
		want    any
		// conflict says whether the error reports the destination as taken.
		conflict bool
	}{
		{"a package that does not load", "archived", func(t *testing.T, root, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "requirements.json"), []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, new(*LoadError), false},
		{"draft", "draft", nil, new(*LifecycleError), false},
		{"active", "active", nil, new(*LifecycleError), false},
		{"sealed", "sealed", nil, new(*LifecycleError), false},
		{"a taken destination", "archived", func(t *testing.T, root, dir string) {
			if err := os.MkdirAll(filepath.Join(root, "archive", "03_x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, new(*SaveError), true},
		{"a read-only root", "archived", func(t *testing.T, root, dir string) {
			readOnly(t, root)
		}, new(*SaveError), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", tc.status)
			archiveExisted := false
			if tc.prepare != nil {
				tc.prepare(t, root, dir)
				archiveExisted = exists(filepath.Join(root, "archive"))
			}
			tree := treeOf(t, dir)

			got, err := MoveUnderArchive(dir, root)
			if got != "" || !errors.As(err, tc.want) {
				t.Fatalf("MoveUnderArchive = %q, %v; want \"\" and a %T", got, err, tc.want)
			}
			want := "archive conflict: " + filepath.Join(root, "archive", "03_x") + " already exists"
			if tc.conflict && !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not say %q", err, want)
			}
			if !sameTree(tree, treeOf(t, dir)) {
				t.Error("the package changed")
			}
			if !archiveExisted && exists(filepath.Join(root, "archive")) {
				t.Error("archive/ was created")
			}
		})
	}
}

// MoveToArchive archives any package that is not archived yet, a superseded
// one included, and moves it.
func TestMoveToArchive_ArchivesAndMovesAnyPackage(t *testing.T) {
	for _, status := range []string{"sealed", "superseded", "archived"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			dir := packageIn(t, root, "03_x", status)

			if err := MoveToArchive(dir, root); err != nil {
				t.Fatalf("MoveToArchive = %v", err)
			}
			if exists(dir) {
				t.Error("the package is still in place")
			}
			if got := loadFixture(t, filepath.Join(root, "archive", "03_x")).Status; got != "archived" {
				t.Errorf("status under archive/ = %q; want archived", got)
			}
		})
	}
}

// A taken destination is found before the transition writes anything.
func TestMoveToArchive_WithATakenDestinationWritesNothing(t *testing.T) {
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
