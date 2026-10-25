package issuex

import (
	"testing"
)

func TestParseRepo(t *testing.T) {
	for _, tc := range []struct {
		in     string
		want   Repo
		wantOK bool
	}{
		{"octocat/Hello-World", Repo{Owner: "octocat", Name: "Hello-World"}, true},
		{"gitlab-org/subgroup/project", Repo{Owner: "gitlab-org/subgroup", Name: "project"}, true},
		{"   org/nested/deep/repo   ", Repo{Owner: "org/nested/deep", Name: "repo"}, true},
		{"o/r.git", Repo{Owner: "o", Name: "r"}, true},
		{"", Repo{}, false},
		{"singleword", Repo{}, false},
		{"/leading", Repo{}, false},
		{"trailing/", Repo{}, false},
		{"a//b", Repo{}, false},
		{"o/.git", Repo{}, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseRepo(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ParseRepo(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// A remote names a repository on a forge in any of git's URL syntaxes; a
// host that is not a known forge, or a path that is not owner/name, is none.
func TestParseRemote(t *testing.T) {
	setEnv(t, map[string]string{"GITHUB_API_URL": "https://api.ghe.acme.com", "GITLAB_API_URL": "https://code.acme.com/api/v4"})
	for _, tc := range []struct {
		in     string
		want   Repo
		wantOK bool
	}{
		{"https://github.com/octocat/Hello-World.git", Repo{Host: "github.com", Owner: "octocat", Name: "Hello-World"}, true},
		{"https://github.com/octocat/Hello-World", Repo{Host: "github.com", Owner: "octocat", Name: "Hello-World"}, true},
		{"git@gitlab.com:group/subgroup/repo.git", Repo{Host: "gitlab.com", Owner: "group/subgroup", Name: "repo"}, true},
		{"git@gitlab.com:deep/sub1/sub2/project.git", Repo{Host: "gitlab.com", Owner: "deep/sub1/sub2", Name: "project"}, true},
		{"ssh://git@github.com/org/repo.git", Repo{Host: "github.com", Owner: "org", Name: "repo"}, true},
		{"ssh://git@GitLab.example.com:2222/org/repo.git", Repo{Host: "gitlab.example.com", Owner: "org", Name: "repo"}, true},
		{"git://www.github.com/org/repo", Repo{Host: "github.com", Owner: "org", Name: "repo"}, true},
		{"https://ghe.acme.com/org/repo.git", Repo{Host: "ghe.acme.com", Owner: "org", Name: "repo"}, true},
		{"git@code.acme.com:team/app.git", Repo{Host: "code.acme.com", Owner: "team", Name: "app"}, true},
		{"", Repo{}, false},
		{"http://", Repo{}, false},
		{"not-a-remote-url", Repo{}, false},
		{"ftp://github.com/o/r.git", Repo{}, false},
		{"https://bitbucket.org/owner/repo.git", Repo{}, false},
		{"https://github.com/", Repo{}, false},
		{"https://github.com/onlyone", Repo{}, false},
		{"https://github.com/a//b", Repo{}, false},
		{"https://github.com/%zz/r", Repo{}, false},
		{"git@github.com:", Repo{}, false},
		{"git@:repo.git", Repo{}, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseRemote(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ParseRemote(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDetectRepo_ReadsTheOriginRemote(t *testing.T) {
	dir := gitRepoWithOrigin(t, "git@github.com:agentfox/agent-fox.git")
	got, ok := DetectRepo(dir)
	if want := (Repo{Host: "github.com", Owner: "agentfox", Name: "agent-fox"}); !ok || got != want {
		t.Errorf("DetectRepo = %+v, %v; want %+v", got, ok, want)
	}
}

// Without a repository or an origin there is nothing to detect, and that is
// no error.
func TestDetectRepo_FindsNothingOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	if got, ok := DetectRepo(dir); ok || got != (Repo{}) {
		t.Errorf("DetectRepo(non-git dir) = %+v, %v", got, ok)
	}
	t.Chdir(dir)
	if got, ok := DetectRepo(""); ok || got != (Repo{}) {
		t.Errorf("DetectRepo(\"\") in a non-git working directory = %+v, %v", got, ok)
	}
}

// An issue, pull request or merge request URL is read as the thread it names:
// GitHub's and GitLab's forms, GitLab's legacy form without /-/, with a
// sub-page of a pull or merge request, a trailing slash, a query or a
// fragment.
func TestParseIssueURL_ReadsTheThread(t *testing.T) {
	clearForgeEnv(t)
	gh := func(n int, pr bool) IssueRef {
		return IssueRef{Repo: Repo{Host: "github.com", Owner: "o", Name: "r"}, Number: n, IsPullRequest: pr}
	}
	gl := func(owner string, n int, mr bool) IssueRef {
		return IssueRef{Repo: Repo{Host: "gitlab.com", Owner: owner, Name: "proj"}, Number: n, IsPullRequest: mr}
	}
	for raw, want := range map[string]IssueRef{
		"https://github.com/o/r/issues/123":                       gh(123, false),
		"https://github.com/o/r/pull/456":                         gh(456, true),
		"https://github.com/o/r/pulls/456":                        gh(456, true),
		"https://www.github.com/o/r/issues/3":                     gh(3, false),
		"http://github.com/o/r/issues/3":                          gh(3, false),
		"https://github.com/o/r/issues/123/?foo=bar#anchor":       gh(123, false),
		"https://github.com/o/r/pull/42/files":                    gh(42, true),
		"https://github.com/o/r/pull/42/commits":                  gh(42, true),
		"https://gitlab.com/g/sub/proj/-/issues/789":              gl("g/sub", 789, false),
		"https://gitlab.com/g/sub/proj/-/issues/789/":             gl("g/sub", 789, false),
		"https://gitlab.com/g/sub/proj/-/merge_requests/101":      gl("g/sub", 101, true),
		"https://gitlab.com/g/proj/-/merge_requests/5/diffs":      gl("g", 5, true),
		"https://gitlab.com/g/sub/proj/issues/5":                  gl("g/sub", 5, false),
		"https://gitlab.com/g/proj/merge_requests/9":              gl("g", 9, true),
		"https://gitlab.com/g/proj/-/merge_requests/9/pipelines/": gl("g", 9, true),
	} {
		t.Run(raw, func(t *testing.T) {
			got, ok := ParseIssueURL(raw)
			if !ok || got != want {
				t.Errorf("ParseIssueURL = %+v, %v; want %+v", got, ok, want)
			}
		})
	}
}

func TestParseIssueURL_RefusesWhatIsNotAThread(t *testing.T) {
	clearForgeEnv(t)
	for _, raw := range []string{
		"",
		"not-a-url",
		"ftp://github.com/o/r/issues/1",
		"https://github.com",
		"https://example.com/owner/repo/issues/123",
		"https://github.com/o/r",
		"https://github.com/o/r/issues/abc",
		"https://github.com/o/r/issues/0",
		"https://github.com/o/r/issues/-5",
		"https://github.com/o/r/commits/123",
		"https://github.com/o/r/pull/42/unknown",
		"https://github.com/o/r/issues/42/files",
		"https://github.com/o/r/pull/42/files/extra",
		"https://github.com/o/r/merge_requests/4",
		"https://github.com/group/sub/r/issues/4",
		"https://gitlab.com/g/sub/proj/-/issues/xyz",
		"https://gitlab.com/g/sub/proj/-/unknown/123",
		"https://gitlab.com/proj/-/issues/123",
		"https://gitlab.com/g//proj/-/issues/123",
	} {
		t.Run(raw, func(t *testing.T) {
			if got, ok := ParseIssueURL(raw); ok || got != (IssueRef{}) {
				t.Errorf("ParseIssueURL = %+v, %v; want nothing", got, ok)
			}
		})
	}
}

// A canonical thread URL parses to the ref whose URL it is.
func TestParseIssueURL_RoundTripsThroughURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/agent-fox-dev/agentfox/issues/42",
		"https://github.com/agent-fox-dev/agentfox/pull/101",
		"https://gitlab.com/group/subgroup/project/-/issues/7",
		"https://gitlab.com/group/subgroup/project/-/merge_requests/88",
	} {
		ref, ok := ParseIssueURL(raw)
		if !ok {
			t.Errorf("ParseIssueURL(%s) failed", raw)
			continue
		}
		if got := ref.URL(); got != raw {
			t.Errorf("ParseIssueURL(%s).URL() = %s", raw, got)
		}
	}
}
