package issuex

import (
	"fmt"
	"strings"
	"time"
)

// Repo names a Git repository across GitHub or GitLab.
// For GitLab, Owner may contain nested group segments (e.g. "group/subgroup").
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Host  string `json:"host,omitempty"`
}

// String returns "Owner/Name".
func (r Repo) String() string {
	return fmt.Sprintf("%s/%s", r.Owner, r.Name)
}

// Valid reports whether both Owner and Name are non-empty.
func (r Repo) Valid() bool {
	return r.Owner != "" && r.Name != ""
}

// IssueRef identifies an issue or pull request in a repository by number.
type IssueRef struct {
	Repo          Repo `json:"repo"`
	Number        int  `json:"number"`
	IsPullRequest bool `json:"is_pull_request"`
}

// String returns "owner/repo#number".
func (r IssueRef) String() string {
	return fmt.Sprintf("%s#%d", r.Repo.String(), r.Number)
}

// URL returns the canonical web URL for the issue or pull request.
func (r IssueRef) URL() string {
	host := strings.TrimSuffix(strings.TrimSpace(r.Repo.Host), "/")
	if host == "" {
		host = "github.com"
	}
	scheme := "https"
	if strings.HasPrefix(host, "http://") {
		scheme = "http"
		host = strings.TrimPrefix(host, "http://")
	} else if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}

	isGitLab := strings.Contains(strings.ToLower(host), "gitlab")
	if isGitLab {
		kind := "issues"
		if r.IsPullRequest {
			kind = "merge_requests"
		}
		return fmt.Sprintf("%s://%s/%s/-/%s/%d", scheme, host, r.Repo.String(), kind, r.Number)
	}

	kind := "issues"
	if r.IsPullRequest {
		kind = "pull"
	}
	return fmt.Sprintf("%s://%s/%s/%s/%d", scheme, host, r.Repo.String(), kind, r.Number)
}

// User represents an account on the forge.
type User struct {
	Login string `json:"login"`
}

// Comment represents an issue comment.
type Comment struct {
	ID        int64     `json:"id,omitempty"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	URL       string    `json:"url,omitempty"`
	HTMLURL   string    `json:"html_url,omitempty"`
}

// Issue represents an issue or pull request metadata.
type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	URL       string     `json:"url,omitempty"`
	HTMLURL   string     `json:"html_url,omitempty"`
	Author    User       `json:"author"`
	Labels    []string   `json:"labels,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	IsPR      bool       `json:"is_pr"`
}

// IssueThread represents an issue and its comments.
type IssueThread struct {
	Issue       Issue     `json:"issue"`
	Comments    []Comment `json:"comments"`
	Truncated   bool      `json:"truncated"`
	CommentsErr error     `json:"comments_err,omitempty"`
}

// CommentList contains comments returned from an issue.
type CommentList struct {
	Comments  []Comment `json:"comments"`
	Truncated bool      `json:"truncated"`
}
