package issuex

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type gitlabIssue struct {
	ID          int    `json:"id"`
	IID         int    `json:"iid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	WebURL      string `json:"web_url"`
	Author      struct {
		Username string `json:"username"`
	} `json:"author"`
	Labels    []string   `json:"labels"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

func (g gitlabIssue) toIssue() Issue {
	st := g.State
	if st == "opened" {
		st = "open"
	}
	return Issue{
		Number:    g.IID,
		Title:     g.Title,
		Body:      g.Description,
		State:     st,
		URL:       g.WebURL,
		HTMLURL:   g.WebURL,
		Author:    User{Login: g.Author.Username},
		Labels:    g.Labels,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
		ClosedAt:  g.ClosedAt,
		IsPR:      false,
	}
}

type gitlabNote struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	System    bool      `json:"system"`
	WebURL    string    `json:"web_url"`
	CreatedAt time.Time `json:"created_at"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
}

// ReadIssue reads an issue and its comments by issue iid.
func (c *gitlabClient) ReadIssue(ctx context.Context, ref IssueRef) (IssueThread, error) {
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}

	path := fmt.Sprintf("/projects/%s/%s/%d", projectPath(ref.Repo), threadKind(ref), ref.Number)
	var raw gitlabIssue
	if _, err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		if IsNotFound(err) && !c.Authenticated() {
			return IssueThread{}, fmt.Errorf("%w: issue or project may be private and require setting GITLAB_TOKEN: %w", ErrNotFound, err)
		}
		return IssueThread{}, err
	}

	issue := raw.toIssue()
	issue.IsPR = issue.IsPR || ref.IsPullRequest
	cmts, cErr := c.ListComments(ctx, ref)
	if cErr != nil {
		return IssueThread{
			Issue:       issue,
			CommentsErr: cErr,
		}, nil
	}
	return IssueThread{
		Issue:     issue,
		Comments:  cmts.Comments,
		Truncated: cmts.Truncated,
	}, nil
}

// AddComment posts a new note (comment) to an issue.
func (c *gitlabClient) AddComment(ctx context.Context, ref IssueRef, body string) (string, error) {
	if !c.Authenticated() {
		return "", ErrNoToken
	}
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}

	payload := map[string]any{"body": body}
	path := fmt.Sprintf("/projects/%s/%s/%d/notes", projectPath(ref.Repo), threadKind(ref), ref.Number)
	var note gitlabNote
	if _, err := c.do(ctx, http.MethodPost, path, payload, &note); err != nil {
		return "", err
	}

	if note.WebURL != "" {
		return note.WebURL, nil
	}
	return c.noteURL(ref, note.ID), nil
}

// noteURL builds {issue web URL}#note_{id} for a note. GitLab's Notes API
// returns no web_url, so this is the URL a real instance gets. The web host is
// the API base URL without its /api/v4 suffix, which NewGitLab guarantees.
func (c *gitlabClient) noteURL(ref IssueRef, id int) string {
	webURL := strings.TrimSuffix(c.baseURL, "/api/v4")
	return fmt.Sprintf("%s/%s/-/%s/%d#note_%d", webURL, ref.Repo.String(), threadKind(ref), ref.Number, id)
}

// threadKind is the API collection a thread lives in. GitLab numbers issues
// and merge requests separately, so issue 5 and merge request 5 are two
// threads, and a merge-request ref sent to the issue endpoint names the
// wrong one.
func threadKind(ref IssueRef) string {
	if ref.IsPullRequest {
		return "merge_requests"
	}
	return "issues"
}

// ListComments retrieves chronological comments on an issue up to 500 total notes, filtering out system notes.
func (c *gitlabClient) ListComments(ctx context.Context, ref IssueRef) (CommentList, error) {
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}

	var (
		all       []Comment
		truncated bool
	)

	for page := 1; page <= 5; page++ {
		path := fmt.Sprintf("/projects/%s/%s/%d/notes?per_page=100&page=%d&sort=asc&order_by=created_at",
			projectPath(ref.Repo), threadKind(ref), ref.Number, page)
		var batch []gitlabNote
		resp, _, err := c.doWithResponse(ctx, http.MethodGet, path, nil, &batch)
		if err != nil {
			return CommentList{}, err
		}
		for _, n := range batch {
			if n.System {
				continue
			}
			htmlURL := n.WebURL
			if htmlURL == "" {
				htmlURL = c.noteURL(ref, n.ID)
			}
			all = append(all, Comment{
				Body:      n.Body,
				User:      User{Login: n.Author.Username},
				CreatedAt: n.CreatedAt,
				HTMLURL:   htmlURL,
			})
		}
		if resp == nil || resp.Header.Get("X-Next-Page") == "" {
			break
		}
		if page == 5 {
			truncated = true
		}
	}

	return CommentList{
		Comments:  all,
		Truncated: truncated,
	}, nil
}
