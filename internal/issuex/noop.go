package issuex

import "context"

// Ensure NoOpClient implements Client at compile time.
var _ Client = (*NoOpClient)(nil)

// NoOpClient is an offline, forge-disabled client. It reads nothing and writes
// nothing: a workspace with no forge gets it.
type NoOpClient struct{}

// NewNoOp creates a new NoOpClient satisfying the Client interface.
func NewNoOp() Client {
	return &NoOpClient{}
}

// Authenticated returns false for NoOpClient.
func (c *NoOpClient) Authenticated() bool { return false }

// Close returns nil for NoOpClient.
func (c *NoOpClient) Close() error { return nil }

// ReadIssue returns a zero-value IssueThread and nil.
func (c *NoOpClient) ReadIssue(ctx context.Context, ref IssueRef) (IssueThread, error) {
	return IssueThread{}, nil
}

// AddComment performs no remote operation and returns an empty URL and nil.
func (c *NoOpClient) AddComment(ctx context.Context, ref IssueRef, body string) (string, error) {
	return "", nil
}

// ListComments returns a zero-value CommentList and nil.
func (c *NoOpClient) ListComments(ctx context.Context, ref IssueRef) (CommentList, error) {
	return CommentList{}, nil
}
