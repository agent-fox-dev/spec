package agentrun

import (
	"errors"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
)

// Navigation never fails a run: an index that cannot be built leaves none, the
// reason, and a closer that does nothing; one that can is closed by the closer.
func TestOpenIndex(t *testing.T) {
	built := &indextest.Index{}
	for _, tc := range []struct {
		name   string
		build  func(*workspace.Workspace) (tools.Index, error)
		want   tools.Index
		reason string
	}{
		{"built", func(*workspace.Workspace) (tools.Index, error) { return built, nil }, built, ""},
		{"failed", func(*workspace.Workspace) (tools.Index, error) { return nil, errors.New("no disk") }, nil, "no disk"},
		{"nothing built", func(*workspace.Workspace) (tools.Index, error) { return nil, nil }, nil, "the index builder returned no index"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := NewIndex
			t.Cleanup(func() { NewIndex = saved })
			NewIndex = tc.build

			idx, reason, closeFn := OpenIndex(nil)
			if idx != tc.want || reason != tc.reason {
				t.Errorf("OpenIndex = %v, %q; want %v, %q", idx, reason, tc.want, tc.reason)
			}
			closeFn()
		})
	}
	if built.CloseCalls() != 1 {
		t.Errorf("the built index was closed %d times, want once", built.CloseCalls())
	}
}
