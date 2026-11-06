package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// metaTree builds a one-skill tree whose frontmatter is the text given, so a
// test varies only the keys it is about.
func metaTree(dir, frontmatter string) map[string]string {
	return map[string]string{dir + "/SKILL.md": "---\n" + frontmatter + "\n---\n\n# body\n"}
}

func TestReadSource_RefusesANameThatBreaksTheAgentSkillsRule(t *testing.T) {
	for name, tc := range map[string]struct {
		dir, frontmatter string
		wantProblem      bool
	}{
		"missing":            {"af-spec", "description: a sentence about it.", true},
		"differs from dir":   {"af-spec", "name: af-impl\ndescription: a sentence about it.", true},
		"uppercase":          {"Af-Spec", "name: Af-Spec\ndescription: a sentence about it.", true},
		"double hyphen":      {"af--spec", "name: af--spec\ndescription: a sentence about it.", true},
		"leading hyphen":     {"-af-spec", "name: -af-spec\ndescription: a sentence about it.", true},
		"trailing hyphen":    {"af-spec-", "name: af-spec-\ndescription: a sentence about it.", true},
		"underscore":         {"af_spec", "name: af_spec\ndescription: a sentence about it.", true},
		"65 characters":      {strings.Repeat("a", 65), "name: " + strings.Repeat("a", 65) + "\ndescription: a sentence about it.", true},
		"one character":      {"a", "name: a\ndescription: a sentence about it.", false},
		"64 characters":      {strings.Repeat("a", 64), "name: " + strings.Repeat("a", 64) + "\ndescription: a sentence about it.", false},
		"digits and hyphens": {"af-spec-2", "name: af-spec-2\ndescription: a sentence about it.", false},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(metaTree(tc.dir, tc.frontmatter)), "skills")
			if tc.wantProblem {
				if got := onlyProblem(t, problems); !strings.Contains(got, "name") {
					t.Errorf("problem %q does not name the rule broken", got)
				}
				return
			}
			if len(problems) != 0 {
				t.Errorf("problems: %s", strings.Join(lines(problems), " | "))
			}
		})
	}
}

func TestReadSource_RefusesADescriptionThatIsMissingEmptyOrTooLong(t *testing.T) {
	for name, tc := range map[string]struct {
		frontmatter string
		wantProblem bool
	}{
		"missing":         {"name: af-spec", true},
		"empty":           {"name: af-spec\ndescription: \"\"", true},
		"only whitespace": {"name: af-spec\ndescription: \"   \"", true},
		"1024 characters": {"name: af-spec\ndescription: " + strings.Repeat("d", descriptionLimit), false},
		"1025 characters": {"name: af-spec\ndescription: " + strings.Repeat("d", descriptionLimit+1), true},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(metaTree("af-spec", tc.frontmatter)), "skills")
			if tc.wantProblem {
				if got := onlyProblem(t, problems); !strings.Contains(got, "description") {
					t.Errorf("problem %q does not name the description", got)
				}
				return
			}
			if len(problems) != 0 {
				t.Errorf("problems: %s", strings.Join(lines(problems), " | "))
			}
		})
	}
}

func TestReadSource_SaysTheLimitWhenADescriptionIsTooLong(t *testing.T) {
	_, problems := readSource(tree(metaTree("af-spec", "name: af-spec\ndescription: "+strings.Repeat("d", descriptionLimit+1))), "skills")
	got := onlyProblem(t, problems)
	for _, want := range []string{"skills/af-spec/SKILL.md:", "1025", "1,024"} {
		if !strings.Contains(got, want) {
			t.Errorf("problem %q does not say %q", got, want)
		}
	}
}

// agentNamed returns the agent by name, failing when the table has no such row.
func agentNamed(t *testing.T, name string) agent {
	t.Helper()
	a, found := lookupAgent(name)
	if !found {
		t.Fatalf("no agent named %q", name)
	}
	return a
}

