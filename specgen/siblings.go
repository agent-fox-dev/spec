package specgen

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/agent-fox-dev/spec/afspec"
)

// The packages of a split are built concurrently, and a package is judged by the
// rules across specs against the others of the spec root. Two things follow
// from that, and both are about the packages a package can depend on, which are
// the ones written before it, so the ones with a lower number.
//
// The package is checked once those have settled. One still being built is not
// on disk yet, and a dependency on it would be reported as a dependency on no
// spec.
//
// Its requirements step is shown their actors, which the rule is decided by.
// Their paths are written by their requirements step, so that step waits for
// theirs, and no further: the steps after it still overlap.
//
// A package waits only for packages with a lower number, which started before
// it, so no two wait for each other.

// sibling is one package of this run, for the packages after it to wait for and
// read.
type sibling struct {
	// name is the package's NN_name.
	name string

	// requirements is closed once its requirements step has ended, and settled
	// once its build has: written and validated, or failed.
	requirements, settled chan struct{}
	reqOnce, settleOnce   sync.Once

	mu sync.Mutex
	// actors are those of its execution paths, once its requirements step
	// has given them.
	actors []string
	// spec is the package, kept in a dry run, which leaves nothing on disk for
	// the packages after it to read.
	spec *afspec.Spec
}

func newSibling(name string) *sibling {
	return &sibling{name: name, requirements: make(chan struct{}), settled: make(chan struct{})}
}

// endRequirements records the actors of the package's paths, or none when the
// step failed, and releases the packages waiting for them. The methods of
// sibling are safe on nil, a package that is not tracked.
func (s *sibling) endRequirements(actors []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.actors = actors
	s.mu.Unlock()
	s.reqOnce.Do(func() { close(s.requirements) })
}

// settle ends the package's build for the packages waiting for it, whatever
// came of it.
func (s *sibling) settle() {
	if s == nil {
		return
	}
	s.reqOnce.Do(func() { close(s.requirements) })
	s.settleOnce.Do(func() { close(s.settled) })
}

// keep holds the package validated in a dry run.
func (s *sibling) keep(spec *afspec.Spec) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.spec = spec
	s.mu.Unlock()
}

func (s *sibling) actorsKnown() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actors
}

func (s *sibling) kept() *afspec.Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spec
}

// track registers the package whose number is reserved, before any of its phases
// run.
func (e *runEnv) track(specID, dirName string) {
	n, err := strconv.Atoi(specID)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.siblings == nil {
		e.siblings = map[int]*sibling{}
	}
	e.siblings[n] = newSibling(dirName)
}

// sibling is the tracked package with the number specID, or nil.
func (e *runEnv) sibling(specID string) *sibling {
	n, err := strconv.Atoi(specID)
	if err != nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.siblings[n]
}

// earlier are the tracked packages with a lower number than specID, in number
// order.
func (e *runEnv) earlier(specID string) []*sibling {
	n, err := strconv.Atoi(specID)
	if err != nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var numbers []int
	for k := range e.siblings {
		if k < n {
			numbers = append(numbers, k)
		}
	}
	sort.Ints(numbers)
	out := make([]*sibling, len(numbers))
	for i, k := range numbers {
		out[i] = e.siblings[k]
	}
	return out
}

// awaitRequirements returns once the requirements step of every earlier package
// has ended, or ctx is done.
func (e *runEnv) awaitRequirements(ctx context.Context, specID string) {
	for _, s := range e.earlier(specID) {
		await(ctx, s.requirements)
	}
}

// awaitSettled returns once every earlier package has settled, or ctx is done.
func (e *runEnv) awaitSettled(ctx context.Context, specID string) {
	for _, s := range e.earlier(specID) {
		await(ctx, s.settled)
	}
}

func await(ctx context.Context, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

// earlierSpecs are the earlier packages of a dry run, held in memory because
// nothing of them is on disk.
func (e *runEnv) earlierSpecs(specID string) []*afspec.Spec {
	var out []*afspec.Spec
	for _, s := range e.earlier(specID) {
		if spec := s.kept(); spec != nil {
			out = append(out, spec)
		}
	}
	return out
}

// actorsNow are the actors of the specs the package is written beside, for its
// requirements step: those on disk, then those of the earlier packages of this
// run that are not. It is called once the earlier packages' requirements steps
// have ended.
func (e *runEnv) actorsNow(specID string) []specActors {
	var out []specActors
	listed := map[string]bool{}
	for _, m := range e.landscapeNow() {
		name := m.SpecID + "_" + m.SpecName
		listed[name] = true
		s, err := afspec.LoadSpec(m.Dir)
		if err != nil || s.Requirements == nil {
			continue // a package that does not load has no paths to match
		}
		if actors := actorsOf(s.Requirements); len(actors) > 0 {
			out = append(out, specActors{Spec: name, Actors: actors})
		}
	}
	for _, s := range e.earlier(specID) {
		if actors := s.actorsKnown(); len(actors) > 0 && !listed[s.name] {
			out = append(out, specActors{Spec: s.name, Actors: actors})
		}
	}
	return out
}

// actorsOf are the actors of the execution paths, each once whatever its case
// (the rule ignores it), in alphabetical order.
func actorsOf(r *afspec.RequirementsV2Json) []string {
	if r == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range r.ExecutionPaths {
		for _, step := range p.Steps {
			actor := strings.TrimSpace(step.Actor)
			if key := strings.ToLower(actor); actor != "" && !seen[key] {
				seen[key] = true
				out = append(out, actor)
			}
		}
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}
