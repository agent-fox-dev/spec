package afspec

import "slices"

// Owned lists what the task with the given id answers for with evidence, in
// the order of requirements.json: for a task of the plan, every criterion its
// criteria name, a requirement id standing for all of that requirement's
// criteria; for the integration task, every execution path. It is nil for an
// id the plan does not have.
func (s *Spec) Owned(id int) []string {
	task, ok := s.GetTask(id)
	if !ok || s.Requirements == nil {
		return nil
	}
	var out []string
	if task.Kind == TaskKindIntegration {
		for _, p := range s.Requirements.ExecutionPaths {
			out = append(out, p.Id)
		}
		return out
	}
	for _, r := range s.Requirements.Requirements {
		for _, c := range r.Criteria {
			if slices.Contains(task.Criteria, r.Id) || slices.Contains(task.Criteria, c.Id) {
				out = append(out, c.Id)
			}
		}
	}
	return out
}

// WithTaskEvidence is the progress of the package with the evidence of the
// task with the given id replaced by entries (none when entries is empty),
// and every other task as it is: a new artifact, so the receiver's progress
// is not touched. An id the plan does not have changes nothing.
func (s *Spec) WithTaskEvidence(id int, entries []EvidenceEntry) *ProgressV3Json {
	p := s.progressArtifact()
	for i := range p.Tasks {
		if p.Tasks[i].Id == id {
			p.Tasks[i].Evidence = cloneEvidence(entries)
		}
	}
	return p
}

// TaskEvidence is a copy of the evidence progress.json holds for the task
// with the given id, sharing nothing with the package: for each criterion the
// task owns (for the integration task, each execution path), the tests that
// cover it and the runs speca recorded of them. It is nil for a task with
// none.
func (s *Spec) TaskEvidence(id int) []EvidenceEntry {
	if s == nil || s.Progress == nil {
		return nil
	}
	for _, e := range s.Progress.Tasks {
		if e.Id == id {
			return cloneEvidence(e.Evidence)
		}
	}
	return nil
}

// cloneEvidence is a copy of entries that shares nothing with them, nil for
// none.
func cloneEvidence(entries []EvidenceEntry) []EvidenceEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]EvidenceEntry, len(entries))
	for i, e := range entries {
		out[i] = EvidenceEntry{Covers: e.Covers, Tests: make([]EvidenceTest, len(e.Tests))}
		if e.NoRed != nil {
			reason := *e.NoRed
			out[i].NoRed = &reason
		}
		for j, t := range e.Tests {
			out[i].Tests[j] = EvidenceTest{Test: t.Test, Failed: cloneRun(t.Failed), Passed: cloneRun(t.Passed)}
		}
	}
	return out
}

func cloneRun(r *EvidenceRun) *EvidenceRun {
	if r == nil {
		return nil
	}
	c := EvidenceRun{At: r.At}
	if r.Tree != nil {
		tree := *r.Tree
		c.Tree = &tree
	}
	return &c
}
