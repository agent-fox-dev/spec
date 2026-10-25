package issuex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewGitLab takes its API base, token and user agent from the options, then
// from the environment, then from GitLab's defaults; the base always ends in
// /api/v4.
func TestNewGitLab_ResolvesItsSettings(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		env                       map[string]string
		opts                      Options
		wantBase, wantTok, wantUA string
	}{
		{"options", map[string]string{"GITLAB_API_URL": "https://ignored", "GITLAB_TOKEN": "ignored"},
			Options{BaseURL: "https://gitlab.example.com/subpath/", Token: "tok1", UserAgent: "custom-ua"},
			"https://gitlab.example.com/subpath/api/v4", "tok1", "custom-ua"},
		{"options naming the API", nil, Options{BaseURL: "https://gitlab.example.com/api/v4/"},
			"https://gitlab.example.com/api/v4", "", "spec"},
		{"environment", map[string]string{"GITLAB_API_URL": "https://gl.corp/api/v4/", "GITLAB_TOKEN": "tok2"},
			Options{}, "https://gl.corp/api/v4", "tok2", "spec"},
		{"defaults", nil, Options{}, "https://gitlab.com/api/v4", "", "spec"},
		{"an environment token withheld from another host", map[string]string{"GITLAB_TOKEN": "tok"},
			Options{BaseURL: "https://gitlab.acme.com", withholdEnvToken: true}, "https://gitlab.acme.com/api/v4", "", "spec"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			c, err := NewGitLab(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			gl := c.(*gitlabClient)
			if gl.baseURL != tc.wantBase || gl.token != tc.wantTok || gl.userAgent != tc.wantUA {
				t.Errorf("NewGitLab = %q, %q, %q; want %q, %q, %q", gl.baseURL, gl.token, gl.userAgent, tc.wantBase, tc.wantTok, tc.wantUA)
			}
		})
	}
}

// GitLab's error payloads come in several shapes; each becomes a readable
// HTTPError message.
func TestGitLabError_FormatsThePayload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"message", 404, `{"message": "404 Project Not Found"}`, "404 Project Not Found"},
		{"validation object", 400, `{"message": {"title": ["is too long", "is invalid"], "labels": "bad", "weight": 3}}`,
			"labels: bad; title: is too long, is invalid; weight: 3"},
		{"validation list with a non-string", 400, `{"message": {"base": ["x", 1]}}`, "base: x, 1"},
		{"error and description", 401, `{"error": "invalid_token", "error_description": "Token was revoked"}`, "invalid_token: Token was revoked"},
		{"error only", 401, `{"error": "invalid_token"}`, "invalid_token"},
		{"description only", 401, `{"error_description": "Token was revoked"}`, "Token was revoked"},
		{"empty error fields fall through to the message", 400, `{"error": "", "message": "plain"}`, "plain"},
		{"blank message", 500, `{"message": "  "}`, "Internal Server Error"},
		{"empty validation object", 400, `{"message": {}}`, "Bad Request"},
		{"no known field", 502, `{"detail": "x"}`, "Bad Gateway"},
		{"no known field and no status text", 599, `{"detail": "x"}`, `{"detail": "x"}`},
		{"not JSON", 503, ` upstream down `, "upstream down"},
		{"empty", 504, ``, "Gateway Timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newGitLabHTTPError("GET", "/projects/x", &http.Response{StatusCode: tc.status, Header: http.Header{}}, []byte(tc.body))
			if e.Message != tc.want || e.Status != tc.status {
				t.Errorf("HTTPError = %+v, want message %q", e, tc.want)
			}
		})
	}
}

// GitLab spells its rate-limit headers without the X- prefix; the older
// spelling is read when the newer is absent.
func TestGitLabError_ReadsTheRateLimitHeaders(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		header               http.Header
		wantReset, wantRemai string
	}{
		{"RateLimit-", http.Header{"Ratelimit-Reset": {"100"}, "Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"999"}}, "100", "0"},
		{"X-RateLimit-", http.Header{"X-Ratelimit-Reset": {"200"}, "X-Ratelimit-Remaining": {"5"}}, "200", "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.header
			h.Set("Retry-After", "9")
			e := newGitLabHTTPError("GET", "/x", &http.Response{StatusCode: 429, Header: h}, nil)
			if e.RateLimitReset != tc.wantReset || e.RateLimitRemaining != tc.wantRemai || e.RetryAfterHeader != "9" {
				t.Errorf("HTTPError = %+v", e)
			}
		})
	}
}

// A project is addressed by its full path, slashes escaped, so a nested
// group stays one path segment.
func TestProjectPath(t *testing.T) {
	for _, tc := range []struct {
		repo Repo
		want string
	}{
		{Repo{Owner: "group", Name: "project"}, "group%2Fproject"},
		{Repo{Owner: "group/subgroup", Name: "project"}, "group%2Fsubgroup%2Fproject"},
		{Repo{Owner: "a/b/c", Name: "d"}, "a%2Fb%2Fc%2Fd"},
		{Repo{Owner: "my group", Name: "pro?ject"}, "my%20group%2Fpro%3Fject"},
	} {
		if got := projectPath(tc.repo); got != tc.want {
			t.Errorf("projectPath(%+v) = %q, want %q", tc.repo, got, tc.want)
		}
	}
}

// PRIVATE-TOKEN is a header of GitLab's own, which Go does not strip on a
// redirect to another host the way it strips Authorization; the default
// client strips it.
func TestNewGitLab_DefaultClientKeepsTheTokenOnItsHost(t *testing.T) {
	clearForgeEnv(t)
	tokens := map[string]string{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens["other host"] = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte(`{"iid": 1}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/o%2Fr/issues/1", "/api/v4/projects/o/r/issues/1":
			http.Redirect(w, r, "/api/v4/moved/1", http.StatusFound)
		case "/api/v4/moved/1":
			tokens["same host"] = r.Header.Get("PRIVATE-TOKEN")
			http.Redirect(w, r, other.URL+"/elsewhere", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	c, err := NewGitLab(Options{BaseURL: srv.URL, Token: "glpat-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}); err != nil {
		t.Fatal(err)
	}
	if tokens["same host"] != "glpat-secret" || tokens["other host"] != "" {
		t.Errorf("PRIVATE-TOKEN seen = %q", tokens)
	}
}

func TestDropTokenAcrossHosts_StopsAfterTenRedirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://gitlab.com/x", nil)
	if err := dropTokenAcrossHosts(req, make([]*http.Request, 10)); err == nil {
		t.Error("an eleventh redirect was followed")
	}
}
