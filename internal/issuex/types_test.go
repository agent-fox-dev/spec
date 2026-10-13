package issuex_test

import (
	"fmt"
	"testing"
	"testing/quick"

	"github.com/agent-fox-dev/spec/internal/issuex"
)

// TestRepoValid verifies TS-01-2: Repo Valid method reports true only when both Owner and Name are non-empty.
// Verifies: 01-REQ-2.1
func TestRepoValid_TS_01_2(t *testing.T) {
	tests := []struct {
		repo issuex.Repo
		want bool
	}{
		{issuex.Repo{Owner: "foo", Name: "bar"}, true},
		{issuex.Repo{Owner: "group/sub", Name: "bar"}, true},
		{issuex.Repo{Owner: "", Name: "bar"}, false},
		{issuex.Repo{Owner: "foo", Name: ""}, false},
		{issuex.Repo{}, false},
	}

	for _, tc := range tests {
		if got := tc.repo.Valid(); got != tc.want {
			t.Errorf("Repo{%q, %q}.Valid() = %v, want %v", tc.repo.Owner, tc.repo.Name, got, tc.want)
		}
	}
}

// TestRepoStringProperty verifies TS-01-3: Repo String returns owner joined with name by slash for any non-empty segments.
// Verifies: 01-REQ-2.2
func TestRepoStringProperty_TS_01_3(t *testing.T) {
	f := func(owner, name string) bool {
		r := issuex.Repo{Owner: owner, Name: name}
		return r.String() == fmt.Sprintf("%s/%s", owner, name)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Errorf("Repo.String() property failed: %v", err)
	}
}

// TestIssueRefStringProperty verifies TS-01-4: IssueRef String returns repo string and issue number joined by hash.
// Verifies: 01-REQ-2.3
func TestIssueRefStringProperty_TS_01_4(t *testing.T) {
	f := func(owner, name string, num int) bool {
		r := issuex.Repo{Owner: owner, Name: name}
		ref := issuex.IssueRef{Repo: r, Number: num}
		return ref.String() == fmt.Sprintf("%s#%d", r.String(), num)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Errorf("IssueRef.String() property failed: %v", err)
	}
}

func TestIssueRefURL(t *testing.T) {
	tests := []struct {
		ref  issuex.IssueRef
		want string
	}{
		{
			ref:  issuex.IssueRef{Repo: issuex.Repo{Owner: "foo", Name: "bar"}, Number: 42, IsPullRequest: false},
			want: "https://github.com/foo/bar/issues/42",
		},
		{
			ref:  issuex.IssueRef{Repo: issuex.Repo{Owner: "foo", Name: "bar"}, Number: 42, IsPullRequest: true},
			want: "https://github.com/foo/bar/pull/42",
		},
		{
			ref:  issuex.IssueRef{Repo: issuex.Repo{Host: "gitlab.com", Owner: "group/sub", Name: "proj"}, Number: 10, IsPullRequest: false},
			want: "https://gitlab.com/group/sub/proj/-/issues/10",
		},
		{
			ref:  issuex.IssueRef{Repo: issuex.Repo{Host: "gitlab.com", Owner: "group/sub", Name: "proj"}, Number: 10, IsPullRequest: true},
			want: "https://gitlab.com/group/sub/proj/-/merge_requests/10",
		},
	}

	for _, tc := range tests {
		if got := tc.ref.URL(); got != tc.want {
			t.Errorf("IssueRef.URL() = %q, want %q", got, tc.want)
		}
	}
}
