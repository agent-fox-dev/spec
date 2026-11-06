package afspec

import "sort"

// CoverageReport lists the criteria whose check resolves and the paths that
// carry a smoke check (Covered), and those that do not (Uncovered), in
// artifact order.
//
// Format v3 §8.5: coverage is computed, never stored. On a package that
// passes its schema everything is covered, since a check and a smoke check
// are required; on one still being written the report shows the gaps.
type CoverageReport struct {
	Covered   []string
	Uncovered []string
}

// ComputeCoverage reports which criteria of req carry a check that resolves
// (its own, or the same as a criterion whose check is its own, or as a path)
// and which paths carry a smoke check.
func (req *RequirementsV3Json) ComputeCoverage() CoverageReport {
	own := map[string]bool{}
	paths := map[string]bool{}
	for _, r := range req.Requirements {
		for _, c := range r.Criteria {
			own[c.Id] = c.Check.SameAs == nil && c.Check.Kind != nil
		}
	}
	for _, p := range req.ExecutionPaths {
		paths[p.Id] = len(p.Smoke.RealComponents) > 0 && len(p.Smoke.EndState) > 0
	}

	covered := []string{}
	uncovered := []string{}
	appendID := func(id string, ok bool) {
		if ok {
			covered = append(covered, id)
		} else {
			uncovered = append(uncovered, id)
		}
	}

	for _, r := range req.Requirements {
		for _, c := range r.Criteria {
			ok := own[c.Id]
			if c.Check.SameAs != nil {
				ok = own[*c.Check.SameAs] || paths[*c.Check.SameAs]
			}
			appendID(c.Id, ok)
		}
	}
	for _, p := range req.ExecutionPaths {
		appendID(p.Id, paths[p.Id])
	}

	return CoverageReport{Covered: covered, Uncovered: uncovered}
}

// TraceLink is one row of the derived traceability matrix (§8.5):
//
//	criterion → check (its own, or the one it is the same as) → tasks (from task.criteria)
//	path      → smoke check                                   → integration task
//
// Kind is "criterion" or "path". Check names the check: the kind of an own
// check, "same as <id>" for a shared one, "smoke" for a path.
type TraceLink struct {
	Kind    string
	ID      string
	Check   string
	Tasks   []int
	Covered bool
	Owned   bool
}

// TraceabilityMatrix is the full derived matrix, in artifact order.
type TraceabilityMatrix struct {
	Links []TraceLink
}

// ComputeTraceability derives the traceability matrix from the artifacts.
// Nothing is read from disk and nothing is stored, so the matrix cannot drift
// from the artifacts it describes (§8.5).
//
// Covered is ComputeCoverage's answer for the entity. Owned reports whether a
// task lists the criterion or its requirement; a path is owned by the
// integration task when the package has one.
func (s *Spec) ComputeTraceability() TraceabilityMatrix {
	matrix := TraceabilityMatrix{Links: []TraceLink{}}
	if s.Requirements == nil {
		return matrix
	}

	coverage := s.Requirements.ComputeCoverage()
	covered := map[string]bool{}
	for _, id := range coverage.Covered {
		covered[id] = true
	}

	// criterion or requirement id → owning task ids, in task order.
	tasksFor := map[string][]int{}
	if s.Tasks != nil {
		for _, task := range s.Tasks.Tasks {
			for _, ref := range task.Criteria {
				tasksFor[ref] = append(tasksFor[ref], task.Id)
			}
		}
	}

	for _, r := range s.Requirements.Requirements {
		for _, c := range r.Criteria {
			tasks := append([]int{}, tasksFor[c.Id]...)
			tasks = append(tasks, tasksFor[r.Id]...)
			sort.Ints(tasks)
			var check string
			switch {
			case c.Check.SameAs != nil:
				check = "same as " + *c.Check.SameAs
			case c.Check.Kind != nil:
				check = string(*c.Check.Kind)
			}
			matrix.Links = append(matrix.Links, TraceLink{
				Kind: "criterion", ID: c.Id, Check: check, Tasks: dedupeInts(tasks), Covered: covered[c.Id], Owned: len(tasks) > 0,
			})
		}
	}
	var integration []int
	if id, ok := s.IntegrationTaskID(); ok {
		integration = []int{id}
	}
	for _, p := range s.Requirements.ExecutionPaths {
		matrix.Links = append(matrix.Links, TraceLink{
			Kind: "path", ID: p.Id, Check: "smoke", Tasks: integration, Covered: covered[p.Id], Owned: len(integration) > 0,
		})
	}

	return matrix
}

func dedupeInts(in []int) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
