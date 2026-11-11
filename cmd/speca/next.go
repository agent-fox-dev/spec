package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// nextSpec is the form of speca --next: the package to implement now.
//
// It reads every package under the spec root and orders them by the
// dependencies their tasks.json declare, ties broken by number, so that a
// package comes after every package it depends on. The package to implement
// is the first one in that order that is open (draft or active, with a task
// pending or in progress) and whose every dependency is finished. A package
// under archive/ is history: a dependency on one is finished. A cycle among the
// packages fails the call, since no order can satisfy it.
func nextSpec(c *call) (*Result, *cli.ErrorInfo) {
	dirs, failure := c.packageDirs("considered")
	if failure != nil {
		return nil, failure
	}
	nodes := make([]*specNode, len(dirs))
	byID := map[string][]int{}
	for i, d := range dirs {
		n := &specNode{dir: specpkg.Display(d.path, c.root)}
		spec, err := afspec.LoadSpec(d.path)
		if err != nil {
			n.loadError = err.Error()
		} else {
			n.spec = spec
			n.deps = dependsOn(spec)
			byID[spec.SpecID] = append(byID[spec.SpecID], i)
		}
		nodes[i] = n
	}

	g := specGraph{nodes: nodes, byID: byID}
	order, cycle := g.order()
	if cycle != nil {
		return nil, g.cycleFailure(cycle)
	}

	g.archived = map[string]bool{}
	for _, d := range c.archiveDirs("read, and a dependency on one of them is not finished") {
		if meta, err := afspec.LoadSpec(d.path); err == nil {
			g.archived[meta.SpecID] = true
		}
	}

	r := &Result{nextForm: true}
	for _, i := range order {
		if n := nodes[i]; n.open() && len(g.unmet(n)) == 0 {
			info := packageInfo(n.dir, n.spec, false)
			r.NextSpec = &info
			break
		}
	}
	r.Waiting = []envelope.WaitingInfo{}
	for i := range nodes {
		if w, ok := g.waiting(i); ok {
			r.Waiting = append(r.Waiting, w)
		}
	}
	allDone := r.NextSpec == nil && len(r.Waiting) == 0
	r.AllDone = &allDone
	return r, nil
}

// specNode is one package under the spec root, as --next sees it.
type specNode struct {
	dir       string
	spec      *afspec.Spec // nil when the package did not load
	loadError string
	deps      []string // the spec ids its tasks.json depends on
}

// terminal reports whether the package's status ends its work: sealed,
// superseded or archived.
func (n *specNode) terminal() bool {
	switch n.spec.Status {
	case string(afspec.PrdFrontmatterV3JsonStatusSealed),
		string(afspec.PrdFrontmatterV3JsonStatusSuperseded),
		string(afspec.PrdFrontmatterV3JsonStatusArchived):
		return true
	}
	return false
}

// finished reports whether nothing is left to implement in the package: its
// status ends its work, or it is in this format version and none of its tasks
// is pending or in progress. A package that did not load, or whose tasks speca
// cannot read, is not known to be finished.
func (n *specNode) finished() bool {
	if n.spec == nil {
		return false
	}
	if n.terminal() {
		return true
	}
	return !specpkg.Legacy(n.spec) && len(openTasks(n.spec)) == 0
}

// open reports whether the package is one to implement: it loaded in this
// format version, is a draft or active, and has a task pending or in progress.
func (n *specNode) open() bool {
	return n.spec != nil && !n.terminal() && !specpkg.Legacy(n.spec) && len(openTasks(n.spec)) > 0
}

// specGraph is the packages under the spec root and the dependencies between
// them, with the spec ids of the packages under archive/.
type specGraph struct {
	nodes    []*specNode
	byID     map[string][]int // the nodes that have a spec id; two when the id is taken twice
	archived map[string]bool  // the spec ids under archive/
}

// edges are the nodes node i depends on, in the order its dependencies name
// them. A dependency no package under the spec root has is no edge.
func (g *specGraph) edges(i int) []int {
	var out []int
	for _, id := range g.nodes[i].deps {
		out = append(out, g.byID[id]...)
	}
	return out
}

