package issuex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests in this file hold the GitHub and the GitLab client to the same
// contract: whatever the forge, a caller of Client sees the same behaviour.

func TestClient_AuthenticatedOnlyWithAToken(t *testing.T) {
	clearForgeEnv(t)
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			if !f.client(t, Options{Token: "secret"}).Authenticated() {
				t.Error("a client with a token is not authenticated")
			}
			if f.client(t, Options{}).Authenticated() {
				t.Error("a client without a token is authenticated")
			}
		})
	}
}

func TestClient_CloseClosesTheIdleConnectionsOfItsTransport(t *testing.T) {
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			spy := &idleCloser{}
			c := f.client(t, Options{Token: "t", HTTPClient: &http.Client{Transport: spy}})
			if err := c.Close(); err != nil {
				t.Fatalf("Close() = %v, want nil", err)
			}
			if got := spy.closed.Load(); got != 1 {
				t.Errorf("CloseIdleConnections was called %d times, want 1", got)
			}
		})
	}
}

// A caller's http.Client is the one the adapter uses, whatever its Timeout,
// so a caller that relies on context deadlines gets no hidden cap and its
// later changes to the client reach the adapter. The 30 s default is only for
// a caller that supplies none.
func TestClient_UsesTheSuppliedHTTPClientAsIs(t *testing.T) {
	clearForgeEnv(t)
	clientOf := func(t *testing.T, c Client) *http.Client {
		t.Helper()
		switch c := c.(type) {
		case *githubClient:
			return c.httpClient
		case *gitlabClient:
			return c.httpClient
		}
		t.Fatalf("unexpected client type %T", c)
		return nil
	}
	constructors := map[string]func(Options) (Client, error){
		"NewGitHub": NewGitHub,
		"NewGitLab": NewGitLab,
		"NewWithOptions (GitHub base URL)": func(o Options) (Client, error) {
			o.BaseURL = "https://api.github.com"
			return NewWithOptions(o)
		},
		"NewWithOptions (GitLab base URL)": func(o Options) (Client, error) {
			o.BaseURL = "https://gitlab.com/api/v4"
			return NewWithOptions(o)
		},
	}
	for name, construct := range constructors {
		t.Run(name, func(t *testing.T) {
			for label, supplied := range map[string]*http.Client{
				"no timeout":    {},
				"own timeout":   {Timeout: 5 * time.Minute},
				"own transport": {Transport: http.DefaultTransport},
			} {
				c, err := construct(Options{HTTPClient: supplied})
				if err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				if got := clientOf(t, c); got != supplied || got.Timeout != supplied.Timeout {
					t.Errorf("%s: adapter uses a different *http.Client than the one supplied (timeout %v)", label, got.Timeout)
				}
			}
			c, err := construct(Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got := clientOf(t, c); got == nil || got.Timeout != 30*time.Second {
				t.Errorf("default client = %+v, want a 30 s timeout", got)
			}
		})
	}
}

// Every request carries the forge's own headers; the credential only when
// there is one, and a JSON content type only with a body.
func TestClient_SendsTheForgeHeaders(t *testing.T) {
	clearForgeEnv(t)
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			var (
				mu   sync.Mutex
				seen []*http.Request
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Clone(context.Background()))
				mu.Unlock()
				switch {
				case r.Method == http.MethodPost:
					_, _ = w.Write([]byte(f.commentURLJSON))
				case strings.HasSuffix(r.URL.Path, f.commentsSuffix):
					_, _ = w.Write([]byte(`[]`))
				default:
					_, _ = fmt.Fprintf(w, f.issueJSON, 1)
				}
			}))
			defer srv.Close()
			ref := IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}

			if _, err := f.client(t, Options{BaseURL: srv.URL, Token: "s3cret", UserAgent: "ua/1"}).AddComment(context.Background(), ref, "hi"); err != nil {
				t.Fatalf("AddComment: %v", err)
			}
			if _, err := f.client(t, Options{BaseURL: srv.URL}).ReadIssue(context.Background(), ref); err != nil {
				t.Fatalf("ReadIssue: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(seen) < 2 {
				t.Fatalf("server saw %d requests, want at least 2", len(seen))
			}
			post, get := seen[0], seen[1]
			for _, r := range seen {
				if r.Header.Get("Accept") != f.accept {
					t.Errorf("%s %s: Accept = %q, want %q", r.Method, r.URL.Path, r.Header.Get("Accept"), f.accept)
				}
			}
			if got := post.Header.Get(f.authHeader); got != f.authValue("s3cret") {
				t.Errorf("POST %s = %q, want %q", f.authHeader, got, f.authValue("s3cret"))
			}
			if got := post.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("POST Content-Type = %q, want application/json", got)
			}
			if got := post.Header.Get("User-Agent"); got != "ua/1" {
				t.Errorf("POST User-Agent = %q, want ua/1", got)
			}
			if got := get.Header.Get(f.authHeader); got != "" {
				t.Errorf("unauthenticated GET sent %s %q", f.authHeader, got)
			}
			if got := get.Header.Get("Content-Type"); got != "" {
				t.Errorf("GET without a body sent Content-Type %q", got)
			}
			if got := get.Header.Get("User-Agent"); got != "spec" {
				t.Errorf("default User-Agent = %q, want spec", got)
			}
		})
	}
}

