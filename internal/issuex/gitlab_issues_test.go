package issuex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ReadIssue reads GitLab's issue fields, with "opened" read as "open", and
// leaves GitLab's system notes out of the comments.
func TestGitLabReadIssue_MapsTheIssueAndSkipsSystemNotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/org%2Frepo/issues/5":
			_, _ = w.Write([]byte(`{
				"iid": 5, "title": "Title", "description": "Desc", "state": "opened", "labels": ["bug"],
				"author": {"username": "bob"}, "web_url": "https://gitlab.com/org/repo/-/issues/5"
			}`))
		case "/api/v4/projects/org%2Frepo/issues/5/notes":
			_, _ = w.Write([]byte(`[
				{"id": 1, "body": "Comment 1", "system": false, "author": {"username": "carol"}, "web_url": "https://gitlab.com/org/repo/-/issues/5#note_1"},
				{"id": 2, "body": "changed the title", "system": true, "author": {"username": "gitlab-bot"}}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewGitLab(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 5})
	if err != nil {
		t.Fatal(err)
	}
	is := thread.Issue
	if is.Number != 5 || is.Title != "Title" || is.Body != "Desc" || is.State != "open" || is.Author.Login != "bob" ||
		is.URL != "https://gitlab.com/org/repo/-/issues/5" || is.IsPR || strings.Join(is.Labels, ",") != "bug" {
		t.Errorf("Issue = %+v", is)
	}
	if len(thread.Comments) != 1 {
		t.Fatalf("Comments = %+v, want the one user note", thread.Comments)
	}
	if cm := thread.Comments[0]; cm.Body != "Comment 1" || cm.User.Login != "carol" || cm.HTMLURL != "https://gitlab.com/org/repo/-/issues/5#note_1" {
		t.Errorf("comment = %+v", cm)
	}
}

// GitLab numbers issues and merge requests separately, so a merge-request
// ref is read, commented on and listed at the merge_requests endpoints.
func TestGitLab_AMergeRequestRefUsesTheMergeRequestEndpoints(t *testing.T) {
	var (
		mu   sync.Mutex
		uris []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uris = append(uris, r.Method+" "+r.URL.EscapedPath())
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id": 3}`))
		case strings.HasSuffix(r.URL.Path, "/notes"):
			if q := r.URL.Query(); q.Get("sort") != "asc" || q.Get("order_by") != "created_at" || q.Get("per_page") != "100" {
				t.Errorf("notes requested with %s, want oldest first, 100 to a page", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"iid": 5, "title": "MR", "state": "merged"}`))
		}
	}))
	defer srv.Close()

	c, err := NewGitLab(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ref := IssueRef{Repo: Repo{Owner: "g", Name: "p"}, Number: 5, IsPullRequest: true}
	thread, err := c.ReadIssue(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !thread.Issue.IsPR || thread.Issue.State != "merged" {
		t.Errorf("Issue = %+v, want the merged merge request", thread.Issue)
	}
	url, err := c.AddComment(context.Background(), ref, "x")
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/g/p/-/merge_requests/5#note_3"; url != want {
		t.Errorf("AddComment = %q, want %q", url, want)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"GET /api/v4/projects/g%2Fp/merge_requests/5",
		"GET /api/v4/projects/g%2Fp/merge_requests/5/notes",
		"POST /api/v4/projects/g%2Fp/merge_requests/5/notes",
	}
	if strings.Join(uris, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(uris, "\n"), strings.Join(want, "\n"))
	}
}

// An unauthenticated 404 names the variable that would let the client see a
// private project.
func TestGitLabReadIssue_Unauthenticated404NamesGITLAB_TOKEN(t *testing.T) {
	clearForgeEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "404 Not Found"}`))
	}))
	defer srv.Close()
	c, err := NewGitLab(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadIssue(context.Background(), IssueRef{Repo: Repo{Owner: "org", Name: "repo"}, Number: 99})
	if !IsNotFound(err) || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Errorf("err = %v, want ErrNotFound naming GITLAB_TOKEN", err)
	}
}

// A note as a real GitLab sends it carries no web_url, so its URL is built as
// {issue web URL}#note_{id} on the web host, not the /api/v4 host.
func TestGitLab_BuildsTheNoteURLWhenGitLabSendsNone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string // path the instance is served under, before /api/v4
		repo   Repo
		path   string // expected project path in the web URL
	}{
		{"root", "", Repo{Owner: "org", Name: "repo"}, "org/repo"},
		{"subgroup", "", Repo{Owner: "group/sub", Name: "proj"}, "group/sub/proj"},
		{"relative URL root", "/gitlab", Repo{Owner: "org", Name: "repo"}, "org/repo"},
	} {
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
				t.Fatal(err)
			}
			ref := IssueRef{Repo: tc.repo, Number: 1}
			want := srv.URL + tc.prefix + "/" + tc.path + "/-/issues/1#note_77"

			got, err := c.AddComment(context.Background(), ref, "a note")
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("AddComment URL = %q, want %q", got, want)
			}
			list, err := c.ListComments(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			if len(list.Comments) != 1 || list.Comments[0].HTMLURL != want {
				t.Errorf("ListComments = %+v, want one comment at %q", list.Comments, want)
			}
		})
	}
}
