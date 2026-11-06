package main

// unlinked reports a reference file that nothing links. It is a warning and not
// a problem: the file is simply not rendered, which is worth a word to its
// author and not a stopped build.
func (s *source) unlinked() []problem {
	linked := map[string]bool{}
	for _, sk := range s.skills {
		for _, r := range sk.refs {
			linked[r.from] = true
		}
	}
	var warnings []problem
	for _, sk := range s.skills {
		warnings = append(warnings, notLinked(sk.own, linked)...)
	}
	return append(warnings, notLinked(s.shared, linked)...)
}

// notLinked warns about every file of one reference pool that no skill reached,
// in name order so a run reports the same lines every time.
func notLinked(pool map[string]*ref, linked map[string]bool) []problem {
	var warnings []problem
	for _, name := range sortedNames(pool) {
		if r := pool[name]; !linked[r.from] {
			warnings = append(warnings, problem{r.from, "linked by nothing; not rendered"})
		}
	}
	return warnings
}