// An error status is the package's sentinel for it, with the HTTPError
// inside. GitLab also reports a merge request it cannot accept as 406.
func TestClient_MapsErrorStatusesToSentinels(t *testing.T) {
	for _, f := range forges {
		for _, tc := range []struct {
			status   int
			notFound bool
			conflict bool
		}{
			{status: http.StatusNotFound, notFound: true},
			{status: http.StatusMethodNotAllowed, conflict: true},
			{status: http.StatusConflict, conflict: true},
			{status: http.StatusNotAcceptable, conflict: f.name == "gitlab"},
			{status: http.StatusInternalServerError},
		} {
			t.Run(fmt.Sprintf("%s %d", f.name, tc.status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"message": "the forge said no"}`))
				}))
				defer srv.Close()
				_, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).ReadIssue(context.Background(),
					IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1})
				if err == nil {
					t.Fatal("no error")
				}
				if IsNotFound(err) != tc.notFound || IsConflict(err) != tc.conflict {
					t.Errorf("IsNotFound = %v, IsConflict = %v, want %v, %v: %v", IsNotFound(err), IsConflict(err), tc.notFound, tc.conflict, err)
				}
				var he *HTTPError
				if !errors.As(err, &he) || he.Status != tc.status || he.Message != "the forge said no" {
					t.Errorf("error %v does not carry the HTTPError with status %d and the forge's message", err, tc.status)
				}
			})
		}
	}
}

// An unauthenticated 404 may be a private thread, and the error says so.
func TestClient_ReadIssueSaysAnUnauthenticated404MayBePrivate(t *testing.T) {
	clearForgeEnv(t)
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message": "Not Found"}`))
			}))
			defer srv.Close()
			_, err := f.client(t, Options{BaseURL: srv.URL}).ReadIssue(context.Background(),
				IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 999})
			if !IsNotFound(err) {
				t.Errorf("IsNotFound = false: %v", err)
			}
			if err == nil || !strings.Contains(err.Error(), "private") {
				t.Errorf("the error does not say the thread may be private: %v", err)
			}
		})
	}
}

