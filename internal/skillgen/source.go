package main

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// sharedDir is the directory under the source root that holds the reference
// files more than one skill links. A directory whose name begins with "_" is
// not a skill, so the pool cannot be mistaken for one.
const sharedDir = "_shared"

// refsSubdir is the directory, under a skill or under the shared pool, that
// holds reference files. A skill links one as references/<file>.md, and that
// link is the inclusion: there is no manifest to keep in step.
const refsSubdir = "references"

// skillDoc is the one file every skill directory must hold.
const skillDoc = "SKILL.md"

// delimiter opens and closes a SKILL.md's YAML frontmatter.
const delimiter = "---"

// problem is one finding the render reports, written as "<path>: <msg>" on one
// line of stderr. A problem is fatal and a warning is not; both take this form.
type problem struct {
	path string
	msg  string
}

func (p problem) String() string { return p.path + ": " + p.msg }

// ref is a reference file available to a skill, with the path it was read from
// so that a message can say which of the two directories resolved it.
type ref struct {
	name string // the base name, e.g. "tasks.md"
	from string // the display path it was read from
	data []byte
}

// skill is one directory under the source root: its SKILL.md split into the
// frontmatter text and the body, the reference files in its own directory, and
// the ones it includes.
type skill struct {
	name        string // the directory name
	dir         string // display path of the directory, e.g. "skills/af-spec"
	path        string // display path of its SKILL.md
	frontmatter string // the raw text between the two delimiters, without them
	body        string // everything after the line that closes the frontmatter
	meta        meta   // the frontmatter keys the render reads

	own  map[string]*ref // the files in this skill's own references/
	refs map[string]*ref // the files it includes, its own and shared together
}

// source is a parsed skills tree: the skills in directory order and the shared
// reference pool.
type source struct {
	root   string // the display prefix every path in a message carries
	skills []*skill
	shared map[string]*ref
}

// refLink matches a Markdown link whose target is a reference file. The link is
// the inclusion, so this expression is what decides which files a skill holds.
var refLink = regexp.MustCompile(`]\(` + refsSubdir + `/([^)]+\.md)\)`)

// readSource reads a skills tree from fsys, whose root is the skills directory
// itself, and reports every problem it holds rather than the first. root is the
// display prefix every message uses, so a message names the path an author
// would edit, whatever fsys is.
func readSource(fsys fs.FS, root string) (*source, []problem) {
	src := &source{root: root, shared: map[string]*ref{}}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return src, []problem{{root, "cannot be read: " + err.Error()}}
	}
	src.shared = readRefs(fsys, sharedDir, root)

	var problems []problem
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		sk, found := readSkill(fsys, entry.Name(), root)
		if !found.ok {
			problems = append(problems, found.problem)
			continue
		}
		src.skills = append(src.skills, sk)
	}
	for _, sk := range src.skills {
		problems = append(problems, checkMeta(sk)...)
		problems = append(problems, src.shadowed(sk)...)
		problems = append(problems, src.include(sk)...)
	}
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].path != problems[j].path {
			return problems[i].path < problems[j].path
		}
		return problems[i].msg < problems[j].msg
	})
	return src, problems
}

// outcome is the result of reading one skill: the skill, or the one problem
// that stopped it being read at all.
type outcome struct {
	ok      bool
	problem problem
}

// readSkill reads one skill directory. A skill whose SKILL.md is missing or
// whose frontmatter cannot be read is not a skill the render can work with, so
// it is reported and left out rather than half-read.
func readSkill(fsys fs.FS, name, root string) (*skill, outcome) {
	dir := path.Join(root, name)
	file := path.Join(dir, skillDoc)

	data, err := fs.ReadFile(fsys, path.Join(name, skillDoc))
	if err != nil {
		return nil, outcome{problem: problem{dir, "holds no " + skillDoc}}
	}
	frontmatter, body, split := splitFrontmatter(string(data))
	if !split {
		return nil, outcome{problem: problem{file, "has no frontmatter"}}
	}
	var parsed meta
	if err := yaml.Unmarshal([]byte(frontmatter), &parsed); err != nil {
		return nil, outcome{problem: problem{file, "frontmatter is not YAML: " + err.Error()}}
	}
	return &skill{
		name:        name,
		dir:         dir,
		path:        file,
		frontmatter: frontmatter,
		body:        body,
		meta:        parsed,
		own:         readRefs(fsys, name, root),
	}, outcome{ok: true}
}

