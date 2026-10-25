package afspec

import (
	"strings"
	"testing"
)

func TestBuildDependencyGraph_LinksADependentToItsUpstream(t *testing.T) {
	root := specRoot(t, map[string]string{
		"01_test_feature":  fixtureValidSpec,
		"02_draft_feature": fixtureDraft,
	})

	metas, err := DiscoverSpecs(root)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := BuildDependencyGraph(metas, root)
	if err != nil {
		t.Fatalf("BuildDependencyGraph = %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("edges = %v; want the single 02 → 01 dependency", graph.Edges)
	}
	edge := graph.Edges[0]
	if edge.FromSpec != "02" || edge.ToSpec != "01" {
		t.Errorf("edge = %+v; want 02 depending on 01", edge)
	}
	if edge.Reason == "" {
		t.Error("the edge carries no reason")
	}

	if deps := graph.Dependencies("02"); len(deps) != 1 {
		t.Errorf("Dependencies(02) = %v; want one", deps)
	}
	if dependents := graph.Dependents("01"); len(dependents) != 1 {
		t.Errorf("Dependents(01) = %v; want one", dependents)
	}

	order, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort = %v", err)
	}
	if len(order) != 2 || order[0] != "01" || order[1] != "02" {
		t.Errorf("order = %v; want the upstream spec first", order)
	}
}

func TestBuildDependencyGraph_ReportsAnUnknownSpecAndKeepsThePartialGraph(t *testing.T) {
	root := specRoot(t, map[string]string{"02_draft_feature": fixtureDraft})
	metas, err := DiscoverSpecs(root)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := BuildDependencyGraph(metas, root)
	if err == nil {
		t.Fatal("a dependency on a spec that is not in the root produced no error")
	}
	if !strings.Contains(err.Error(), "01") {
		t.Errorf("the error does not name the missing spec: %v", err)
	}
	if graph == nil {
		t.Error("a partial graph should still be returned")
	}
}

func TestTopologicalSort_DetectsACycle(t *testing.T) {
	graph := &DependencyGraph{Edges: []DependencyEdge{
		{FromSpec: "01", ToSpec: "02"},
		{FromSpec: "02", ToSpec: "01"},
	}}
	if _, err := graph.TopologicalSort(); err == nil {
		t.Fatal("a two-spec cycle was not detected")
	}
}
