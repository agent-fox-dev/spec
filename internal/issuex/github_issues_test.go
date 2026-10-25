package issuex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ReadIssue reads GitHub's issue fields; an issue that carries a
// pull_request object is a pull request.
func TestGitHubReadIssue_MapsTheIssue(t *testing.T) {
	var (
		mu   sync.Mutex
		urls []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.URL.RequestURI())
		mu.Unlock()
		switch r.URL.Path {
		case "/repos/o/r/issues/42/comments", "/repos/o/r/issues/7/comments":
			_, _ = w.Write([]byte(`[{"id": 9, "body": "looks good", "user": {"login": "bob"}, "html_url": "https://github.com/o/r/pull/42#issuecomment-9"}]`))
		case "/repos/o/r/issues/42":
			_, _ = w.Write([]byte(`{
				"number": 42, "title": "Add X", "body": "Please", "state": "closed",
				"html_url": "https://github.com/o/r/pull/42", "user": {"login": "alice"},
				"labels": [{"name": "bug"}, {"name": ""}, {"name": "p1"}],
				"created_at": "2026-01-02T03:04:05Z", "closed_at": "2026-01-03T00:00:00Z",
				"pull_request": {"url": "https://api.github.com/repos/o/r/pulls/42"}
			}`))
		case "/repos/o/r/issues/7":
			_, _ = w.Write([]byte(`{"number": 7, "title": "An issue", "state": "open", "labels": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client, err := NewGitHub(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	thread, err := client.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 42})
	if err != nil {
		t.Fatal(err)
	}
	is := thread.Issue
	if is.Number != 42 || is.Title != "Add X" || is.Body != "Please" || is.State != "closed" || is.Author.Login != "alice" ||
		is.URL != "https://github.com/o/r/pull/42" || is.HTMLURL != is.URL || !is.IsPR ||
		strings.Join(is.Labels, ",") != "bug,p1" || !is.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || is.ClosedAt == nil {
		t.Errorf("Issue = %+v", is)
	}
	if len(thread.Comments) != 1 || thread.Comments[0].User.Login != "bob" || thread.Comments[0].HTMLURL != "https://github.com/o/r/pull/42#issuecomment-9" {
		t.Errorf("Comments = %+v", thread.Comments)
	}

	plain, err := client.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Issue.IsPR || plain.Issue.Labels != nil {
		t.Errorf("a plain issue read as %+v", plain.Issue)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(urls) < 2 || urls[1] != "/repos/o/r/issues/42/comments?per_page=100&page=1" {
		t.Errorf("requests = %v, want the comments read 100 to a page", urls)
	}
}