// splitFrontmatter splits a SKILL.md into the text between its two delimiters
// and the body that follows the closing one. split is false when the file does
// not open with a delimiter or never closes it: then it has no frontmatter.
func splitFrontmatter(data string) (frontmatter, body string, split bool) {
	open := delimiter + "\n"
	if !strings.HasPrefix(data, open) {
		return "", "", false
	}
	rest := data[len(open):]
	close := "\n" + delimiter + "\n"
	if i := strings.Index(rest, close); i >= 0 {
		return rest[:i], rest[i+len(close):], true
	}
	// A file that ends on its closing delimiter has a frontmatter and no body.
	if trailing := "\n" + delimiter; strings.HasSuffix(rest, trailing) {
		return rest[:len(rest)-len(trailing)], "", true
	}
	return "", "", false
}

// readRefs reads the reference files in dir/references/. A directory that does
// not exist is not a problem: a skill need not have reference files of its own.
func readRefs(fsys fs.FS, dir, root string) map[string]*ref {
	refs := map[string]*ref{}
	entries, err := fs.ReadDir(fsys, path.Join(dir, refsSubdir))
	if err != nil {
		return refs
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, refsSubdir, entry.Name()))
		if err != nil {
			continue
		}
		refs[entry.Name()] = &ref{
			name: entry.Name(),
			from: path.Join(root, dir, refsSubdir, entry.Name()),
			data: data,
		}
	}
	return refs
}

// shadowed reports a reference file name that is in both a skill's own
// directory and the shared pool. It is an error and not an override, so that
// one skill cannot drift from its siblings without a word.
func (s *source) shadowed(sk *skill) []problem {
	var problems []problem
	for _, name := range sortedNames(sk.own) {
		if _, both := s.shared[name]; !both {
			continue
		}
		problems = append(problems, problem{sk.dir, fmt.Sprintf(
			"%s is in both %s/ and %s/; one name must not be in both",
			name, path.Join(sk.dir, refsSubdir), path.Join(s.root, sharedDir, refsSubdir))})
	}
	return problems
}

// include walks the links from a skill's body through the reference files it
// reaches, so that a file is included when the skill links it or when a file
// already included links it, and no other file is.
func (s *source) include(sk *skill) []problem {
	sk.refs = map[string]*ref{}

	var problems []problem
	queue := []*ref{{from: sk.path, data: []byte(sk.body)}}
	for len(queue) > 0 {
		from := queue[0]
		queue = queue[1:]
		for _, name := range linkedRefs(string(from.data)) {
			if _, already := sk.refs[name]; already {
				continue
			}
			target := sk.own[name]
			if target == nil {
				target = s.shared[name]
			}
			if target == nil {
				problems = append(problems, problem{from.from, fmt.Sprintf(
					"links %s/%s, which is in neither %s/ nor %s/",
					refsSubdir, name,
					path.Join(sk.dir, refsSubdir), path.Join(s.root, sharedDir, refsSubdir))})
				continue
			}
			sk.refs[name] = target
			queue = append(queue, target)
		}
	}
	return problems
}

// linkedRefs returns the reference file names text links, each once, in the
// order they first appear, so that a file linked twice is one inclusion and a
// missing one is one problem.
func linkedRefs(text string) []string {
	var found []string
	seen := map[string]bool{}
	for _, match := range refLink.FindAllStringSubmatch(text, -1) {
		if name := match[1]; !seen[name] {
			seen[name] = true
			found = append(found, name)
		}
	}
	return found
}

// sortedNames returns a reference map's keys in order, so that every message
// the render writes comes out the same on every run.
func sortedNames(refs map[string]*ref) []string {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
