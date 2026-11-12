package afspec

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// NextPackage is the package to implement now: its directory, as the caller's
// display function shows it, and the package as it loaded.
type NextPackage struct {
	Dir  string
	Spec *Spec
}

// WaitingPackage is a package under the spec root that is open but cannot
// start yet, or that cannot be judged: it does not load, or is in another
// format version.
type WaitingPackage struct {
	// Dir is the package directory, as the display function shows it.
	Dir string
	// SpecID is empty when the package did not load.
	SpecID string
	// WaitsOn is the spec ids of its dependencies that are not finished, in
	// the order its tasks.json names them; nil when it waits on nothing but
	// itself.
	WaitsOn []string
	// Reason says why, in words.
	Reason string
}

// NextChoice is what ChooseNext found under a spec root.
type NextChoice struct {
	// Next is the package to implement now; nil when there is none.
	Next *NextPackage
	// AllDone is true when nothing is left to implement: no package is open
	// and every one of them loads in this format version.
	AllDone bool
	// Waiting is the packages that are open and cannot start, or cannot be
	// judged, in number order; empty when there are none.
	Waiting []WaitingPackage
	// ArchiveErr is why archive/ could not be read, when it exists and could
	// not; a dependency on a package there is then not finished.
	ArchiveErr error
}

// CycleError says the packages under the spec root depend on one another in
// a cycle, so that no order can satisfy them.
type CycleError struct {
	// Dirs is the packages of the cycle in turn, each depending on the next,
	// the first repeated at the end, as the display function shows them.
	Dirs []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("the packages under the spec root depend on one another in a cycle, each on the one after it: %s; "+
		"none of them can be implemented before the others, so no next package is chosen. "+
		"Remove one of these dependencies from the tasks.json that declares it", strings.Join(e.Dirs, " → "))
}

// SpecRootError says the spec root could not be read: an empty choice would
// hide a mistyped root.
type SpecRootError struct {
	Err error
}

func (e *SpecRootError) Error() string { return e.Err.Error() }
func (e *SpecRootError) Unwrap() error { return e.Err }

// ChooseNext chooses the package to implement now among the packages under
// specsDir. They are ordered by the dependencies their tasks.json declare,
// ties broken by number, so that a package comes after every package it
// depends on; the next is the first in that order that is open (draft or
// active, with a task pending or in progress) and whose every dependency is
// finished (sealed, superseded or archived, with no task open, or under
// archive/). display shows a package directory in the reasons and the
// result; nil shows it as it is. A cycle among the packages is a *CycleError,
// and a spec root that cannot be read a *SpecRootError.
func ChooseNext(specsDir string, display func(string) string) (NextChoice, error) {
	if display == nil {
		display = func(p string) string { return p }
	}
	names, err := ListSpecDirs(specsDir)
	if err != nil {
		return NextChoice{}, &SpecRootError{Err: err}
	}
	g := nextGraph{byID: map[string][]int{}, archived: map[string]bool{}}
	for i, name := range names {
		path := filepath.Join(specsDir, name)
		n := &nextNode{dir: display(path)}
		if spec, err := LoadSpec(path); err != nil {
			n.loadError = err.Error()
		} else {
			n.spec = spec
			n.deps = dependencyIDs(spec)
			g.byID[spec.SpecID] = append(g.byID[spec.SpecID], i)
		}
		g.nodes = append(g.nodes, n)
	}
	order, cycle := g.order()
	if cycle != nil {
		dirs := make([]string, 0, len(cycle)+1)
		for _, i := range append(cycle, cycle[0]) {
			dirs = append(dirs, g.nodes[i].dir)
		}
		return NextChoice{}, &CycleError{Dirs: dirs}
	}

	var c NextChoice
	archive := filepath.Join(specsDir, "archive")
	archived, err := ListSpecDirs(archive)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		c.ArchiveErr = err
	default:
		for _, name := range archived {
			if spec, err := LoadSpec(filepath.Join(archive, name)); err == nil {
				g.archived[spec.SpecID] = true
			}
		}
	}

	for _, i := range order {
		if n := g.nodes[i]; n.open() && len(g.unmet(n)) == 0 {
			c.Next = &NextPackage{Dir: n.dir, Spec: n.spec}
			break
		}
	}
	c.Waiting = []WaitingPackage{}
	for i := range g.nodes {
		if w, ok := g.waiting(i); ok {
			c.Waiting = append(c.Waiting, w)
		}
	}
	c.AllDone = c.Next == nil && len(c.Waiting) == 0
	return c, nil
}