// oneSkill reads a one-skill tree and returns that skill, failing on a problem.
func oneSkill(t *testing.T, files map[string]string) (*source, *skill) {
	t.Helper()
	src, problems := readSource(tree(files), "skills")
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	if len(src.skills) != 1 {
		t.Fatalf("want one skill, got %d", len(src.skills))
	}
	return src, src.skills[0]
}

// frontmatterKeys returns the top-level keys of a rendered frontmatter, in the
// order they appear, so a test can assert both the set and the order.
func frontmatterKeys(doc string) []string {
	frontmatter, _, split := splitFrontmatter(doc)
	if !split {
		return nil
	}
	var keys []string
	for _, line := range strings.Split(frontmatter, "\n") {
		if m := topLevelKey.FindStringSubmatch(line); m != nil {
			keys = append(keys, m[1])
		}
	}
	return keys
}

const multiKeyFrontmatter = `name: af-spec
description: Writes a package in the session,
  continuing on a second line that the filter must keep.
argument-hint: "[a PRD path, an idea, or nothing]"
disable-model-invocation: true
metadata:
  author: the repository
  version: 2`

func TestRenderSkill_KeepsOnlyTheKeysTheAgentReadsInTheSourcesOrder(t *testing.T) {
	_, sk := oneSkill(t, map[string]string{
		"af-spec/SKILL.md": "---\n" + multiKeyFrontmatter + "\n---\n\n# af-spec\n\nThe body.\n",
	})

	for _, tc := range []struct {
		agent string
		want  []string
	}{
		{"claude", []string{"name", "description", "argument-hint", "disable-model-invocation"}},
		{"codex", []string{"name", "description"}},
		{"cursor", []string{"name", "description", "disable-model-invocation"}},
		{"copilot", []string{"name", "description"}},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			got := renderSkill(sk, agentNamed(t, tc.agent))
			if keys := frontmatterKeys(got.doc); !slices.Equal(keys, tc.want) {
				t.Errorf("keys = %v, want %v", keys, tc.want)
			}
			if strings.Contains(got.doc, "metadata:") || strings.Contains(got.doc, "the repository") {
				t.Error("the rendered frontmatter still carries metadata and its nested keys")
			}
			if !strings.Contains(got.doc, "  continuing on a second line that the filter must keep.") {
				t.Error("the description lost its continuation line")
			}
			if !strings.Contains(got.doc, `argument-hint: "[a PRD path, an idea, or nothing]"`) &&
				tc.agent == "claude" {
				t.Error("argument-hint was not kept with its source text")
			}
		})
	}
}

const invokeBody = "# af-spec\n\nRun {{invoke af-impl}} next, and {{invoke af-prd}} before it.\n\n" +
	"```sh\n{{invoke af-impl}} <NN>   # implement it\n```\n\n" +
	"| Step | Skill |\n|---|---|\n| after | {{invoke af-impl}} |\n"

func TestRenderSkill_ReplacesTheInvokeTokenAndNothingElse(t *testing.T) {
	files := map[string]string{
		"af-spec/SKILL.md":            skillFile("af-spec", invokeBody+"\nRead [references/tasks.md](references/tasks.md).\n"),
		"af-spec/references/tasks.md": "The plan, then {{invoke af-impl}}.\n",
		"af-prd/SKILL.md":             skillFile("af-prd", "# af-prd\n"),
		"af-impl/SKILL.md":            skillFile("af-impl", "# af-impl\n"),
	}
	src, problems := readSource(tree(files), "skills")
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	var spec *skill
	for _, sk := range src.skills {
		if sk.name == "af-spec" {
			spec = sk
		}
	}

	for _, tc := range []struct{ agent, impl, prd string }{
		{"claude", "/af-impl", "/af-prd"},
		{"codex", "$af-impl", "$af-prd"},
		{"cursor", "/af-impl", "/af-prd"},
		{"copilot", "/af-impl", "/af-prd"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			got := renderSkill(spec, agentNamed(t, tc.agent))
			want := strings.ReplaceAll(spec.body, "{{invoke af-impl}}", tc.impl)
			want = strings.ReplaceAll(want, "{{invoke af-prd}}", tc.prd)
			if got.body != want {
				t.Errorf("body differs from the source with only the token replaced:\ngot  %q\nwant %q", got.body, want)
			}
			if strings.Count(got.body, tc.impl) != 3 {
				t.Errorf("replaced %d of the 3 af-impl tokens", strings.Count(got.body, tc.impl))
			}
			if gotRef := got.refs["tasks.md"]; gotRef != "The plan, then "+tc.impl+".\n" {
				t.Errorf("the reference file was not rendered: %q", gotRef)
			}
			if strings.Contains(got.doc, "{{") {
				t.Error("a brace token survived into the rendered document")
			}
		})
	}
}

