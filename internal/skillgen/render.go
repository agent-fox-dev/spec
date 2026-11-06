package main

import (
	"fmt"
	"regexp"
	"strconv"
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

// topLevelKey matches a frontmatter line that opens a key at the top level. A
// line it does not match continues the key before it, which is why the filter
// works on lines and not on a parsed mapping: the source's key order and the
// text of each value survive (see ADR 12).
var topLevelKey = regexp.MustCompile(`^([A-Za-z0-9_.-]+):`)

// invokeToken is the one token the render replaces, and braceToken is every
// brace token, so that one the render does not understand is reported rather
// than passed through to an agent that would show it to the user.
var (
	invokeToken = regexp.MustCompile(`\{\{invoke ([a-z0-9-]+)\}\}`)
	braceToken  = regexp.MustCompile(`\{\{[^{}]*\}\}`)
)

// argumentPlaceholder is Claude Code's own placeholder for a skill's argument.
// A skill that carries it works in Claude Code and nowhere else.
const argumentPlaceholder = "$ARGUMENTS"

// ownSyntax matches a skill named in an agent's own invocation syntax, but only
// where the name stands alone: the delimiters on both sides keep a path such as
// skills/af-impl, written in prose, from being reported (see ADR 12).
var ownSyntax = regexp.MustCompile("(?m)(^|[\\s`(\\[\"])([/$])([a-z0-9-]+)($|[\\s[:punct:]])")

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

// rendered is one skill as one agent receives it.
type rendered struct {
	doc  string            // the whole SKILL.md, frontmatter and body
	body string            // the part after the closing delimiter, which carries the ceiling
	refs map[string]string // the included reference files, by base name
}

// renderSkill renders one skill for one agent: the frontmatter filtered to the
// keys that agent reads, and the invocation token replaced. Nothing else in the
// body or in a reference file changes.
func renderSkill(sk *skill, a agent) rendered {
	out := rendered{
		body: a.substitute(sk.body),
		refs: make(map[string]string, len(sk.refs)),
	}
	out.doc = delimiter + "\n" + filterFrontmatter(sk.frontmatter, a.keys) + "\n" + delimiter + "\n" + out.body
	for name, r := range sk.refs {
		out.refs[name] = a.substitute(string(r.data))
	}
	return out
}

// substitute replaces every invocation token with this agent's syntax. It is
// the only text the render rewrites. The replacement is computed and not a
// template, because codex's prefix is "$", which a template would read as the
// start of a group reference.
func (a agent) substitute(text string) string {
	return invokeToken.ReplaceAllStringFunc(text, func(token string) string {
		return a.invoke + invokeToken.FindStringSubmatch(token)[1]
	})
}

// filterFrontmatter keeps only the keys given, in the order the source gives
// them, each with its own line and its continuation lines verbatim. A line that
// does not open a top-level key belongs to the key above it, so a value spread
// over several lines is kept or dropped whole.
func filterFrontmatter(frontmatter string, keys []string) string {
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[key] = true
	}
	var kept []string
	keeping := false
	for _, line := range strings.Split(frontmatter, "\n") {
		if match := topLevelKey.FindStringSubmatch(line); match != nil {
			keeping = wanted[match[1]]
		}
		if keeping {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// checkTokens reports the brace tokens and the agent-specific text one source
// file must not carry. skills is the set of names an invocation may name.
func checkTokens(path, text string, skills map[string]bool) []problem {
	var problems []problem
	for _, token := range braceToken.FindAllString(text, -1) {
		match := invokeToken.FindStringSubmatch(token)
		switch {
		case match == nil:
			problems = append(problems, problem{path, fmt.Sprintf(
				"holds %s; the only token the render replaces is {{invoke <name>}}", token)})
		case !skills[match[1]]:
			problems = append(problems, problem{path, fmt.Sprintf(
				"holds %s, and %s is no skill under the source", token, match[1])})
		}
	}
	if strings.Contains(text, argumentPlaceholder) {
		problems = append(problems, problem{path, fmt.Sprintf(
			"holds %s; name the argument in words, which every agent reads the same, "+
				"and another skill as {{invoke <name>}}", argumentPlaceholder)})
	}
	for _, match := range ownSyntax.FindAllStringSubmatch(text, -1) {
		name := match[3]
		if !skills[name] {
			continue
		}
		problems = append(problems, problem{path, fmt.Sprintf(
			"holds %s%s, one agent's own invocation syntax; write it as {{invoke %s}}",
			match[2], name, name)})
	}
	return dedupe(problems)
}

// dedupe drops the repeats a file with the same token twice would produce, so
// one mistake is one line for the author to fix.
func dedupe(problems []problem) []problem {
	seen := map[problem]bool{}
	kept := problems[:0]
	for _, p := range problems {
		if seen[p] {
			continue
		}
		seen[p] = true
		kept = append(kept, p)
	}
	return kept
}

// bodyLimit is the most bytes a rendered body may carry. The body loads with
// the skill at every step of a run, while a reference file loads only at the
// step that links it, so the ceiling is on the body alone and is measured in
// bytes, because bytes are what load.
const bodyLimit = 24576

// checkAgents reports a rendered body over the ceiling, one line per agent it
// is over for. Every agent's invocation prefix is one byte today, so the four
// sizes coincide; the measure stays per agent because the criterion is, and an
// agent with a longer prefix would need it.
func (s *source) checkAgents() []problem {
	var problems []problem
	for _, sk := range s.skills {
		for _, a := range agents {
			size := len(renderSkill(sk, a).body)
			if size <= bodyLimit {
				continue
			}
			problems = append(problems, problem{sk.path, fmt.Sprintf(
				"body is %s bytes for %s, %s over the %s-byte limit",
				commas(size), a.name, commas(size-bodyLimit), commas(bodyLimit))})
		}
	}
	return problems
}

// commas writes n with a comma every three digits, the way every number in a
// message the render writes is spelled.
func commas(n int) string {
	digits := strconv.Itoa(n)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}
