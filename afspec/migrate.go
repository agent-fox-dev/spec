package afspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// The migration from format version 2 (format v3 §13). LoadV2 reads a v2
// package and returns the v3 package it converts to; MigrateV2 writes that
// package in place. The conversion is deterministic: the same v2 package
// always gives the same v3 package, so it is run once over the packages of a
// spec root and never again.

// TestSpecFileName is the v2 artifact the migration removes.
const TestSpecFileName = "test_spec.json"

// The v2 artifacts, as far as the conversion reads them. They are decoded
// loosely: the conversion refuses what it cannot convert and leaves the rest
// to the v3 validation of the result.
type (
	v2Requirements struct {
		SpecId         string            `json:"spec_id"`
		SpecName       string            `json:"spec_name"`
		Glossary       map[string]string `json:"glossary"`
		Requirements   []v2Requirement   `json:"requirements"`
		ExecutionPaths []v2Path          `json:"execution_paths"`
		ExternalApis   []ExternalApi     `json:"external_apis"`
	}
	v2Requirement struct {
		Id        string        `json:"id"`
		Title     string        `json:"title"`
		Rationale *string       `json:"rationale"`
		Criteria  []v2Criterion `json:"criteria"`
	}
	v2Criterion struct {
		Id        string  `json:"id"`
		Pattern   string  `json:"pattern"`
		Condition *string `json:"condition"`
		Guard     *string `json:"guard"`
		System    *string `json:"system"`
		Action    string  `json:"action"`
		Contract  *string `json:"contract"`
	}
	v2Path struct {
		Id    string     `json:"id"`
		Title string     `json:"title"`
		Steps []PathStep `json:"steps"`
	}
	v2TestSpec struct {
		Tests []v2Test `json:"tests"`
	}
	v2Test struct {
		Id             string   `json:"id"`
		Kind           string   `json:"kind"`
		Verifies       []string `json:"verifies"`
		Given          []string `json:"given"`
		Then           []string `json:"then"`
		Pseudocode     *string  `json:"pseudocode"`
		RealComponents []string `json:"real_components"`
	}
	v2Tasks struct {
		SpecId       string       `json:"spec_id"`
		SpecName     string       `json:"spec_name"`
		TestCommands TestCommands `json:"test_commands"`
		Dependencies []Dependency `json:"dependencies"`
		Tasks        []v2Task     `json:"tasks"`
	}
	v2Task struct {
		Id        int      `json:"id"`
		Kind      string   `json:"kind"`
		Title     string   `json:"title"`
		Criteria  []string `json:"criteria"`
		Steps     []string `json:"steps"`
		Touches   []string `json:"touches"`
		DependsOn []int    `json:"depends_on"`
		DoneWhen  []string `json:"done_when"`
		State     string   `json:"state"`
	}
)

// migrationError is the error of a conversion that cannot be made: a package
// that is not version 2, or a version 2 package with a gap the rules of its
// own version forbid.
func migrationError(dir, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return &LoadError{Msg: msg, File: dir, Err: &SpecError{Msg: msg}}
}

