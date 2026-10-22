package specpkg

import (
	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
)

// ReportOf flattens afspec's verdict for JSON: the counts, and every error and
// warning with the rule that produced it.
func ReportOf(v afspec.ValidationResult) envelope.ValidationReport {
	out := envelope.ValidationReport{
		Valid:        v.Valid,
		ErrorCount:   len(v.Errors),
		WarningCount: len(v.Warnings),
	}
	for _, e := range v.Errors {
		out.Errors = append(out.Errors, itemOf(e))
	}
	for _, w := range v.Warnings {
		out.Warnings = append(out.Warnings, itemOf(w))
	}
	return out
}

func itemOf(e afspec.ValidationEntry) envelope.ValidationItem {
	return envelope.ValidationItem{
		Check:    e.Check,
		Artifact: e.Artifact,
		Entity:   e.EntityID,
		Message:  e.Message,
	}
}

// TraceOf derives the coverage summary.
//
// It is computed rather than read, because format v2 makes coverage and
// traceability derived: there is no coverage object in the artifacts to
// report, and the numbers here are the only place they exist. The matrix
// carries one link per criterion AND one per execution path, distinguished by
// Kind, which is why this walks it once rather than asking for coverage twice.
func TraceOf(s *afspec.Spec) envelope.TraceReport {
	var out envelope.TraceReport
	for _, link := range s.ComputeTraceability().Links {
		switch link.Kind {
		case "path":
			if link.Covered {
				out.PathsCovered++
			} else {
				out.PathsUncovered = append(out.PathsUncovered, link.ID)
			}
		default:
			if link.Covered {
				out.CriteriaCovered++
			} else {
				out.CriteriaUncovered = append(out.CriteriaUncovered, link.ID)
			}
		}
	}
	out.TestsUnowned = unownedTests(s)
	return out
}

// unownedTests are the tests no task lists. Rule C7 already reports them as
// errors; repeating them here is what lets a caller act on the gap without
// parsing validation messages.
func unownedTests(s *afspec.Spec) []string {
	if s.TestSpec == nil || s.Tasks == nil {
		return nil
	}
	owned := map[string]bool{}
	for _, t := range s.Tasks.Tasks {
		for _, id := range t.Tests {
			owned[id] = true
		}
	}
	var out []string
	for _, t := range s.TestSpec.Tests {
		if !owned[t.Id] {
			out = append(out, t.Id)
		}
	}
	return out
}
