package main

// agent is one coding agent the render targets. Each loads a SKILL.md from a
// directory, reads its own frontmatter keys and invokes another skill in its
// own syntax; that invocation is the only text that differs between them.
type agent struct {
	name string
	// keys are the frontmatter keys this agent reads. A key it does not read is
	// dropped, so an installed skill carries nothing it would ignore.
	keys []string
	// invoke is the prefix an invocation of another skill takes.
	invoke string
}

// agents is the table the render walks, in this order, so the output of a run
// does not depend on map iteration.
var agents = []agent{
	{"claude", []string{"name", "description", "argument-hint", "disable-model-invocation"}, "/"},
	{"codex", []string{"name", "description"}, "$"},
	{"cursor", []string{"name", "description", "disable-model-invocation"}, "/"},
	{"copilot", []string{"name", "description"}, "/"},
}

// lookupAgent finds an agent by name. found is false for a name outside the
// table, which the install refuses rather than guesses at.
func lookupAgent(name string) (agent, bool) {
	for _, a := range agents {
		if a.name == name {
			return a, true
		}
	}
	return agent{}, false
}