// LoadV2 reads the version 2 package in dir and returns the version 3 package
// it converts to, in memory, by the rules of format v3 §13. Nothing is
// written. It returns a LoadError when the package is not version 2, when it
// cannot be read, or when a criterion is verified by no test or a path by no
// smoke test, which version 2 forbade and which has no deterministic
// conversion.
func LoadV2(dir string) (*Spec, error) {
	prdPath := filepath.Join(dir, "prd.md")
	prdData, err := os.ReadFile(prdPath)
	if err != nil {
		return nil, &LoadError{Msg: fmt.Sprintf("cannot read prd.md: %s", err), File: prdPath, Err: &SpecError{Msg: fmt.Sprintf("cannot read prd.md: %s", err)}}
	}
	fm, body, err := parsePRD(prdData, prdPath)
	if err != nil {
		return nil, err
	}
	if fm.SchemaVersion != 2 {
		return nil, migrationError(dir, "the package declares schema_version %d; the migration converts version 2 packages only", fm.SchemaVersion)
	}

	var req v2Requirements
	var tests v2TestSpec
	var tasks v2Tasks
	extensions := map[string]extensionSet{}
	for _, f := range []struct {
		name   string
		target any
	}{{RequirementsFileName, &req}, {TestSpecFileName, &tests}, {TasksFileName, &tasks}} {
		data, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return nil, &LoadError{Msg: fmt.Sprintf("cannot read %s: %s", f.name, err), File: filepath.Join(dir, f.name), Err: &SpecError{Msg: fmt.Sprintf("cannot read %s: %s", f.name, err)}}
		}
		if err := json.Unmarshal(data, f.target); err != nil {
			return nil, &LoadError{Msg: fmt.Sprintf("cannot parse %s: %s", f.name, err), File: filepath.Join(dir, f.name), Err: &SpecError{Msg: fmt.Sprintf("cannot parse %s: %s", f.name, err)}}
		}
		// The v3 types are what the extension keys are addressed against;
		// the objects that carry them exist in both versions, under ids
		// the conversion shortens below.
		var raw any
		if json.Unmarshal(data, &raw) == nil && f.name != TestSpecFileName {
			set := extensionSet{}
			target := any(&RequirementsV3Json{})
			if f.name == TasksFileName {
				target = &TasksV3Json{}
			}
			collectExtensions(raw, reflect.TypeOf(target), "", set)
			if len(set) > 0 {
				extensions[f.name] = set
			}
		}
	}

	c := &converter{specID: fm.SpecID, dir: dir}
	requirements, err := c.requirements(req, tests)
	if err != nil {
		return nil, err
	}
	plan, progress, err := c.tasks(tasks, stringOf(fm.Raw["updated_at"]))
	if err != nil {
		return nil, err
	}

	spec := &Spec{
		SpecID:          fm.SpecID,
		SpecName:        fm.SpecName,
		Title:           fm.Title,
		Status:          fm.Status,
		CreatedAt:       fm.CreatedAt,
		Source:          fm.Source,
		Supersedes:      fm.Supersedes,
		IntentHash:      fm.IntentHash,
		SchemaVersion:   SchemaVersion,
		SteeringVersion: fm.SteeringVersion,
		UpdatedAt:       progress.UpdatedAt,
		PRDBody:         withGlossarySection(body, req.Glossary),
		Dir:             dir,
		Requirements:    requirements,
		Tasks:           plan,
		Progress:        progress,
		extensions:      c.rekey(extensions, fm),
		fromDisk:        true,
	}
	if archData, err := os.ReadFile(filepath.Join(dir, "architecture.md")); err == nil {
		spec.Architecture = string(archData)
	}
	return spec, nil
}

// MigrateV2 converts the version 2 package in dir to version 3 in place: it
// writes prd.md, requirements.json, tasks.json, progress.json, architecture.md
// when there is one and spec.md, through the temporary files and renames a
// save uses, whatever the package's status, and removes test_spec.json. The
// errors are LoadV2's, and a SaveError when the write or the removal fails.
func MigrateV2(dir string) error {
	spec, err := LoadV2(dir)
	if err != nil {
		return err
	}
	if err := spec.saveToDisk(dir); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, TestSpecFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return saveFailure("the package was written as version %d, and %s could not be removed: %s", SchemaVersion, TestSpecFileName, err)
	}
	return nil
}

// converter carries the package's identity through the conversion.
type converter struct {
	specID string
	dir    string
}

// shorten takes the package's prefix off a v2 REQ or PATH id.
func (c *converter) shorten(id string) string {
	return strings.TrimPrefix(id, c.specID+"-")
}

