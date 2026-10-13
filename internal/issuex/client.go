package issuex

import "context"

// Client is the forge-neutral client the spec CLI reads its input through and
// posts the finished PRD back with. It covers GitHub and GitLab issues, pull
// requests and merge requests.
type Client interface {
	// Authenticated reports whether the client holds a token and so can write.
	Authenticated() bool
	// Close releases the client's resources.
	Close() error

	// ReadIssue reads an issue, pull request or merge request and its comments.
	ReadIssue(ctx context.Context, ref IssueRef) (IssueThread, error)
	// AddComment posts a comment on the thread and returns its web URL.
	AddComment(ctx context.Context, ref IssueRef, body string) (string, error)
	// ListComments reads the thread's comments, oldest first.
	ListComments(ctx context.Context, ref IssueRef) (CommentList, error)
}
