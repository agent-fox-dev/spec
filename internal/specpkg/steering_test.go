package specpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// steeringRoot is a spec root holding only a steering.md with this content.
func steeringRoot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "steering.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// record is a steering at the first of the amendments, newest first, each
// given as version, date, change.
func record(amendments ...[3]string) string {
	s := "---\nversion: " + amendments[0][0] + "\namendments:\n"
	for _, a := range amendments {
		s += "  - version: " + a[0] + "\n    date: " + a[1] + "\n    change: " + a[2] + "\n"
	}
	return s + "---\n# Steering\n\nUse make check.\n"
}

// steeringFindings are the errors and the warnings of the rule in a verdict.
func steeringFindings(v afspec.ValidationResult) (errs, warns []afspec.ValidationEntry) {
	for _, e := range v.Errors {
		if e.Check == "steering_version" {
			errs = append(errs, e)
		}
	}
	for _, w := range v.Warnings {
		if w.Check == "steering_version" {
			warns = append(warns, w)
		}
	}
	return errs, warns
}

// packageAt is the valid fixture in a status, written under a steering version.
func packageAt(t *testing.T, status, steeringVersion string) *afspec.Spec {
	t.Helper()
	s := loadedSpec(t, "01", "test_feature", "user")
	s.Tasks.Dependencies = []afspec.Dependency{}
	s.Status, s.SteeringVersion = status, steeringVersion
	return s
}

var fourAmendments = record(
	[3]string{"2.1.0", "2026-11-06", "CHANGE-210 a directive was added."},
	[3]string{"2.0.0", "2026-10-25", "CHANGE-200 tests are organised by behaviour."},
	[3]string{"1.1.0", "2026-10-15", "CHANGE-110 generated docs."},
	[3]string{"1.0.0", "2026-10-15", "CHANGE-100 the baseline."},
)

// An equal version says nothing; an older one is one warning naming both
// versions and every amendment since, newest first, with the sentence about a
// changed directive when one of them raised the MAJOR number.
func TestVerdict_NamesTheAmendmentsAPackageMissed(t *testing.T) {
	root := steeringRoot(t, fourAmendments)
	for _, status := range []string{"draft", "active"} {
		for _, tc := range []struct {
			version string
			says    []string // in this order
			notSays []string
			major   bool
		}{
			{version: "2.1.0"},
			{version: "2.0.0", says: []string{"2.0.0", "2.1.0", "2026-11-06", "CHANGE-210"}, notSays: []string{"CHANGE-200", "CHANGE-110"}},
			{version: "1.1.0", says: []string{"1.1.0", "2.1.0", "CHANGE-210", "2026-10-25", "CHANGE-200"}, notSays: []string{"CHANGE-110", "CHANGE-100"}, major: true},
		} {
			t.Run(status+"/"+tc.version, func(t *testing.T) {
				v, err := Verdict(root, packageAt(t, status, tc.version))
				if err != nil {
					t.Fatal(err)
				}
				errs, warns := steeringFindings(v)
				if len(errs) != 0 {
					t.Fatalf("errors %+v; the rule never makes an error", errs)
				}
				if tc.says == nil {
					if len(warns) != 0 {
						t.Errorf("warnings %+v; want none for equal versions", warns)
					}
					return
				}
				if len(warns) != 1 || warns[0].Artifact != "prd.md" {
					t.Fatalf("warnings %+v; want one on prd.md", warns)
				}
				m, at := warns[0].Message, 0
				for _, s := range tc.says {
					i := strings.Index(m[at:], s)
					if i < 0 {
						t.Errorf("message %q lacks %q in its order", m, s)
						break
					}
					at += i
				}
				for _, s := range tc.notSays {
					if strings.Contains(m, s) {
						t.Errorf("message %q names %q, an amendment the package was written under", m, s)
					}
				}
				if got := strings.Contains(m, "removed or changed"); got != tc.major {
					t.Errorf("message %q: the changed-directive sentence is there %v; want %v", m, got, tc.major)
				}
			})
		}
	}
}

// A version newer than the steering's, or one the record never had, is one
// warning that says which.
func TestVerdict_SaysWhenAPackageVersionIsNotInTheRecord(t *testing.T) {
	root := steeringRoot(t, record(
		[3]string{"2.0.0", "2026-10-25", "B."},
		[3]string{"1.0.0", "2026-10-15", "A."},
	))
	for _, tc := range []struct{ version, says string }{
		{"3.0.0", "newer than"},
		{"1.5.0", "in no amendment"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			v, _ := Verdict(root, packageAt(t, "draft", tc.version))
			errs, warns := steeringFindings(v)
			if len(errs) != 0 || len(warns) != 1 || warns[0].Artifact != "prd.md" {
				t.Fatalf("errors %+v, warnings %+v; want one warning on prd.md", errs, warns)
			}
			if m := warns[0].Message; !strings.Contains(m, tc.version) || !strings.Contains(m, "2.0.0") || !strings.Contains(m, tc.says) {
				t.Errorf("message %q; want both versions and %q", m, tc.says)
			}
		})
	}
}

// The rule says nothing of a package that is history, of one with no
// version, or under a steering with none; and it is never an error.
func TestVerdict_SaysNothingWhereTheSteeringVersionDoesNotApply(t *testing.T) {
	versioned := steeringRoot(t, record([3]string{"2.0.0", "2026-10-25", "B."}, [3]string{"1.0.0", "2026-10-15", "A."}))
	for _, tc := range []struct {
		name string
		root string
		spec *afspec.Spec
	}{
		{"sealed", versioned, packageAt(t, "sealed", "1.0.0")},
		{"superseded", versioned, packageAt(t, "superseded", "1.0.0")},
		{"archived", versioned, packageAt(t, "archived", "1.0.0")},
		{"no steering_version", versioned, packageAt(t, "active", "")},
		{"an unversioned steering", steeringRoot(t, "# Steering\n\nUse make check.\n"), packageAt(t, "active", "1.0.0")},
		{"a malformed steering", steeringRoot(t, strings.Replace(record([3]string{"2.0.0", "2026-10-25", "B."}), "version: 2.0.0\namendments", "version: 2.0\namendments", 1)),
			packageAt(t, "active", "1.0.0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Verdict(tc.root, tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if errs, warns := steeringFindings(v); len(errs)+len(warns) != 0 {
				t.Errorf("errors %+v, warnings %+v; want nothing", errs, warns)
			}
			if !v.Valid {
				t.Errorf("the package does not validate: %+v", v.Errors)
			}
		})
	}
}
