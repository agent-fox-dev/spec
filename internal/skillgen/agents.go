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
	// userDir is this agent's user skill directory, relative to the home
	// directory, and projectDir its directory inside a repository.
	userDir    string
	projectDir string
	// homeEnv names an environment variable that, when set, replaces the home
	// directory with its value plus "skills". Only codex has one.
	homeEnv string
	// alsoUser and alsoProject are the other directories this agent also loads,
	// so an install can warn that it will see a skill twice. They come from the
	// vendors' own documentation, as ADR 12 records.
	alsoUser    []string
	alsoProject []string
}

// agents is the table the render walks, in this order, so the output of a run
// does not depend on map iteration.
var agents = []agent{{
	name:       "claude",
	keys:       []string{"name", "description", "argument-hint", "disable-model-invocation"},
	invoke:     "/",
	userDir:    ".claude/skills",
	projectDir: ".claude/skills",
}, {
	name:       "codex",
	keys:       []string{"name", "description"},
	invoke:     "$",
	userDir:    ".codex/skills",
	projectDir: ".agents/skills",
	homeEnv:    "CODEX_HOME",
}, {
	name:        "cursor",
	keys:        []string{"name", "description", "disable-model-invocation"},
	invoke:      "/",
	userDir:     ".cursor/skills",
	projectDir:  ".cursor/skills",
	alsoProject: []string{".agents/skills", ".claude/skills", ".codex/skills"},
}, {
	name:        "copilot",
	keys:        []string{"name", "description"},
	invoke:      "/",
	userDir:     ".copilot/skills",
	projectDir:  ".github/skills",
	alsoUser:    []string{".claude/skills", ".agents/skills"},
	alsoProject: []string{".claude/skills", ".agents/skills"},
}}

// agentNames lists the agents for the message that names the ones it accepts.
func agentNames() []string {
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.name)
	}
	return names
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