// ListSpecDirs returns the names of the NN_name directories under root, in
// the order of their numbers: the prefix compared as a number, so that 99_y
// comes before 100_x, and a tie broken by the whole name. It lists
// directories whether or not they hold a package that loads.
func ListSpecDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && IsSpecDirName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	slices.SortFunc(out, compareSpecDirs)
	return out, nil
}

// compareSpecDirs orders two NN_name directory names by their numbers. The
// numbers are compared as digit strings without their leading zeros, shorter
// first, so that a number too long for an int still orders.
func compareSpecDirs(a, b string) int {
	na, _, _ := strings.Cut(a, "_")
	nb, _, _ := strings.Cut(b, "_")
	na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
	if c := cmp.Compare(len(na), len(nb)); c != 0 {
		return c
	}
	if c := strings.Compare(na, nb); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// IsLegacy reports whether the package is in a format version other than the
// one this library reads. Such a package loads, but the v3 types read none of
// its progress and little of its plan. MigrateV2 converts a version 2 package.
func (s *Spec) IsLegacy() bool {
	return s.SchemaVersion != 0 && s.SchemaVersion != SchemaVersion
}

// OpenTasks are the tasks of the plan that are pending or in progress, the
// derived integration task included, in plan order.
func (s *Spec) OpenTasks() []PlannedTask {
	var out []PlannedTask
	for _, t := range s.Plan() {
		if t.State == TaskStatePending || t.State == TaskStateInProgress {
			out = append(out, t)
		}
	}
	return out
}

// dependencyIDs is the spec ids the package's tasks.json depends on, in file
// order, each once.
func dependencyIDs(s *Spec) []string {
	var out []string
	if s.Tasks == nil {
		return out
	}
	for _, d := range s.Tasks.Dependencies {
		if !slices.Contains(out, d.Spec) {
			out = append(out, d.Spec)
		}
	}
	return out
}

// nextNode is one package under the spec root, as ChooseNext sees it.
type nextNode struct {
	dir       string
	spec      *Spec // nil when the package did not load
	loadError string
	deps      []string
}

// terminal reports whether the package's status ends its work.
func (n *nextNode) terminal() bool {
	switch n.spec.Status {
	case string(PrdFrontmatterV3JsonStatusSealed), string(PrdFrontmatterV3JsonStatusSuperseded),
		string(PrdFrontmatterV3JsonStatusArchived):
		return true
	}
	return false
}

// finished reports whether nothing is left to implement in the package. A
// package that did not load, or whose tasks this version cannot read, is not
// known to be finished.
func (n *nextNode) finished() bool {
	if n.spec == nil {
		return false
	}
	return n.terminal() || !n.spec.IsLegacy() && len(n.spec.OpenTasks()) == 0
}

// open reports whether the package is one to implement.
func (n *nextNode) open() bool {
	return n.spec != nil && !n.terminal() && !n.spec.IsLegacy() && len(n.spec.OpenTasks()) > 0
}

// nextGraph is the packages under the spec root and the dependencies between
// them, with the spec ids of the packages under archive/.
type nextGraph struct {
	nodes    []*nextNode
	byID     map[string][]int
	archived map[string]bool
}

// edges are the nodes node i depends on; a dependency no package under the
// spec root has is no edge.
func (g *nextGraph) edges(i int) []int {
	var out []int
	for _, id := range g.nodes[i].deps {
		out = append(out, g.byID[id]...)
	}
	return out
}

// order is the nodes in dependency order, each after every node it depends on
// and, of the nodes that could come next, the first in number order; or, when
// no order exists, one cycle.
func (g *nextGraph) order() (order, cycle []int) {
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

// cycleAmong finds a cycle among the nodes not placed: each waits on one that
// is not placed either, so following those dependencies comes back to a node
// already visited.
func (g *nextGraph) cycleAmong(placed []bool) []int {
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

// unmet is the spec ids node n depends on that are not finished.
func (g *nextGraph) unmet(n *nextNode) []string {
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
func (g *nextGraph) met(id string) bool {
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

// waiting is the entry of node i in the waiting list, if it has one.
func (g *nextGraph) waiting(i int) (WaitingPackage, bool) {
	n := g.nodes[i]
	w := WaitingPackage{Dir: n.dir}
	switch {
	case n.spec == nil:
		w.Reason = "it does not load, so whether anything is left to implement in it is not known: " + n.loadError
		return w, true
	case n.finished():
		return w, false
	}
	w.SpecID = n.spec.SpecID
	if n.spec.IsLegacy() {
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