// order is the nodes in dependency order: each after every node it depends on,
// and of the nodes that could come next, the one first in number order. When
// the dependencies form a cycle no order exists, and it returns one cycle
// instead, each node in it depending on the next and the last on the first.
func (g *specGraph) order() (order, cycle []int) {
	waits := make([]int, len(g.nodes))
	dependents := make([][]int, len(g.nodes))
	for i := range g.nodes {
		for _, j := range g.edges(i) {
			waits[i]++
			dependents[j] = append(dependents[j], i)
		}
	}
	placed := make([]bool, len(g.nodes))
	for len(order) < len(g.nodes) {
		next := -1
		for i := range g.nodes {
			if !placed[i] && waits[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			return nil, g.cycleAmong(placed)
		}
		placed[next] = true
		order = append(order, next)
		for _, k := range dependents[next] {
			waits[k]--
		}
	}
	return order, nil
}

// cycleAmong finds a cycle among the nodes not placed. Each of them waits on a
// node that is not placed either, so following the first such dependency from
// any of them comes back to a node already visited.
func (g *specGraph) cycleAmong(placed []bool) []int {
	at := slices.Index(placed, false)
	seen := map[int]int{}
	var path []int
	for {
		if p, ok := seen[at]; ok {
			return path[p:]
		}
		seen[at] = len(path)
		path = append(path, at)
		for _, j := range g.edges(at) {
			if !placed[j] {
				at = j
				break
			}
		}
	}
}

// cycleFailure is the failure of a call whose packages depend on one another
// in a cycle, naming each package of it in turn.
func (g *specGraph) cycleFailure(cycle []int) *cli.ErrorInfo {
	names := make([]string, 0, len(cycle)+1)
	for _, i := range append(cycle, cycle[0]) {
		names = append(names, g.nodes[i].dir)
	}
	return &cli.ErrorInfo{Stage: "graph", Category: "input",
		Message: fmt.Sprintf("the packages under the spec root depend on one another in a cycle, each on the one after it: %s; "+
			"none of them can be implemented before the others, so no next package is chosen. "+
			"Remove one of these dependencies from the tasks.json that declares it",
			strings.Join(names, " → "))}
}

// unmet is the spec ids node n depends on that are not finished: a package under
// the spec root that has the id and is not finished, or no package under the
// spec root or archive/ with that id at all.
func (g *specGraph) unmet(n *specNode) []string {
	var out []string
	for _, id := range n.deps {
		if !g.met(id) {
			out = append(out, id)
		}
	}
	return out
}

// met reports whether the dependency on id is finished: every package under
// the spec root with that id is, or none has it and one under archive/ does.
func (g *specGraph) met(id string) bool {
	holders := g.byID[id]
	if len(holders) == 0 {
		return g.archived[id]
	}
	for _, j := range holders {
		if !g.nodes[j].finished() {
			return false
		}
	}
	return true
}

// waiting is the entry of node i in the waiting list, if it has one: it did not
// load, it is in another format version and is not finished, or it is open and
// waits on a dependency.
func (g *specGraph) waiting(i int) (envelope.WaitingInfo, bool) {
	n := g.nodes[i]
	w := envelope.WaitingInfo{SpecDir: n.dir}
	switch {
	case n.spec == nil:
		w.Reason = "it does not load, so whether anything is left to implement in it is not known: " + n.loadError
		return w, true
	case n.finished():
		return w, false
	}
	w.SpecID = n.spec.SpecID
	if specpkg.Legacy(n.spec) {
		w.Reason = fmt.Sprintf("it is format version %d, whose tasks speca does not read; speca %s --migrate converts a version 2 package",
			n.spec.SchemaVersion, n.dir)
		return w, true
	}
	w.WaitsOn = g.unmet(n)
	if len(w.WaitsOn) == 0 {
		return w, false
	}
	why := make([]string, len(w.WaitsOn))
	for k, id := range w.WaitsOn {
		if len(g.byID[id]) == 0 {
			why[k] = id + " is no package under the spec root or archive/"
		} else {
			why[k] = id + " is not finished"
		}
	}
	w.Reason = "it depends on packages that are not finished: " + strings.Join(why, "; ")
	return w, true
}

// nextSummary says what --next found: the package to implement, or that
// everything is done, or that nothing can start.
func (r *Result) nextSummary() string {
	switch {
	case r.NextSpec != nil:
		s := fmt.Sprintf("%s: next is %s", tool, r.NextSpec.SpecDir)
		if n := len(r.Waiting); n > 0 {
			s += fmt.Sprintf("; %d more waiting", n)
		}
		return s
	case r.AllDone != nil && *r.AllDone:
		return tool + ": every package is done; nothing is left to implement"
	default:
		return fmt.Sprintf("%s: no package can start; %d waiting", tool, len(r.Waiting))
	}
}

// nextNext suggests the report on the package to implement, which names the
// task to start with.
func (r *Result) nextNext() []cli.Next {
	if r.NextSpec == nil {
		return nil
	}
	return []cli.Next{{Tool: tool, Input: r.NextSpec.SpecDir, Flags: []string{},
		Why: fmt.Sprintf("%s is the next package to implement; its report names the task to start with", r.NextSpec.SpecDir)}}
}
