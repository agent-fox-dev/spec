package issuex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Issue #222 (2): a credential goes only to the host its configuration names.
func TestTheTokenGoesOnlyToTheHostItIsConfiguredFor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		host      string
		wantType  ForgeType
		wantBase  string
		wantToken string
	}{
		{"self-hosted GitLab, no API URL", map[string]string{"GITLAB_TOKEN": "gl"},
			"gitlab.acme.com", ForgeTypeGitLab, "https://gitlab.acme.com/api/v4", ""},
		{"self-hosted GitHub, no API URL", map[string]string{"GITHUB_TOKEN": "gh"},
			"github.acme.com", ForgeTypeGitHub, "https://github.acme.com/api/v3", ""},
		{"github.com with a GHES API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://ghe.acme.com/api/v3"},
			"github.com", ForgeTypeGitHub, "https://api.github.com", ""},
		{"github.com with its own API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://api.github.com"},
			"github.com", ForgeTypeGitHub, "https://api.github.com", "gh"},
		{"github.com, no API URL", map[string]string{"GITHUB_TOKEN": "gh"},
			"github.com", ForgeTypeGitHub, "https://api.github.com", "gh"},
		{"GHES named by its API URL", map[string]string{"GITHUB_TOKEN": "gh", "GITHUB_API_URL": "https://ghe.acme.com/api/v3"},
			"ghe.acme.com", ForgeTypeGitHub, "https://ghe.acme.com/api/v3", "gh"},
		{"gitlab.com", map[string]string{"GITLAB_TOKEN": "gl"},
			"gitlab.com", ForgeTypeGitLab, "https://gitlab.com/api/v4", "gl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_API_URL", "GITLAB_TOKEN", "GITLAB_API_URL"} {
				t.Setenv(k, tc.env[k])
			}
			ft, opts, err := detectForge(Options{Repo: Repo{Host: tc.host, Owner: "g", Name: "p"}})
			if err != nil {
				t.Fatalf("detectForge: %v", err)
			}
			if ft != tc.wantType || opts.BaseURL != tc.wantBase {
				t.Errorf("forge %q at %q, want %q at %q", ft, opts.BaseURL, tc.wantType, tc.wantBase)
			}
			c, err := NewWithOptions(Options{Repo: Repo{Host: tc.host, Owner: "g", Name: "p"}})
			if err != nil {
				t.Fatalf("NewWithOptions: %v", err)
			}
			if got := c.Authenticated(); got != (tc.wantToken != "") {
				t.Errorf("Authenticated() = %v, want a token only when it belongs to this host (%q)", got, tc.wantToken)
			}
		})
	}
}

// Issue #222 (3): forge detection reads the origin of --dir, not of the
// process's working directory, and a host named by an API URL is that forge.
func TestDetectionReadsTheOriginOfTheGivenDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, argv := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "remote", "add", "origin", "https://gitlab.com/team/app.git"},
	} {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", argv, err, out)
		}
	}
	// Both forges configured: the origin decides. This test runs in a
	// checkout whose own origin is on GitHub.
	t.Setenv("GITHUB_TOKEN", "gh")
	t.Setenv("GITLAB_TOKEN", "gl")
	t.Setenv("GITHUB_API_URL", "")
	t.Setenv("GITLAB_API_URL", "")
	ft, _, err := detectForge(Options{Dir: dir})
	if err != nil || ft != ForgeTypeGitLab {
		t.Errorf("forge = %q (%v), want gitlab from %s's origin", ft, err, dir)
	}

	// A self-hosted host is matched against the configured API URLs before
	// anything else, whatever the checkout's origin.
	t.Setenv("GITLAB_API_URL", "https://git.example.com")
	ft, opts, err := detectForge(Options{Dir: dir, Repo: Repo{Host: "git.example.com", Owner: "team", Name: "app"}})
	if err != nil || ft != ForgeTypeGitLab || !strings.HasPrefix(opts.BaseURL, "https://git.example.com") {
		t.Errorf("forge = %q at %q (%v), want gitlab at git.example.com", ft, opts.BaseURL, err)
	}
}

// Issue #222 (smaller): GitHub's secondary rate limit — 403 while requests
// remain, saying so in its message — is a rate limit, waited out once. A 403
// with requests remaining and no such message is a refusal (TS-01-21).
func TestASecondaryRateLimitIsARateLimit(t *testing.T) {
	e := &HTTPError{Status: http.StatusForbidden, RetryAfterHeader: "3", RateLimitRemaining: "4999",
		Message: "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}
	if wait, ok := e.RetryAfter(); !ok || wait < 3*time.Second {
		t.Errorf("RetryAfter = %v, %v", wait, ok)
	}
	if !IsRateLimited(e) {
		t.Error("IsRateLimited = false for a secondary rate limit")
	}
	if _, ok := (&HTTPError{Status: http.StatusForbidden, RateLimitRemaining: "4999"}).RetryAfter(); ok {
		t.Error("a plain 403 is treated as a rate limit")
	}
}

// Issue #222 (smaller): the rate-limit wait ends when the run is cancelled.
func TestTheRateLimitWaitHonoursCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, err := NewGitHub(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = c.ReadIssue(ctx, IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the run waited %s past its cancellation", d)
	}
}

// Issue #222 (smaller): issue and PR URLs with a sub-page, and GitLab's
// legacy form without /-/, are read as the thread they name.
func TestParseIssueURLAcceptsSubPagesAndLegacyGitLabPaths(t *testing.T) {
	for raw, want := range map[string]IssueRef{
		"https://github.com/o/r/pull/42/files":            {Repo: Repo{Host: "github.com", Owner: "o", Name: "r"}, Number: 42, IsPullRequest: true},
		"https://github.com/o/r/pull/42/commits":          {Repo: Repo{Host: "github.com", Owner: "o", Name: "r"}, Number: 42, IsPullRequest: true},
		"https://gitlab.com/g/p/-/merge_requests/5/diffs": {Repo: Repo{Host: "gitlab.com", Owner: "g", Name: "p"}, Number: 5, IsPullRequest: true},
		"https://gitlab.com/g/sub/proj/issues/5":          {Repo: Repo{Host: "gitlab.com", Owner: "g/sub", Name: "proj"}, Number: 5},
		"https://gitlab.com/g/p/merge_requests/9":         {Repo: Repo{Host: "gitlab.com", Owner: "g", Name: "p"}, Number: 9, IsPullRequest: true},
		"https://gitlab.com/g/sub/proj/-/issues/7":        {Repo: Repo{Host: "gitlab.com", Owner: "g/sub", Name: "proj"}, Number: 7},
		"https://github.com/o/r/issues/3":                 {Repo: Repo{Host: "github.com", Owner: "o", Name: "r"}, Number: 3},
	} {
		got, ok := ParseIssueURL(raw)
		if !ok || got != want {
			t.Errorf("ParseIssueURL(%s) = %+v, %v; want %+v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://github.com/o/r/pull/42/unknown",
		"https://github.com/o/r/issues/x",
		"https://github.com/o/r",
	} {
		if _, ok := ParseIssueURL(raw); ok {
			t.Errorf("ParseIssueURL(%s) accepted", raw)
		}
	}
}
