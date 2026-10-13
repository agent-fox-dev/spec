package issuex_test

import (
	"context"
	"testing"

	"github.com/agent-fox-dev/spec/internal/issuex"
)

// The no-op client a workspace with no forge gets reads nothing, writes
// nothing and cannot write.
func TestNoOpClientReadsAndWritesNothing(t *testing.T) {
	c := issuex.NewNoOp()
	if c.Authenticated() {
		t.Error("the no-op client must not report a credential")
	}
	ref := issuex.IssueRef{Repo: issuex.Repo{Owner: "o", Name: "r"}, Number: 1}
	thread, err := c.ReadIssue(context.Background(), ref)
	if err != nil || thread.Issue.Number != 0 || len(thread.Comments) != 0 {
		t.Errorf("ReadIssue = %+v, %v", thread, err)
	}
	if url, err := c.AddComment(context.Background(), ref, "body"); err != nil || url != "" {
		t.Errorf("AddComment = %q, %v", url, err)
	}
	if list, err := c.ListComments(context.Background(), ref); err != nil || len(list.Comments) != 0 {
		t.Errorf("ListComments = %+v, %v", list, err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}
