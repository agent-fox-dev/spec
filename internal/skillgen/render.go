package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// nameLimit and descriptionLimit are the Agent Skills bounds a skill's
// frontmatter must stay inside. Every agent loads a skill by its directory and
// offers it by its description, so breaking one breaks the skill in the agent
// rather than in the render.
const (
	nameLimit        = 64
	descriptionLimit = 1024
)

// nameRule is the Agent Skills name: lowercase letters, digits and single
// hyphens, with neither the first nor the last character a hyphen. The length
// is checked separately so the message can say which rule was broken.
var nameRule = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// nameRuleText is how a message describes the rule, so an author reads what to
// fix rather than a regular expression.
const nameRuleText = "1 to 64 characters of lowercase letters, digits and single hyphens, " +
	"with neither the first nor the last character a hyphen"

// meta is the part of a frontmatter the render reads. Every other key is
// carried through or dropped by the agent's key list, never interpreted.
type meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// checkMeta reports a name or a description that breaks the Agent Skills rule.
func checkMeta(sk *skill) []problem {
	var problems []problem
	switch name := sk.meta.Name; {
	case name == "":
		problems = append(problems, problem{sk.path, "has no name in its frontmatter"})
	case name != sk.name:
		problems = append(problems, problem{sk.path, fmt.Sprintf(
			"name %q is not the name of its directory, %q", name, sk.name)})
	case utf8.RuneCountInString(name) > nameLimit || !nameRule.MatchString(name):
		problems = append(problems, problem{sk.path, fmt.Sprintf(
			"name %q is not %s", name, nameRuleText)})
	}
	switch description := sk.meta.Description; {
	case description == "":
		problems = append(problems, problem{sk.path, "has no description in its frontmatter"})
	case strings.TrimSpace(description) == "":
		problems = append(problems, problem{sk.path, "has an empty description"})
	case utf8.RuneCountInString(description) > descriptionLimit:
		problems = append(problems, problem{sk.path, fmt.Sprintf(
			"description is %d characters, over the 1,024-character limit",
			utf8.RuneCountInString(description))})
	}
	return problems
}
