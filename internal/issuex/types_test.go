package issuex

import "testing"

func TestRepo_Valid(t *testing.T) {
	for _, tc := range []struct {
		repo Repo
		want bool
	}{
		{Repo{Owner: "foo", Name: "bar"}, true},
		{Repo{Owner: "group/sub", Name: "bar"}, true},
		{Repo{Owner: "", Name: "bar"}, false},
		{Repo{Owner: "foo", Name: ""}, false},
		{Repo{}, false},
	} {
		if got := tc.repo.Valid(); got != tc.want {
			t.Errorf("Repo{%q, %q}.Valid() = %v, want %v", tc.repo.Owner, tc.repo.Name, got, tc.want)
		}
	}
}

func TestIssueRef_StringNamesTheRepositoryAndNumber(t *testing.T) {
	for _, tc := range []struct {
		ref  IssueRef
		repo string
		want string
	}{
		{IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 42}, "o/r", "o/r#42"},
		{IssueRef{Repo: Repo{Owner: "group/sub", Name: "proj", Host: "gitlab.com"}, Number: 7, IsPullRequest: true}, "group/sub/proj", "group/sub/proj#7"},
	} {
		if got := tc.ref.Repo.String(); got != tc.repo {
			t.Errorf("Repo.String() = %q, want %q", got, tc.repo)
		}
		if got := tc.ref.String(); got != tc.want {
			t.Errorf("IssueRef.String() = %q, want %q", got, tc.want)
		}
	}
}

// URL is the thread's web page: GitHub's issues or pull path, GitLab's /-/
// issues or merge_requests path, on the ref's host (github.com when none).
func TestIssueRef_URL(t *testing.T) {
	for _, tc := range []struct {
		ref  IssueRef
		want string
	}{
		{IssueRef{Repo: Repo{Owner: "foo", Name: "bar"}, Number: 42}, "https://github.com/foo/bar/issues/42"},
		{IssueRef{Repo: Repo{Owner: "foo", Name: "bar"}, Number: 42, IsPullRequest: true}, "https://github.com/foo/bar/pull/42"},
		{IssueRef{Repo: Repo{Host: "gitlab.com", Owner: "group/sub", Name: "proj"}, Number: 10}, "https://gitlab.com/group/sub/proj/-/issues/10"},
		{IssueRef{Repo: Repo{Host: "gitlab.com", Owner: "group/sub", Name: "proj"}, Number: 10, IsPullRequest: true}, "https://gitlab.com/group/sub/proj/-/merge_requests/10"},
		{IssueRef{Repo: Repo{Host: " https://ghe.acme.com/ ", Owner: "o", Name: "r"}, Number: 1}, "https://ghe.acme.com/o/r/issues/1"},
		{IssueRef{Repo: Repo{Host: "http://GitLab.local", Owner: "o", Name: "r"}, Number: 2}, "http://GitLab.local/o/r/-/issues/2"},
	} {
		if got := tc.ref.URL(); got != tc.want {
			t.Errorf("%+v.URL() = %q, want %q", tc.ref, got, tc.want)
		}
	}
}