func TestReadSource_RefusesABraceTokenItCannotResolve(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"an invoke naming no skill": {
			files: map[string]string{"af-spec/SKILL.md": skillFile("af-spec", "Run {{invoke af-fix}} next.\n")},
			want:  []string{"skills/af-spec/SKILL.md:", "{{invoke af-fix}}", "af-fix"},
		},
		"another brace token": {
			files: map[string]string{"af-spec/SKILL.md": skillFile("af-spec", "Takes {{ARGUMENTS}} as input.\n")},
			want:  []string{"skills/af-spec/SKILL.md:", "{{ARGUMENTS}}"},
		},
		"an invoke with no name": {
			files: map[string]string{
				"af-spec/SKILL.md":        skillFile("af-spec", "Read [references/a.md](references/a.md).\n"),
				"af-spec/references/a.md": "Then {{invoke}} it.\n",
			},
			want: []string{"skills/af-spec/references/a.md:", "{{invoke}}"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := readSource(tree(tc.files), "skills")
			got := onlyProblem(t, problems)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("problem %q does not name %q", got, want)
				}
			}
		})
	}
}

func TestReadSource_RefusesAnArgumentPlaceholderOrAnAgentsOwnInvocationSyntax(t *testing.T) {
	for name, tc := range map[string]struct {
		body        string
		wantProblem bool
	}{
		"an argument placeholder":            {"The argument $ARGUMENTS is one of:\n", true},
		"a slash invocation in a code block": {"```sh\n/af-impl <NN>   # implement it\n```\n", true},
		"a slash invocation after a space":   {"Then run /af-impl to finish.\n", true},
		"a dollar invocation after a space":  {"Then run $af-impl to finish.\n", true},
		"a slash invocation in backticks":    {"Run `/af-impl` next.\n", true},
		"a slash invocation in parentheses":  {"Finish it (/af-impl) afterwards.\n", true},
		"a slash invocation in brackets":     {"See [/af-impl] for the next step.\n", true},
		"a slash invocation at a line start": {"/af-impl runs last.\n", true},
		"a path in prose":                    {"The detail is in skills/af-impl/references/landing.md today.\n", false},
		"a path in backticks":                {"Write it to `skills/af-impl/references/landing.md`.\n", false},
		"another repository's path":          {"It lives in spec/af-impl for now.\n", false},
		"the bare name":                      {"The af-impl skill implements the package.\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				"af-spec/SKILL.md": skillFile("af-spec", tc.body),
				"af-impl/SKILL.md": skillFile("af-impl", "# af-impl\n"),
			}
			_, problems := readSource(tree(files), "skills")
			if tc.wantProblem {
				if got := onlyProblem(t, problems); !strings.Contains(got, "{{invoke") {
					t.Errorf("problem %q does not give {{invoke ...}} as the replacement", got)
				}
				return
			}
			if len(problems) != 0 {
				t.Errorf("problems: %s", strings.Join(lines(problems), " | "))
			}
		})
	}
}

func TestMain_RendersWithNoCredentialAndNoReachableNetwork(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"af-spec/SKILL.md":            skillFile("af-spec", "Read [references/tasks.md](references/tasks.md).\n"),
		"af-spec/references/tasks.md": "# the plan\n",
	})
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--source", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

// docOf assembles a SKILL.md whose frontmatter and body are exactly the text
// given, so a test controls the body's byte length to the byte.
func docOf(frontmatter, body string) string {
	return "---\n" + frontmatter + "\n---\n" + body
}