// The thread is read with its comments. A failure to read the comments does
// not lose the issue: it comes back with CommentsErr set.
func TestClient_ReadIssueReturnsTheThreadAndKeepsItWhenCommentsFail(t *testing.T) {
	for _, f := range forges {
		for _, failComments := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s comments fail %v", f.name, failComments), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, f.commentsSuffix) {
						if failComments {
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						_, _ = w.Write([]byte(jsonItems(1, 2, f.commentJSON)))
						return
					}
					_, _ = fmt.Fprintf(w, f.issueJSON, 42)
				}))
				defer srv.Close()
				thread, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).ReadIssue(context.Background(),
					IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 42})
				if err != nil {
					t.Fatalf("ReadIssue: %v", err)
				}
				if thread.Issue.Number != 42 || thread.Issue.Title != "issue" || thread.Issue.State != "open" {
					t.Errorf("Issue = %+v", thread.Issue)
				}
				if failComments {
					if thread.CommentsErr == nil || len(thread.Comments) != 0 {
						t.Errorf("CommentsErr = %v with %d comments, want an error and none", thread.CommentsErr, len(thread.Comments))
					}
					return
				}
				if thread.CommentsErr != nil || len(thread.Comments) != 2 {
					t.Fatalf("CommentsErr = %v with %d comments, want none and 2", thread.CommentsErr, len(thread.Comments))
				}
				if c := thread.Comments[0]; c.Body != "comment 1" || c.User.Login != "u" {
					t.Errorf("first comment = %+v", c)
				}
			})
		}
	}
}

// A ref without a repository is read from the repository the client was
// made for.
func TestClient_FallsBackToItsOwnRepository(t *testing.T) {
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				paths []string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.EscapedPath())
				mu.Unlock()
				switch {
				case r.Method == http.MethodPost:
					_, _ = w.Write([]byte(f.commentURLJSON))
				case strings.HasSuffix(r.URL.Path, f.commentsSuffix):
					_, _ = w.Write([]byte(`[]`))
				default:
					_, _ = fmt.Fprintf(w, f.issueJSON, 3)
				}
			}))
			defer srv.Close()
			c := f.client(t, Options{BaseURL: srv.URL, Token: "tok", Repo: Repo{Owner: "own", Name: "repo"}})
			ref := IssueRef{Number: 3}
			if _, err := c.ReadIssue(context.Background(), ref); err != nil {
				t.Fatal(err)
			}
			if _, err := c.AddComment(context.Background(), ref, "x"); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ListComments(context.Background(), ref); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, p := range paths {
				if !strings.Contains(p, "own/repo") && !strings.Contains(p, "own%2Frepo") {
					t.Errorf("request to %s does not name the client's repository", p)
				}
			}
		})
	}
}

// Posting needs a credential, and without one nothing is sent.
func TestClient_AddCommentRefusesWithoutAToken(t *testing.T) {
	clearForgeEnv(t)
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			tt := &scriptedTransport{}
			c := f.client(t, Options{BaseURL: "https://forge.invalid", HTTPClient: &http.Client{Transport: tt}})
			_, err := c.AddComment(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}, "x")
			if !errors.Is(err, ErrNoToken) || !IsNoToken(err) {
				t.Errorf("err = %v, want ErrNoToken", err)
			}
			if tt.RequestCount != 0 {
				t.Errorf("%d request(s) sent without a token", tt.RequestCount)
			}
		})
	}
}

func TestClient_AddCommentPostsTheBodyAndReturnsTheCommentURL(t *testing.T) {
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, f.commentsSuffix) {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["body"] != "the PRD" {
					t.Errorf("posted body = %v (%v), want the PRD", body, err)
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(f.commentURLJSON))
			}))
			defer srv.Close()
			got, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).AddComment(context.Background(),
				IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}, "the PRD")
			if err != nil {
				t.Fatal(err)
			}
			if got != f.commentURL {
				t.Errorf("AddComment = %q, want %q", got, f.commentURL)
			}
		})
	}
}

func TestClient_AddCommentReportsAFailedPost(t *testing.T) {
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message": "locked"}`))
			}))
			defer srv.Close()
			url, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).AddComment(context.Background(),
				IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}, "x")
			var he *HTTPError
			if url != "" || !errors.As(err, &he) || he.Status != http.StatusForbidden {
				t.Errorf("AddComment = %q, %v; want no URL and the 403", url, err)
			}
		})
	}
}

