package issuex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type testHeaderRoundTripper struct {
	rt   http.RoundTripper
	name string
	val  string
}

func (h *testHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(h.name, h.val)
	return h.rt.RoundTrip(req)
}

// TestGitHub_Issue_Read_TS_02_9 verifies TS-02-9:
// ReadIssue fetches issue metadata, sets IsPR, and handles comment retrieval and partial failures.
// Verifies: 02-REQ-3.3, 02-REQ-3.4
func TestGitHub_Issue_Read_TS_02_9(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			if r.Header.Get("X-Fail-Comments") == "1" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write([]byte(`[{"body": "first comment", "user": {"login": "bob"}, "html_url": "https://cmt/1"}]`))
			return
		}
		w.Write([]byte(`{
			"number": 42,
			"title": "PR issue",
			"pull_request": {"url": "https://api.github.com/repos/o/r/pulls/42"}
		}`))
	}))
	defer srv.Close()

	c := newTestGitHubClient(Options{BaseURL: srv.URL, Token: "tok"})
	thread, err := c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 42})
	if err != nil {
		t.Fatalf("unexpected error on ReadIssue: %v", err)
	}
	if !thread.Issue.IsPR {
		t.Errorf("expected IsPR true, got false")
	}
	if len(thread.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(thread.Comments))
	}
	if thread.Comments[0].Body != "first comment" {
		t.Errorf("expected comment body 'first comment', got %s", thread.Comments[0].Body)
	}

	// Test partial comment failure resilience
	cFail := newTestGitHubClient(Options{
		BaseURL: srv.URL,
		Token:   "tok",
		HTTPClient: &http.Client{
			Transport: &testHeaderRoundTripper{rt: http.DefaultTransport, name: "X-Fail-Comments", val: "1"},
		},
	})
	threadFail, errFail := cFail.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 42})
	if errFail != nil {
		t.Fatalf("expected nil error on ReadIssue with comment failure, got: %v", errFail)
	}
	if threadFail.Issue.Number != 42 {
		t.Errorf("expected issue number 42, got %d", threadFail.Issue.Number)
	}
	if threadFail.CommentsErr == nil {
		t.Errorf("expected non-nil CommentsErr, got nil")
	}
}

// TestGitHub_Issue_ReadUnauthenticatedNotFound_TS_02_10 verifies TS-02-10:
// ReadIssue wraps ErrNotFound on 404 with private issue guidance for unauthenticated callers.
// Verifies: 02-REQ-3.5
func TestGitHub_Issue_ReadUnauthenticatedNotFound_TS_02_10(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	unauth := newTestGitHubClient(Options{BaseURL: srv.URL})
	_, err := unauth.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 999})
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound(err) == true, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "private") {
		t.Errorf("expected error to contain 'private', got %s", err.Error())
	}
}

// TestGitHub_Comment_Operations_TS_02_13 verifies TS-02-13:
// AddComment posts comment and returns URL, and ListComments paginates up to 500 cap with truncation flag.
// Verifies: 02-REQ-4.1, 02-REQ-4.2
func TestGitHub_Comment_Operations_TS_02_13(t *testing.T) {
	makeCommentPageJSON := func(n int) []byte {
		var items []map[string]any
		for i := 0; i < n; i++ {
			items = append(items, map[string]any{
				"id":   i + 1,
				"body": fmt.Sprintf("comment %d", i+1),
				"user": map[string]any{"login": "user"},
			})
		}
		b, _ := json.Marshal(items)
		return b
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["body"] != "test comment" {
				t.Errorf("expected body 'test comment', got %s", body["body"])
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"html_url": "https://github.com/o/r/issues/1#issuecomment-1"}`))
			return
		}
		// More than 500 comments exist: every page is full and GitHub
		// announces the next one.
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		githubNext(w, r, page+1)
		w.Write(makeCommentPageJSON(100))
	}))
	defer srv.Close()

	c := newTestGitHubClient(Options{BaseURL: srv.URL, Token: "tok"})
	ref := IssueRef{Repo: Repo{Owner: "o", Name: "r"}, Number: 1}

	url, err := c.AddComment(context.Background(), ref, "test comment")
	if err != nil {
		t.Fatalf("unexpected error on AddComment: %v", err)
	}
	if url != "https://github.com/o/r/issues/1#issuecomment-1" {
		t.Errorf("expected html_url 'https://github.com/o/r/issues/1#issuecomment-1', got %s", url)
	}

	cmts, errList := c.ListComments(context.Background(), ref)
	if errList != nil {
		t.Fatalf("unexpected error on ListComments: %v", errList)
	}
	if len(cmts.Comments) != 500 {
		t.Errorf("expected 500 comments, got %d", len(cmts.Comments))
	}
	if !cmts.Truncated {
		t.Errorf("expected Truncated true, got false")
	}
}