// requirements converts the requirements and the tests: every criterion gets
// its check from the tests that verify it, every path its smoke check from
// the smoke tests that verify it (format v3 §13).
func (c *converter) requirements(req v2Requirements, tests v2TestSpec) (*RequirementsV3Json, error) {
	criteria := map[string]bool{}
	for _, r := range req.Requirements {
		for _, cr := range r.Criteria {
			criteria[c.shorten(cr.Id)] = true
		}
	}
	paths := map[string]bool{}
	for _, p := range req.ExecutionPaths {
		paths[c.shorten(p.Id)] = true
	}

	// owner is the first criterion of this package in a test's verifies; a
	// test's criteria are those of its verifies that are criteria.
	owner := map[int]string{}
	verifiedBy := map[string][]int{} // criterion → non-smoke tests, in order
	smokedBy := map[string][]int{}   // criterion or path → smoke tests, in order
	for i, t := range tests.Tests {
		for _, ref := range t.Verifies {
			id := c.shorten(ref)
			switch {
			case criteria[id] && t.Kind != "smoke":
				if _, taken := owner[i]; !taken {
					owner[i] = id
				}
				verifiedBy[id] = append(verifiedBy[id], i)
			case criteria[id] || paths[id]:
				smokedBy[id] = append(smokedBy[id], i)
			}
		}
	}
	owned := map[string][]int{} // criterion → the tests it owns, in order
	for i := range tests.Tests {
		if o, ok := owner[i]; ok {
			owned[o] = append(owned[o], i)
		}
	}

	out := &RequirementsV3Json{
		Schema:        requirementsSchemaURI,
		SpecId:        req.SpecId,
		SpecName:      req.SpecName,
		SchemaVersion: SchemaVersion,
		ExternalApis:  req.ExternalApis,
	}
	for _, r := range req.Requirements {
		nr := Requirement{Id: c.shorten(r.Id), Title: r.Title, Rationale: r.Rationale}
		for _, cr := range r.Criteria {
			nc := Criterion{
				Id:        c.shorten(cr.Id),
				Pattern:   CriterionPattern(cr.Pattern),
				Condition: cr.Condition,
				System:    cr.System,
				Action:    cr.Action,
				Contract:  cr.Contract,
			}
			if cr.Pattern == "complex_event" {
				nc.Pattern = CriterionPatternEventDriven
				cond := cr.Condition
				if cond == nil {
					cond = strPtr("")
				}
				if cr.Guard != nil {
					cond = strPtr(*cond + " and " + *cr.Guard)
				}
				nc.Condition = cond
			}
			switch {
			case len(owned[nc.Id]) > 0:
				first := tests.Tests[owned[nc.Id][0]]
				kind := CheckKind(first.Kind)
				nc.Check = Check{Kind: &kind}
				var setup []string
				var pseudocode []string
				for _, i := range owned[nc.Id] {
					t := tests.Tests[i]
					setup = appendNew(setup, t.Given...)
					if t.Pseudocode != nil && *t.Pseudocode != "" {
						pseudocode = append(pseudocode, *t.Pseudocode)
					}
				}
				if len(setup) > 0 {
					nc.Check.Setup = setup
				}
				if len(pseudocode) > 0 {
					nc.Check.Pseudocode = strPtr(strings.Join(pseudocode, "\n"))
				}
				if nc.ContractText() == "" && len(first.Then) > 0 {
					nc.Contract = strPtr(strings.Join(first.Then, "; "))
				}
			case len(verifiedBy[nc.Id]) > 0:
				nc.Check = SameAs(owner[verifiedBy[nc.Id][0]])
			case len(smokedBy[nc.Id]) > 0:
				t := tests.Tests[smokedBy[nc.Id][0]]
				var path string
				for _, ref := range t.Verifies {
					if id := c.shorten(ref); paths[id] {
						path = id
						break
					}
				}
				if path == "" {
					return nil, migrationError(c.dir, "criterion %s is verified by the smoke test %s alone, which verifies no execution path", cr.Id, t.Id)
				}
				nc.Check = SameAs(path)
			default:
				return nil, migrationError(c.dir, "criterion %s is verified by no test, which format v2 rule C4 forbids; the conversion has no check for it", cr.Id)
			}
			nr.Criteria = append(nr.Criteria, nc)
		}
		hoistSystem(&nr)
		out.Requirements = append(out.Requirements, nr)
	}

	out.ExecutionPaths = []ExecutionPath{}
	for _, p := range req.ExecutionPaths {
		np := ExecutionPath{Id: c.shorten(p.Id), Title: p.Title, Steps: p.Steps}
		if len(smokedBy[np.Id]) == 0 {
			return nil, migrationError(c.dir, "execution path %s is verified by no smoke test, which format v2 rule C5 forbids; the conversion has no smoke check for it", p.Id)
		}
		var pseudocode []string
		for _, i := range smokedBy[np.Id] {
			t := tests.Tests[i]
			np.Smoke.RealComponents = appendNew(np.Smoke.RealComponents, t.RealComponents...)
			np.Smoke.Setup = appendNew(np.Smoke.Setup, t.Given...)
			np.Smoke.EndState = appendNew(np.Smoke.EndState, t.Then...)
			if t.Pseudocode != nil && *t.Pseudocode != "" {
				pseudocode = append(pseudocode, *t.Pseudocode)
			}
		}
		if len(pseudocode) > 0 {
			np.Smoke.Pseudocode = strPtr(strings.Join(pseudocode, "\n"))
		}
		out.ExecutionPaths = append(out.ExecutionPaths, np)
	}
	return out, nil
}

// hoistSystem moves a system every criterion of the requirement names to the
// requirement, and off the criteria. A requirement whose criteria disagree,
// or one of which names none, is left as it is: a default a criterion would
// inherit changes its sentence.
func hoistSystem(r *Requirement) {
	if len(r.Criteria) == 0 {
		return
	}
	var system string
	for i, c := range r.Criteria {
		if c.System == nil || *c.System == "" {
			return
		}
		if i == 0 {
			system = *c.System
		} else if *c.System != system {
			return
		}
	}
	r.System = strPtr(system)
	for i := range r.Criteria {
		r.Criteria[i].System = nil
	}
}

