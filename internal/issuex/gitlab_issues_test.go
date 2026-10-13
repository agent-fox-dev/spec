package issuex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TS-03-11: ReadIssue fetches issue details and chronologically appends discussion comments
func TestGitLab_ReadIssue_TS_03_11(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/v4/projects/org%2Frepo/issues/5" {
			w.Write([]byte(`{
				"iid": 5, "title": "Title", "description": "Desc", "state": "opened",
				"author": {"username": "bob"}, "web_url": "https://gitlab.com/org/repo/-/issues/5"
			}`))
			return
		}
		if r.URL.EscapedPath() == "/api/v4/projects/org%2Frepo/issues/5/notes" {
			w.Write([]byte(`[
				{"id": 1, "body": "Comment 1", "system": false, "author": {"username": "carol"}},
				{"id": 2, "body": "system note", "system": true, "author": {"username": "gitlab-bot"}}
			]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, err := NewGitLab(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatalf("NewGitLab failed: %v", err)
	}
	thread, err := c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 5})
	if err != nil {
		t.Fatalf("ReadIssue failed: %v", err)
	}
	if thread.Issue.Number != 5 {
		t.Errorf("expected Number 5, got %d", thread.Issue.Number)
	}
	if thread.Issue.State != "open" {
		t.Errorf("expected State 'open', got %s", thread.Issue.State)
	}
	if thread.Issue.IsPR != false {
		t.Errorf("expected IsPR false, got %v", thread.Issue.IsPR)
	}
	if len(thread.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(thread.Comments))
	}
	if thread.Comments[0].Body != "Comment 1" {
		t.Errorf("expected comment body 'Comment 1', got %s", thread.Comments[0].Body)
	}
}

// TS-03-12: ReadIssue handles comment fetch failure gracefully and annotates unauthenticated 404 errors
func TestGitLab_ReadIssue_CommentsErrAndUnauth404_TS_03_12(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/v4/projects/org%2Frepo/issues/7" {
			w.Write([]byte(`{"iid": 7, "title": "Issue 7", "state": "opened", "author": {"username": "u"}}`))
			return
		}
		if r.URL.EscapedPath() == "/api/v4/projects/org%2Frepo/issues/7/notes" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "notes service unavailable"}`))
			return
		}
		if r.URL.EscapedPath() == "/api/v4/projects/org%2Frepo/issues/99" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "404 Not Found"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cAuth, err := NewGitLab(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatalf("NewGitLab failed: %v", err)
	}
	thread, err := cAuth.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 7})
	if err != nil {
		t.Fatalf("expected nil error from ReadIssue, got %v", err)
	}
	if thread.Issue.Number != 7 {
		t.Errorf("expected Issue.Number 7, got %d", thread.Issue.Number)
	}
	if thread.CommentsErr == nil {
		t.Errorf("expected non-nil CommentsErr, got nil")
	}

	cUnauth, err := NewGitLab(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewGitLab failed: %v", err)
	}
	_, errNotFound := cUnauth.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 99})
	if !IsNotFound(errNotFound) {
		t.Errorf("expected IsNotFound(err) == true, got %v", errNotFound)
	}
	if !strings.Contains(errNotFound.Error(), "GITLAB_TOKEN") {
		t.Errorf("expected error message to mention GITLAB_TOKEN, got %v", errNotFound)
	}
}

// TS-03-15: AddComment posts a note and ListComments paginates user notes up to 500 cap filtering system notes
func TestGitLab_Comments_TS_03_15(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/notes") {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id": 42, "body": "a note", "web_url": "https://gitlab.com/note/42"}`))
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/notes") {
			page := r.URL.Query().Get("page")
			if page == "1" {
				w.Header().Set("X-Next-Page", "2")
				w.Write([]byte(`[{"id": 1, "body": "user note", "system": false, "author": {"username": "u"}}, {"id": 2, "body": "audit", "system": true, "author": {"username": "bot"}}]`))
			} else {
				w.Write([]byte(`[]`))
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, err := NewGitLab(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatalf("NewGitLab failed: %v", err)
	}
	ref := IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 1}
	url, err := c.AddComment(context.Background(), ref, "a note")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if url != "https://gitlab.com/note/42" {
		t.Errorf("expected url https://gitlab.com/note/42, got %s", url)
	}

	comments, errList := c.ListComments(context.Background(), ref)
	if errList != nil {
		t.Fatalf("ListComments failed: %v", errList)
	}
	if len(comments.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments.Comments))
	}
	if comments.Comments[0].Body != "user note" {
		t.Errorf("expected comment 'user note', got %s", comments.Comments[0].Body)
	}
	if comments.Truncated {
		t.Errorf("expected Truncated false, got true")
	}
}

// TestGitLab_NoteURLFallback verifies 03-REQ-4.1 for the payload a real GitLab
// sends: a note carries no web_url, so the URL is built as
// {issue web URL}#note_{id} on the web host, not the /api/v4 host.
func TestGitLab_NoteURLFallback(t *testing.T) {
	cases := []struct {
		name   string
		prefix string // path the instance is served under, before /api/v4
		repo   Repo
		path   string // expected project path in the web URL
	}{
		{"root", "", Repo{Owner: "org", Name: "repo"}, "org/repo"},
		{"subgroup", "", Repo{Owner: "group/sub", Name: "proj"}, "group/sub/proj"},
		{"relative URL root", "/gitlab", Repo{Owner: "org", Name: "repo"}, "org/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/notes"):
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id": 77, "body": "a note", "author": {"username": "u"}}`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/notes"):
					_, _ = w.Write([]byte(`[{"id": 77, "body": "a note", "system": false, "author": {"username": "u"}}]`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			c, err := NewGitLab(Options{BaseURL: srv.URL + tc.prefix + "/api/v4", Token: "tok"})
			if err != nil {
				t.Fatalf("NewGitLab failed: %v", err)
			}
			ref := IssueRef{Repo: tc.repo, Number: 1}
			want := srv.URL + tc.prefix + "/" + tc.path + "/-/issues/1#note_77"

			got, err := c.AddComment(context.Background(), ref, "a note")
			if err != nil {
				t.Fatalf("AddComment failed: %v", err)
			}
			if got != want {
				t.Errorf("AddComment URL = %q, want %q", got, want)
			}

			list, err := c.ListComments(context.Background(), ref)
			if err != nil {
				t.Fatalf("ListComments failed: %v", err)
			}
			if len(list.Comments) != 1 {
				t.Fatalf("expected 1 comment, got %d", len(list.Comments))
			}
			if list.Comments[0].HTMLURL != want {
				t.Errorf("ListComments HTMLURL = %q, want %q", list.Comments[0].HTMLURL, want)
			}
		})
	}
}