// bodyOfBytes returns a body of exactly n bytes that begins with lead, pads
// with a byte the render has no rule about, and ends with a newline.
func bodyOfBytes(lead string, n int) string {
	if pad := n - len(lead) - 1; pad > 0 {
		return lead + strings.Repeat("x", pad) + "\n"
	}
	return lead[:n-1] + "\n"
}

// the frontmatter is padded to exactly 300 bytes, so a case can show that its
// size is counted in no measurement of the body.
var paddedFrontmatter = func() string {
	head := "name: af-x\ndescription: "
	return head + strings.Repeat("d", 300-len(head)-1) + "."
}()

func TestReadSource_MeasuresTheBodyAgainstTheCeilingInBytesAfterSubstitution(t *testing.T) {
	if len(paddedFrontmatter) != 300 {
		t.Fatalf("the padded frontmatter is %d bytes, want 300", len(paddedFrontmatter))
	}
	for name, tc := range map[string]struct {
		body      string
		wantOver  bool
		wantBytes string
	}{
		"exactly at the ceiling": {
			body: bodyOfBytes("# af-x\n", bodyLimit),
		},
		"one byte over": {
			body: bodyOfBytes("# af-x\n", bodyLimit+1), wantOver: true, wantBytes: "24,577",
		},
		"at the ceiling in characters but over it in bytes": {
			// 24,575 single-byte runes and one two-byte rune: 24,576 characters
			// and 24,577 bytes, which the ceiling counts.
			body: strings.Repeat("x", bodyLimit-1) + "é", wantOver: true, wantBytes: "24,577",
		},
		"over before substitution and at the ceiling after it": {
			// {{invoke af-impl}} is 18 bytes and /af-impl or $af-impl is 8, so
			// the rendered body is 10 bytes shorter than the source's.
			body: bodyOfBytes("{{invoke af-impl}}\n", bodyLimit+10),
		},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				"af-x/SKILL.md":    docOf(paddedFrontmatter, tc.body),
				"af-impl/SKILL.md": skillFile("af-impl", "# af-impl\n"),
			}
			_, problems := readSource(tree(files), "skills")
			if !tc.wantOver {
				if len(problems) != 0 {
					t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
				}
				return
			}
			// One line per agent the body is over for; every agent's prefix is
			// one byte, so it is over for all four at the same size.
			if len(problems) != len(agents) {
				t.Fatalf("want %d problems, one per agent, got %d: %s",
					len(agents), len(problems), strings.Join(lines(problems), " | "))
			}
			want := "skills/af-x/SKILL.md: body is " + tc.wantBytes +
				" bytes for claude, " + commas(len(tc.body)-bodyLimit) + " over the 24,576-byte limit"
			if got := lines(problems); !slices.Contains(got, want) {
				t.Errorf("no problem reads\n  %s\ngot\n  %s", want, strings.Join(got, "\n  "))
			}
		})
	}
}

func TestReadSource_DoesNotMeasureAReferenceFile(t *testing.T) {
	files := map[string]string{
		"af-x/SKILL.md":          docOf(paddedFrontmatter, bodyOfBytes("Read [references/big.md](references/big.md).\n", bodyLimit)),
		"af-x/references/big.md": strings.Repeat("y", 60000),
	}
	src, problems := readSource(tree(files), "skills")
	if len(problems) != 0 {
		t.Fatalf("problems: %s", strings.Join(lines(problems), " | "))
	}
	if got := len(src.skills[0].refs["big.md"].data); got != 60000 {
		t.Errorf("the reference file is %d bytes, want it rendered whole", got)
	}
}

func TestCommas_WritesEveryNumberTheWayAMessageSpellsIt(t *testing.T) {
	for in, want := range map[int]string{0: "0", 1: "1", 999: "999", 1000: "1,000", 24576: "24,576", 31204: "31,204", 1234567: "1,234,567"} {
		if got := commas(in); got != want {
			t.Errorf("commas(%d) = %q, want %q", in, got, want)
		}
	}
}