// tasks converts the plan: every implement task without its kind, tests,
// state and optional flag, with its state in the progress; the integration
// task removed, with its state as the progress entry of task N+1.
func (c *converter) tasks(tasks v2Tasks, updatedAt string) (*TasksV3Json, *ProgressV3Json, error) {
	out := &TasksV3Json{
		Schema:        tasksSchemaURI,
		SpecId:        tasks.SpecId,
		SpecName:      tasks.SpecName,
		SchemaVersion: SchemaVersion,
		TestCommands:  tasks.TestCommands,
		Dependencies:  tasks.Dependencies,
		Tasks:         []Task{},
	}
	if out.Dependencies == nil {
		out.Dependencies = []Dependency{}
	}
	progress := &ProgressV3Json{
		Schema:        progressSchemaURI,
		SpecId:        tasks.SpecId,
		SpecName:      tasks.SpecName,
		SchemaVersion: SchemaVersion,
		UpdatedAt:     updatedAt,
		Tasks:         []TaskProgress{},
	}
	integrationState := ""
	for _, t := range tasks.Tasks {
		if t.Kind == "integration" {
			integrationState = t.State
			continue
		}
		nt := Task{Id: t.Id, Title: t.Title, Criteria: make([]string, 0, len(t.Criteria)), Steps: t.Steps,
			Touches: t.Touches, DependsOn: t.DependsOn, DoneWhen: t.DoneWhen}
		for _, ref := range t.Criteria {
			nt.Criteria = append(nt.Criteria, c.shorten(ref))
		}
		out.Tasks = append(out.Tasks, nt)
		progress.Tasks = append(progress.Tasks, TaskProgress{Id: t.Id, State: stateOf(t.State)})
	}
	if integrationState != "" && len(out.Tasks) > 0 {
		progress.Tasks = append(progress.Tasks, TaskProgress{Id: NextTaskID(*out), State: stateOf(integrationState)})
	}
	sort.SliceStable(progress.Tasks, func(i, j int) bool { return progress.Tasks[i].Id < progress.Tasks[j].Id })
	return out, progress, nil
}

// stateOf is a v2 task state as a v3 one; a state v2 did not define reads as
// pending.
func stateOf(state string) TaskState {
	s := TaskState(state)
	if _, ok := taskTransitions[s]; !ok {
		return TaskStatePending
	}
	return s
}

// rekey moves the extension keys read from the v2 files to the addresses
// their objects have in v3: the REQ and PATH ids without the package's
// prefix. The keys of the integration task, which v3 does not store, are
// dropped. The frontmatter's keys gain x-owner and x-tags when the v2
// frontmatter set owner or tags.
func (c *converter) rekey(ext map[string]extensionSet, fm *prdFrontmatter) map[string]extensionSet {
	out := map[string]extensionSet{}
	for file, set := range ext {
		moved := extensionSet{}
		for address, keys := range set {
			moved[strings.ReplaceAll(strings.ReplaceAll(address, "id="+c.specID+"-REQ-", "id=REQ-"), "id="+c.specID+"-PATH-", "id=PATH-")] = keys
		}
		if len(moved) > 0 {
			out[file] = moved
		}
	}
	front := map[string]any{}
	for k, v := range fm.Extensions {
		front[k] = v
	}
	if owner := stringOf(fm.Raw["owner"]); owner != "" {
		front["x-owner"] = owner
	}
	if tags, ok := fm.Raw["tags"].([]any); ok && len(tags) > 0 {
		front["x-tags"] = tags
	}
	if len(front) > 0 {
		out["prd.md"] = extensionSet{"": front}
	}
	return out
}

// withGlossarySection appends the v2 glossary to the PRD body as a
// "## Glossary" section, one term a line in term order, or returns the body
// as it was when there is none.
func withGlossarySection(body string, glossary map[string]string) string {
	if len(glossary) == 0 {
		return body
	}
	terms := make([]string, 0, len(glossary))
	for term := range glossary {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n## Glossary\n\n")
	for _, term := range terms {
		fmt.Fprintf(&b, "- **%s**: %s\n", term, glossary[term])
	}
	return b.String()
}

// appendNew appends to list the items it does not hold yet, in order.
func appendNew(list []string, items ...string) []string {
	for _, it := range items {
		if !slices.Contains(list, it) {
			list = append(list, it)
		}
	}
	return list
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}
