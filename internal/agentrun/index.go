package agentrun

import (
	"github.com/agent-fox-dev/agentkit-go/codesearch"
	"github.com/agent-fox-dev/agentkit-go/tools"
)

// NewIndex builds the run's code-search index. Creating it does not read the
// tree: AgentKit indexes it on the first code_search call. It is a variable
// so a test can stand in for codesearch.New.
var NewIndex = func(ws *tools.Workspace) (tools.Index, error) {
	return codesearch.New(ws, codesearch.Options{})
}

// OpenIndex builds the run's one code-search index and returns it with the
// function that closes it. When it cannot be built it returns a nil index,
// why, and a no-op closer: navigation never fails a run, and without an index
// the phases search with search_files.
func OpenIndex(ws *tools.Workspace) (idx tools.Index, unavailable string, closeFn func()) {
	idx, err := NewIndex(ws)
	switch {
	case err != nil:
		return nil, err.Error(), func() {}
	case idx == nil:
		return nil, "the index builder returned no index", func() {}
	}
	return idx, "", func() { _ = idx.Close() }
}
