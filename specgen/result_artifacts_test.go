package specgen

import (
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// One spec_package artifact per package the run wrote, its validity read off
// the package's own verdict, and a comment artifact for each comment posted;
// nothing for a result that wrote nothing.
func TestResult_Artifacts(t *testing.T) {
	r := &Result{}
	r.Package = Package{SpecDir: ".specs/06_trim", SpecID: "06", Validation: ValidationReport{Valid: true},
		CommentURL: "https://github.com/o/r/issues/1#issuecomment-1"}
	r.FollowOnSpecs = []Package{{SpecDir: ".specs/07_next", SpecID: "07"}}
	want := []cli.Artifact{
		{Kind: cli.ArtifactSpecPackage, Path: ".specs/06_trim", ID: "06", Valid: true},
		{Kind: cli.ArtifactComment, URL: "https://github.com/o/r/issues/1#issuecomment-1", Role: "prd"},
		{Kind: cli.ArtifactSpecPackage, Path: ".specs/07_next", ID: "07", Valid: false},
	}
	got := r.Artifacts()
	if len(got) != len(want) {
		t.Fatalf("artifacts = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("artifact %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := (&Result{}).Artifacts(); len(got) != 0 {
		t.Errorf("an unwritten result's artifacts = %+v", got)
	}
	if got := (*Result)(nil).Artifacts(); got != nil {
		t.Errorf("a nil result's artifacts = %+v", got)
	}
}
