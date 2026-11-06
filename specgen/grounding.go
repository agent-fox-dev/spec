package specgen

import (
	"fmt"
	"os"
	"strings"

	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
)

// groundingFact is whether one path a task's touches names exists in the
// repository, as a file or as a directory.
type groundingFact struct {
	Path   string
	Exists bool
}

// groundingFacts says, for every distinct path in every task's touches, in
// task order and then touches order, whether it exists. The answer is the
// file system's, not a model's (ADR 17): a path is resolved inside the
// repository and looked at, so one that resolves outside it does not exist.
func groundingFacts(ws *workspace.Workspace, tasks *afspec.TasksV2Json) []groundingFact {
	facts := []groundingFact{}
	if tasks == nil {
		return facts
	}
	seen := map[string]bool{}
	for _, task := range tasks.Tasks {
		for _, path := range task.Touches {
			if seen[path] {
				continue
			}
			seen[path] = true
			exists := false
			if ws != nil {
				if abs, err := ws.Resolve(path); err == nil {
					_, err = os.Stat(abs)
					exists = err == nil
				}
			}
			facts = append(facts, groundingFact{Path: path, Exists: exists})
		}
	}
	return facts
}

// groundingBlock hands the grounding facts to the plan's two phases.
func groundingBlock(facts []groundingFact) string {
	var b strings.Builder
	b.WriteString("\n## Grounding facts\n\n" +
		"Computed from the file system before the review, not judged: for every path a task's `touches` names, " +
		"whether it exists in the repository now. A path that does not exist is not a flaw when a step of the " +
		"same task, or of a task it depends on, creates it.\n\n")
	if len(facts) == 0 {
		b.WriteString("The plan's tasks name no path.\n")
		return b.String()
	}
	for _, f := range facts {
		state := "does not exist"
		if f.Exists {
			state = "exists"
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", f.Path, state)
	}
	return b.String()
}
