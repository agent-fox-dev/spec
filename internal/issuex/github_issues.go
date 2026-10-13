package issuex

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type ghIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	User    User   `json:"user"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	PullRequest *struct {
		URL     string `json:"url,omitempty"`
		HTMLURL string `json:"html_url,omitempty"`
	} `json:"pull_request,omitempty"`
}

func (g ghIssue) toIssue() Issue {
	var labelNames []string
	if len(g.Labels) > 0 {
		labelNames = make([]string, 0, len(g.Labels))
		for _, l := range g.Labels {
			if l.Name != "" {
				labelNames = append(labelNames, l.Name)
			}
		}
	}
	return Issue{
		Number:    g.Number,
		Title:     g.Title,
		Body:      g.Body,
		State:     g.State,
		URL:       g.HTMLURL,
		HTMLURL:   g.HTMLURL,
		Author:    g.User,
		Labels:    labelNames,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
		ClosedAt:  g.ClosedAt,
		IsPR:      g.PullRequest != nil,
	}
}

// ReadIssue retrieves an issue and its comments by issue number.
func (c *githubClient) ReadIssue(ctx context.Context, ref IssueRef) (IssueThread, error) {
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}
	var raw ghIssue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", ref.Repo.Owner, ref.Repo.Name, ref.Number)
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return IssueThread{}, err
	}
	issue := raw.toIssue()
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

// AddComment posts a new comment to an issue or pull request.
func (c *githubClient) AddComment(ctx context.Context, ref IssueRef, body string) (string, error) {
	if !c.Authenticated() {
		return "", ErrNoToken
	}
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}
	payload := map[string]any{"body": body}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", ref.Repo.Owner, ref.Repo.Name, ref.Number)
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := c.do(ctx, http.MethodPost, path, payload, &out); err != nil {
		return "", err
	}
	return out.HTMLURL, nil
}

// ListComments retrieves chronological comments on an issue up to 500 total comments.
func (c *githubClient) ListComments(ctx context.Context, ref IssueRef) (CommentList, error) {
	if !ref.Repo.Valid() && c.repo.Valid() {
		ref.Repo = c.repo
	}
	var (
		all       []Comment
		truncated bool
	)
	for page := 1; page <= 5; page++ {
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", ref.Repo.Owner, ref.Repo.Name, ref.Number, page)
		var batch []Comment
		resp, err := c.doWithResponse(ctx, http.MethodGet, path, nil, &batch)
		if err != nil {
			return CommentList{}, err
		}
		all = append(all, batch...)
		if !hasNextPage(resp) {
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
