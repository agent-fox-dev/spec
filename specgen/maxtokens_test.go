package specgen

import (
	"context"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/afspec"
)

// Thinking is spent from the same max_tokens as the reply, and a phase's reply
// is one tool call carrying the whole document. At the efforts the API names
// (xhigh and max) the guidance is a cap of at least 64K. A run on the ADVANCED
// tier, whose PRD phase is on xhigh, spent a whole 32768 on one turn thinking,
// never reached submit_prd and failed with "no result".
func TestEveryPhaseLeavesRoomToThinkAtTheHighestEfforts(t *testing.T) {
	const floor = 64000
	ws := newWorkspace(t)
	ctx := context.Background()

	phases := map[string]func(*agentAuthor){
		"prd": func(a *agentAuthor) {
			_, _, _ = a.WritePRD(ctx, prdRequest{Root: ws.Root()})
		},
		"generation": func(a *agentAuthor) {
			_, _, _ = a.GenerateArtifact(ctx, artifactRequest{
				Step: afspec.StepRequirements, Root: ws.Root(), Partial: &afspec.PartialSpec{},
			})
		},
		"architecture": func(a *agentAuthor) {
			_, _, _ = a.WriteArchitecture(ctx, architectureRequest{Partial: &afspec.PartialSpec{}})
		},
	}
	for name, run := range phases {
		t.Run(name, func(t *testing.T) {
			p := faux.New()
			run(&agentAuthor{runner: indexedRunner(t, ws, p, nil), ws: ws})

			reqs := p.Requests()
			if len(reqs) == 0 {
				t.Fatal("the phase sent no request")
			}
			if got := reqs[0].MaxTokens; got == nil || *got < floor {
				t.Errorf("max_tokens = %v, want at least %d", deref(got), floor)
			}
		})
	}
}

func deref(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}
