package issuex

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// clearForgeEnv empties every variable forge detection reads, so a
// developer's own tokens and API URLs cannot decide a test.
func clearForgeEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"GITHUB_API_URL", "GITHUB_TOKEN", "GH_TOKEN", "GITLAB_API_URL", "GITLAB_TOKEN"} {
		t.Setenv(v, "")
	}
}

// setEnv sets the forge variables to env after clearing them all.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	clearForgeEnv(t)
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// gitRepoWithOrigin is a fresh git repository whose origin is originURL.
func gitRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, argv := range [][]string{
		{"git", "init", "-q", "-b", "main", dir},
		{"git", "-C", dir, "remote", "add", "origin", originURL},
	} {
		if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", argv, err, out)
		}
	}
	return dir
}

// withAdapter replaces the adapter registered for a forge for the test's
// duration (nil removes it) and restores what was there.
func withAdapter(t *testing.T, forge ForgeType, factory func(Options) (Client, error)) {
	t.Helper()
	adaptersMu.Lock()
	old, had := adapters[forge]
	if factory == nil {
		delete(adapters, forge)
	} else {
		adapters[forge] = factory
	}
	adaptersMu.Unlock()
	t.Cleanup(func() {
		adaptersMu.Lock()
		defer adaptersMu.Unlock()
		if had {
			adapters[forge] = old
		} else {
			delete(adapters, forge)
		}
	})
}

// scriptedTransport is an http.RoundTripper that answers with Responses in
// order, then with an empty 200, and records what it was asked.
type scriptedTransport struct {
	Responses    []*http.Response
	RequestCount int
	Requests     []*http.Request
}

func (tt *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tt.RequestCount++
	tt.Requests = append(tt.Requests, req)
	if len(tt.Responses) == 0 {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}
	resp := tt.Responses[0]
	tt.Responses = tt.Responses[1:]
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	if resp.Body == nil {
		resp.Body = io.NopCloser(strings.NewReader(""))
	}
	if resp.Request == nil {
		resp.Request = req
	}
	return resp, nil
}

// scriptedForge is the shared request executor over a scriptedTransport.
func scriptedForge(tt *scriptedTransport, sleep func(time.Duration)) forgeHTTP {
	return forgeHTTP{
		baseURL:  "https://api.example.com",
		client:   &http.Client{Transport: tt},
		sleep:    sleep,
		newError: newGitHubHTTPError,
	}
}

// idleCloser is a RoundTripper that counts CloseIdleConnections calls, as
// http.Client.CloseIdleConnections makes them on its Transport.
type idleCloser struct{ closed atomic.Int32 }

func (c *idleCloser) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("no requests expected")
}
func (c *idleCloser) CloseIdleConnections() { c.closed.Add(1) }

// forge describes one forge to the tests that hold both clients to the same
// contract: how to build its client, and how its server spells an issue, a
// comment, a comment page and the comments collection.
type forge struct {
	name      string
	newClient func(Options) (Client, error)
	// issueJSON is an issue numbered by its %d.
	issueJSON string
	// commentJSON is a comment whose body is "comment %d".
	commentJSON string
	// commentsSuffix ends the path of the comments collection.
	commentsSuffix string
	// commentURLJSON is the reply to a posted comment, carrying commentURL.
	commentURLJSON string
	commentURL     string
	// announceNext tells the client there is a page after this one.
	announceNext func(w http.ResponseWriter, r *http.Request, next int)
	// accept and authHeader are the headers the forge's API expects.
	accept     string
	authHeader string
	authValue  func(token string) string
}

var forges = []forge{
	{
		name:           "github",
		newClient:      NewGitHub,
		issueJSON:      `{"number": %d, "title": "issue", "state": "open"}`,
		commentJSON:    `{"body": "comment %d", "user": {"login": "u"}}`,
		commentsSuffix: "/comments",
		commentURLJSON: `{"html_url": "https://github.com/o/r/issues/1#issuecomment-1"}`,
		commentURL:     "https://github.com/o/r/issues/1#issuecomment-1",
		announceNext:   githubNext,
		accept:         "application/vnd.github+json",
		authHeader:     "Authorization",
		authValue:      func(tok string) string { return "Bearer " + tok },
	},
	{
		name:           "gitlab",
		newClient:      NewGitLab,
		issueJSON:      `{"iid": %d, "title": "issue", "state": "opened"}`,
		commentJSON:    `{"id": %d, "body": "comment %[1]d", "system": false, "author": {"username": "u"}}`,
		commentsSuffix: "/notes",
		commentURLJSON: `{"id": 42, "body": "a note", "web_url": "https://gitlab.com/o/r/-/issues/1#note_42"}`,
		commentURL:     "https://gitlab.com/o/r/-/issues/1#note_42",
		announceNext:   gitlabNext,
		accept:         "application/json",
		authHeader:     "PRIVATE-TOKEN",
		authValue:      func(tok string) string { return tok },
	},
}

// client builds f's client against baseURL, failing the test on an error.
func (f forge) client(t *testing.T, o Options) Client {
	t.Helper()
	c, err := f.newClient(o)
	if err != nil {
		t.Fatalf("%s client: %v", f.name, err)
	}
	return c
}

func githubNext(w http.ResponseWriter, r *http.Request, next int) {
	w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=%d>; rel="next", <http://%s%s?page=99>; rel="last"`, r.Host, r.URL.Path, next, r.Host, r.URL.Path))
}

func gitlabNext(w http.ResponseWriter, _ *http.Request, next int) {
	w.Header().Set("X-Next-Page", strconv.Itoa(next))
}

// pagedServer serves pages (JSON arrays) in order, one per ?page=N, and
// answers an unknown page with []. announceNext is called for every response
// with the next page number when one exists, and is where a test says how its
// forge signals "there is another page". A server whose last page is exactly
// full signals nothing.
func pagedServer(t *testing.T, pages []string, announceNext func(w http.ResponseWriter, r *http.Request, next int)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			page = 1
		}
		if page < len(pages) {
			announceNext(w, r, page+1)
		}
		if page > len(pages) {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(pages[page-1]))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// jsonItems renders n objects of the form format, which takes the object's
// running number, numbered from first.
func jsonItems(first, n int, format string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(format, first+i)
	}
	return "[" + strings.Join(items, ",") + "]"
}