// Comments are read page by page up to 500. Truncated is set when the cap is
// hit and further comments exist on the server, not when the 500th comment is
// the last one.
func TestClient_ListCommentsStopsAt500AndSaysWhenMoreRemain(t *testing.T) {
	for _, f := range forges {
		full := func(n int) []string {
			pages := make([]string, n)
			for i := range pages {
				pages[i] = jsonItems(i*100+1, 100, f.commentJSON)
			}
			return pages
		}
		for _, tc := range []struct {
			name          string
			pages         []string
			wantComments  int
			wantTruncated bool
		}{
			{"exactly 500", full(5), 500, false},
			{"more than 500", append(full(5), jsonItems(501, 1, f.commentJSON)), 500, true},
			{"many more than 500", full(8), 500, true},
			{"under the cap across pages", []string{jsonItems(1, 100, f.commentJSON), jsonItems(101, 30, f.commentJSON)}, 130, false},
			{"none", []string{`[]`}, 0, false},
		} {
			t.Run(f.name+" "+tc.name, func(t *testing.T) {
				srv := pagedServer(t, tc.pages, f.announceNext)
				list, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).ListComments(context.Background(),
					IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 1})
				if err != nil {
					t.Fatalf("ListComments: %v", err)
				}
				if len(list.Comments) != tc.wantComments {
					t.Errorf("got %d comments, want %d", len(list.Comments), tc.wantComments)
				}
				if list.Truncated != tc.wantTruncated {
					t.Errorf("Truncated = %v, want %v", list.Truncated, tc.wantTruncated)
				}
				if tc.wantComments > 0 && list.Comments[tc.wantComments-1].Body != fmt.Sprintf("comment %d", tc.wantComments) {
					t.Errorf("last comment = %q, want the comments in order", list.Comments[tc.wantComments-1].Body)
				}
			})
		}
	}
}

func TestClient_ListCommentsReportsAFailedPage(t *testing.T) {
	for _, f := range forges {
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))
			defer srv.Close()
			list, err := f.client(t, Options{BaseURL: srv.URL, Token: "tok"}).ListComments(context.Background(),
				IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1})
			var he *HTTPError
			if !errors.As(err, &he) || he.Status != http.StatusForbidden || len(list.Comments) != 0 {
				t.Errorf("ListComments = %+v, %v; want no comments and the 403", list, err)
			}
		})
	}
}

// A short rate limit is waited out once with the sleep the caller set, then
// the request is sent again; a long one is not waited for.
func TestClient_WaitsOutAShortRateLimitWithItsSleep(t *testing.T) {
	for _, f := range forges {
		for _, tc := range []struct {
			name       string
			retryAfter string
			wantSleeps []time.Duration
			wantErr    bool
		}{
			{"short", "10", []time.Duration{11 * time.Second}, false},
			{"too long", "125", nil, true},
		} {
			t.Run(f.name+" "+tc.name, func(t *testing.T) {
				var (
					mu    sync.Mutex
					calls int
				)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					calls++
					first := calls == 1
					mu.Unlock()
					if strings.HasSuffix(r.URL.Path, f.commentsSuffix) {
						_, _ = w.Write([]byte(`[]`))
						return
					}
					if first {
						w.Header().Set("Retry-After", tc.retryAfter)
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					_, _ = fmt.Fprintf(w, f.issueJSON, 1)
				}))
				defer srv.Close()
				c := f.client(t, Options{BaseURL: srv.URL, Token: "tok"})
				var slept []time.Duration
				c.(interface{ SetSleep(func(time.Duration)) }).SetSleep(func(d time.Duration) { slept = append(slept, d) })

				_, err := c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1})
				if tc.wantErr != (err != nil) || (err != nil && !IsRateLimited(err)) {
					t.Errorf("err = %v, want a rate-limit error: %v", err, tc.wantErr)
				}
				if fmt.Sprint(slept) != fmt.Sprint(tc.wantSleeps) {
					t.Errorf("slept %v, want %v", slept, tc.wantSleeps)
				}
			})
		}
	}
}
