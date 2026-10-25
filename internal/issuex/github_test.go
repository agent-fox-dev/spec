package issuex

import (
	"net/http"
	"testing"
)

// NewGitHub takes its API base, token and user agent from the options, then
// from the environment, then from GitHub's defaults.
func TestNewGitHub_ResolvesItsSettings(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		env                       map[string]string
		opts                      Options
		wantBase, wantTok, wantUA string
	}{
		{"options", map[string]string{"GITHUB_API_URL": "https://ignored", "GITHUB_TOKEN": "ignored"},
			Options{BaseURL: " https://ghe.example.com/api/v3/ ", Token: " tok1 ", UserAgent: "custom-ua"},
			"https://ghe.example.com/api/v3", "tok1", "custom-ua"},
		{"environment", map[string]string{"GITHUB_API_URL": "https://api.github.corp/", "GITHUB_TOKEN": "tok2", "GH_TOKEN": "ignored"},
			Options{}, "https://api.github.corp", "tok2", "spec"},
		{"GH_TOKEN", map[string]string{"GH_TOKEN": "tok3"},
			Options{}, "https://api.github.com", "tok3", "spec"},
		{"defaults", nil, Options{UserAgent: "  "}, "https://api.github.com", "", "spec"},
		{"an environment token withheld from another host", map[string]string{"GITHUB_TOKEN": "tok"},
			Options{BaseURL: "https://github.acme.com/api/v3", withholdEnvToken: true}, "https://github.acme.com/api/v3", "", "spec"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			c, err := NewGitHub(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			gh := c.(*githubClient)
			if gh.baseURL != tc.wantBase || gh.token != tc.wantTok || gh.userAgent != tc.wantUA {
				t.Errorf("NewGitHub = %q, %q, %q; want %q, %q, %q", gh.baseURL, gh.token, gh.userAgent, tc.wantBase, tc.wantTok, tc.wantUA)
			}
		})
	}
}

// GitHub's error payload becomes the HTTPError message, with each field
// error's details, so the caller sees what GitHub objected to.
func TestGitHubError_FormatsThePayload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"message", 404, `{"message": "Not Found"}`, "Not Found"},
		{"field errors", 422,
			`{"message": "Validation Failed", "errors": [{"resource": "Label", "field": "name", "code": "already_exists"}, {"field": "email", "code": "invalid", "message": "bad"}]}`,
			"Validation Failed (Label.name.already_exists) (email.invalid: bad)"},
		{"a field without a code", 422, `{"message": "Invalid", "errors": [{"field": "title"}, {"field": "body", "message": "too long"}]}`,
			"Invalid (title) (body: too long)"},
		{"a code without a field", 422, `{"message": "Invalid", "errors": [{"code": "missing"}, {"code": "custom", "message": "explained"}]}`,
			"Invalid (missing) (custom: explained)"},
		{"a bare message and an empty error", 422, `{"message": "Invalid", "errors": [{"message": "just words"}, {}]}`,
			"Invalid (just words)"},
		{"no message", 502, `{"errors": []}`, "Bad Gateway"},
		{"not JSON", 503, `<html>down</html>`, "Service Unavailable"},
		{"not JSON and no status text", 599, ` upstream says no `, "upstream says no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newGitHubHTTPError("POST", "/repos/o/r/labels", &http.Response{StatusCode: tc.status, Header: http.Header{}}, []byte(tc.body))
			if e.Message != tc.want || e.Status != tc.status || e.Method != "POST" || e.Path != "/repos/o/r/labels" {
				t.Errorf("HTTPError = %+v, want message %q", e, tc.want)
			}
		})
	}
}

func TestGitHubError_ReadsTheRateLimitHeaders(t *testing.T) {
	e := newGitHubHTTPError("GET", "/x", &http.Response{StatusCode: 403, Header: http.Header{
		"Retry-After": {"7"}, "X-Ratelimit-Reset": {"1700000000"}, "X-Ratelimit-Remaining": {"0"},
	}}, nil)
	if e.RetryAfterHeader != "7" || e.RateLimitReset != "1700000000" || e.RateLimitRemaining != "0" {
		t.Errorf("HTTPError = %+v", e)
	}
}

// GitHub omits rel="next" from the Link header on the last page.
func TestHasNextPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		links []string
		want  bool
	}{
		{"no header", nil, false},
		{"next and last", []string{`<https://api.github.com/r?page=2>; rel="next", <https://api.github.com/r?page=9>; rel="last"`}, true},
		{"last page: prev and first", []string{`<https://api.github.com/r?page=8>; rel="prev", <https://api.github.com/r?page=1>; rel="first"`}, false},
		{"next in a second header line", []string{`<https://api.github.com/r?page=1>; rel="prev"`, `<https://api.github.com/r?page=3>; rel="next"`}, true},
		{"unquoted rel", []string{`<https://api.github.com/r?page=2>; rel=next`}, true},
		{"next is not a substring match", []string{`<https://api.github.com/r?page=2>; rel="nextish"`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasNextPage(&http.Response{Header: http.Header{"Link": tc.links}}); got != tc.want {
				t.Errorf("hasNextPage = %v, want %v", got, tc.want)
			}
		})
	}
	if hasNextPage(nil) {
		t.Error("hasNextPage(nil) = true, want false")
	}
}
