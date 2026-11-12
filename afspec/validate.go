package afspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidationResult holds the results of spec validation. Valid is true if and
// only if Errors is empty; warnings never block.
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationEntry
	Warnings []ValidationEntry
}

// ValidationEntry is a single validation error or warning.
type ValidationEntry struct {
	Category string // "schema", "integrity" or "warning"
	Message  string
	Artifact string // the artifact file the issue was found in
	Path     string // JSON path (InstanceLocation) for schema errors
	Keyword  string // KeywordLocation for schema errors
	Check    string // rule name: "json_schema", "completeness", "C1".."C8", "scope_limit", "missing_contract", …
	EntityID string // the entity the entry is about
	Value    string // optional value context
}

// ---------------------------------------------------------------------------
// JSON Schema validation infrastructure
// ---------------------------------------------------------------------------

var (
	compiledSchemas     map[string]*jsonschema.Schema
	compiledSchemasErr  error
	compiledSchemasOnce sync.Once
)

// embeddedURLLoader implements jsonschema.URLLoader, serving schema bytes from
// the embedded schemaFS under the "https://agent-fox.dev/schemas/" namespace.
type embeddedURLLoader struct {
	schemas map[string][]byte
}

func (l *embeddedURLLoader) Load(url string) (any, error) {
	const prefix = "https://agent-fox.dev/schemas/"
	if !strings.HasPrefix(url, prefix) {
		return nil, fmt.Errorf("unsupported schema URL: %s", url)
	}
	name := strings.TrimPrefix(url, prefix)
	data, ok := l.schemas[name]
	if !ok {
		return nil, fmt.Errorf("schema not found: %s", name)
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

// getCompiledSchemas compiles the bundled schemas once and caches the result.
func getCompiledSchemas() (map[string]*jsonschema.Schema, error) {
	compiledSchemasOnce.Do(func() {
		schemas := Schemas()
		loader := &embeddedURLLoader{schemas: schemas}

		c := jsonschema.NewCompiler()
		c.UseLoader(jsonschema.SchemeURLLoader{"https": loader})

		compiled := make(map[string]*jsonschema.Schema, len(schemas))
		for name, data := range schemas {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				compiledSchemasErr = &LoadError{
					Msg:  fmt.Sprintf("cannot parse schema %s: %s", name, err),
					File: name,
					Err:  &SpecError{Msg: fmt.Sprintf("cannot parse schema %s: %s", name, err)},
				}
				return
			}
			if err := c.AddResource("https://agent-fox.dev/schemas/"+name, doc); err != nil {
				compiledSchemasErr = &LoadError{
					Msg:  fmt.Sprintf("cannot add schema resource %s: %s", name, err),
					File: name,
					Err:  &SpecError{Msg: fmt.Sprintf("cannot add schema resource %s: %s", name, err)},
				}
				return
			}
		}

		for name := range schemas {
			sch, err := c.Compile("https://agent-fox.dev/schemas/" + name)
			if err != nil {
				compiledSchemasErr = &LoadError{
					Msg:  fmt.Sprintf("cannot compile schema %s: %s", name, err),
					File: name,
					Err:  &SpecError{Msg: fmt.Sprintf("cannot compile schema %s: %s", name, err)},
				}
				return
			}
			compiled[name] = sch
		}
		compiledSchemas = compiled
	})
	return compiledSchemas, compiledSchemasErr
}

// flattenValidationError flattens a jsonschema.ValidationError tree into its
// leaf errors.
func flattenValidationError(ve *jsonschema.ValidationError, artifact string) []ValidationEntry {
	if len(ve.Causes) == 0 {
		instanceLoc := ""
		if len(ve.InstanceLocation) > 0 {
			instanceLoc = "/" + strings.Join(ve.InstanceLocation, "/")
		}
		keywordLoc, msg := "", ""
		if ve.ErrorKind != nil {
			keywordLoc = "/" + strings.Join(ve.ErrorKind.KeywordPath(), "/")
			msg = ve.Error()
		}
		return []ValidationEntry{{
			Category: "schema",
			Check:    "json_schema",
			Message:  msg,
			Artifact: artifact,
			Path:     instanceLoc,
			Keyword:  keywordLoc,
		}}
	}

	var entries []ValidationEntry
	for _, cause := range ve.Causes {
		entries = append(entries, flattenValidationError(cause, artifact)...)
	}
	return entries
}

// ValidateArtifactSchema validates one already-decoded artifact against the
// named bundled schema. It is exported so the generation pipeline can check a
// model's tool input before it is turned into a Spec.
func ValidateArtifactSchema(artifact any, schemaName, artifactName string) []ValidationEntry {
	schemas, err := getCompiledSchemas()
	if err != nil {
		return []ValidationEntry{{
			Category: "schema", Check: "json_schema", Artifact: artifactName,
			Message: fmt.Sprintf("schema compilation error: %s", err),
		}}
	}

	sch, ok := schemas[schemaName]
	if !ok {
		return []ValidationEntry{{
			Category: "schema", Check: "json_schema", Artifact: artifactName,
			Message: fmt.Sprintf("schema not found: %s", schemaName),
		}}
	}

	// The validator works on decoded JSON values, not on Go structs, so a
	// struct is round-tripped through encoding/json first. A value that is
	// already decoded JSON is passed straight through.
	instance := artifact
	switch artifact.(type) {
	case map[string]any, []any:
	default:
		data, marshalErr := json.Marshal(artifact)
		if marshalErr != nil {
			return []ValidationEntry{{
				Category: "schema", Check: "json_schema", Artifact: artifactName,
				Message: fmt.Sprintf("cannot marshal %s: %s", artifactName, marshalErr),
			}}
		}
		var decoded any
		if parseErr := json.Unmarshal(data, &decoded); parseErr != nil {
			return []ValidationEntry{{
				Category: "schema", Check: "json_schema", Artifact: artifactName,
				Message: fmt.Sprintf("cannot parse %s JSON: %s", artifactName, parseErr),
			}}
		}
		instance = decoded
	}

	validationErr := sch.Validate(instance)
	if validationErr == nil {
		return nil
	}

	ve, ok := validationErr.(*jsonschema.ValidationError)
	if !ok {
		return []ValidationEntry{{
			Category: "schema", Check: "json_schema", Artifact: artifactName,
			Message: fmt.Sprintf("validation error: %s", validationErr),
		}}
	}
	return flattenValidationError(ve, artifactName)
}

// Schema file names for the four v3 artifacts.
const (
	PRDFrontmatterSchemaName = "prd-frontmatter.v3.json"
	RequirementsSchemaName   = "requirements.v3.json"
	TasksSchemaName          = "tasks.v3.json"
	ProgressSchemaName       = "progress.v3.json"
)

// prdFrontmatterForSchema carries exactly the fields of
// prd-frontmatter.v3.json, so that non-schema fields of Spec cannot produce
// spurious additionalProperties errors. The optional four are omitted when
// empty, matching what renderPRD writes.
type prdFrontmatterForSchema struct {
	SpecID          string   `json:"spec_id"`
	SpecName        string   `json:"spec_name"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	IntentHash      *string  `json:"intent_hash"`
	SchemaVersion   int      `json:"schema_version"`
	Profile         string   `json:"profile,omitempty"`
	SteeringVersion string   `json:"steering_version,omitempty"`
	Source          string   `json:"source,omitempty"`
	Supersedes      []string `json:"supersedes,omitempty"`
}

// legacyVersionEntry describes a spec still written in an older format
// version. Validating it against the v3 schemas produces a schema error per
// artifact plus every shape difference underneath; one sentence naming the
// version and the way to convert it is more use than that list.
func legacyVersionEntry(version int) ValidationEntry {
	msg := fmt.Sprintf("spec declares schema_version %d; this library reads version %d only", version, SchemaVersion)
	if version == 2 {
		msg += " — speca <package> --migrate converts a version 2 package in place (format v3 §13 maps each v2 construct to its v3 form)"
	} else {
		msg += fmt.Sprintf(" — format v3 §13 maps each v2 construct to its v3 form; a version %d package has no conversion", version)
	}
	return ValidationEntry{
		Category: "integrity",
		Check:    "schema_version",
		Artifact: "prd.md",
		Message:  msg,
	}
}

// scaffoldEntry describes a spec that has been created but not yet generated.
// An empty artifact list violates the schemas' minItems, which would bury the
// operator in schema noise; §10 treats this as incompleteness instead.
func scaffoldEntry() ValidationEntry {
	return ValidationEntry{
		Category: "integrity",
		Check:    "completeness",
		Message:  "spec is an empty scaffold: generate its requirements and tasks",
	}
}

// ValidateSchema validates the PRD frontmatter, the two contract artifacts
// and, when the package has one, the progress file against the bundled v3
// schemas. The EARS field constraints of §6.2.1 and the two forms of a check
// (§6.2.2) are expressed as conditionals inside requirements.v3.json, so
// there is no separate EARS check.
func (s *Spec) ValidateSchema() ValidationResult {
	if s.SchemaVersion != 0 && s.SchemaVersion != SchemaVersion {
		return ValidationResult{Valid: false, Errors: []ValidationEntry{legacyVersionEntry(s.SchemaVersion)}}
	}
	if s.IsScaffold() {
		return ValidationResult{Valid: false, Errors: []ValidationEntry{scaffoldEntry()}}
	}

	var errors []ValidationEntry

	fm := prdFrontmatterForSchema{
		SpecID:          s.SpecID,
		SpecName:        s.SpecName,
		Title:           s.Title,
		Status:          s.Status,
		CreatedAt:       s.CreatedAt,
		IntentHash:      s.IntentHash,
		SchemaVersion:   s.SchemaVersion,
		Profile:         s.Profile,
		SteeringVersion: s.SteeringVersion,
		Source:          s.Source,
		Supersedes:      s.Supersedes,
	}
	errors = append(errors, ValidateArtifactSchema(fm, PRDFrontmatterSchemaName, "prd.md")...)

	if s.Requirements != nil {
		errors = append(errors, ValidateArtifactSchema(s.Requirements, RequirementsSchemaName, RequirementsFileName)...)
	}
	if s.Tasks != nil {
		errors = append(errors, ValidateArtifactSchema(s.Tasks, TasksSchemaName, TasksFileName)...)
	}
	if s.Progress != nil {
		errors = append(errors, ValidateArtifactSchema(s.Progress, ProgressSchemaName, ProgressFileName)...)
	}
	// Keys on disk that no schema allows: the decode dropped them, so the
	// schema run above never saw them.
	for _, artifact := range []string{RequirementsFileName, TasksFileName, ProgressFileName} {
		for _, key := range s.unknown[artifact] {
			errors = append(errors, ValidationEntry{
				Category: "schema", Check: "unknown_field", Artifact: artifact,
				Message: fmt.Sprintf("%s: the key %s is not allowed by the schema (additionalProperties: false); "+
					"remove it", artifact, key),
			})
		}
	}

	return ValidationResult{Valid: len(errors) == 0, Errors: errors}
}

// ---------------------------------------------------------------------------
// ID formats (Appendix A) and language heuristics
// ---------------------------------------------------------------------------

var (
	requirementIDPattern = regexp.MustCompile(`^REQ-\d+$`)
	criterionIDPattern   = regexp.MustCompile(`^REQ-\d+\.\d+$`)
	pathIDPattern        = regexp.MustCompile(`^PATH-\d+$`)

	// criterionParentRe captures the requirement id a criterion belongs to.
	criterionParentRe = regexp.MustCompile(`^(.+)\.\d+$`)

	// errorKeywordRe spots an action that describes an error outcome; §10.2
	// warns when such a criterion carries no contract.
	errorKeywordRe = regexp.MustCompile(`(?i)\b(error|fail|reject|denied|deny|invalid|unauthorized|unauthorised|forbidden|timeout|not found)\b`)
)

// Scope limits of §6.5.4 and §7.3.3. Exceeding one is a warning. A lite
// package holds at most maxRequirementsLite requirements, which is an error
// (rule C7).
const (
	maxRequirementsPerSpec = 10
	maxCriteriaPerReq      = 8
	maxStepsPerTask        = 12
	maxTasksPerSpec        = 12
	maxRequirementsLite    = 3
)

// ---------------------------------------------------------------------------
// Spec.ValidateCrossFile — rules C1 to C8 of §10.2
// ---------------------------------------------------------------------------

// The rule functions below run over whatever artifacts the Spec carries, so
// that the generation pipeline can apply the decidable subset after each step
// (§12.2). These accessors return an empty slice for an absent artifact.

func requirementsOf(s *Spec) []Requirement {
	if s.Requirements == nil {
		return nil
	}
	return s.Requirements.Requirements
}

func executionPathsOf(s *Spec) []ExecutionPath {
	if s.Requirements == nil {
		return nil
	}
	return s.Requirements.ExecutionPaths
}

func tasksOf(s *Spec) []Task {
	if s.Tasks == nil {
		return nil
	}
	return s.Tasks.Tasks
}

// specIndex holds the entity sets the cross-file rules resolve references
// against, built once per validation run.
type specIndex struct {
	requirementIDs map[string]bool
	criterionIDs   map[string]bool
	criteriaOfReq  map[string][]string
	criterionOrder []string
	// ownCheck is true for a criterion whose check is its own, not shared.
	ownCheck  map[string]bool
	pathIDs   map[string]bool
	pathOrder []string
}

func (s *Spec) buildIndex() specIndex {
	idx := specIndex{
		requirementIDs: map[string]bool{},
		criterionIDs:   map[string]bool{},
		criteriaOfReq:  map[string][]string{},
		ownCheck:       map[string]bool{},
		pathIDs:        map[string]bool{},
	}
	if s.Requirements != nil {
		for _, r := range s.Requirements.Requirements {
			idx.requirementIDs[r.Id] = true
			for _, c := range r.Criteria {
				idx.criterionIDs[c.Id] = true
				idx.criterionOrder = append(idx.criterionOrder, c.Id)
				idx.criteriaOfReq[r.Id] = append(idx.criteriaOfReq[r.Id], c.Id)
				idx.ownCheck[c.Id] = c.Check.SameAs == nil
			}
		}
		for _, p := range s.Requirements.ExecutionPaths {
			idx.pathIDs[p.Id] = true
			idx.pathOrder = append(idx.pathOrder, p.Id)
		}
	}
	return idx
}

// ValidateCrossFile runs the cross-file integrity rules C1 to C8 of §10.2
// plus the warnings listed there. All rules are errors.
func (s *Spec) ValidateCrossFile() ValidationResult {
	if s.SchemaVersion != 0 && s.SchemaVersion != SchemaVersion {
		return ValidationResult{Valid: false, Errors: []ValidationEntry{legacyVersionEntry(s.SchemaVersion)}}
	}
	if s.IsScaffold() {
		return ValidationResult{Valid: false, Errors: []ValidationEntry{scaffoldEntry()}}
	}

	var incomplete []string
	if s.Requirements == nil {
		incomplete = append(incomplete, RequirementsFileName)
	}
	if s.Tasks == nil {
		incomplete = append(incomplete, TasksFileName)
	}
	if len(incomplete) > 0 {
		return ValidationResult{
			Valid: false,
			Errors: []ValidationEntry{{
				Category: "integrity",
				Check:    "completeness",
				Message:  fmt.Sprintf("spec is incomplete: %s", strings.Join(incomplete, ", ")),
			}},
		}
	}

	idx := s.buildIndex()

	var errors []ValidationEntry
	errors = append(errors, s.checkC1()...)
	errors = append(errors, s.checkC2()...)
	errors = append(errors, s.checkC3(idx)...)
	errors = append(errors, s.checkC4C5(idx)...)
	errors = append(errors, s.checkC6()...)
	errors = append(errors, s.checkC7()...)
	errors = append(errors, s.checkC8()...)
	errors = append(errors, s.checkAffectedTestsBase()...)

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: s.crossFileWarnings(),
	}
}

// AffectedTestsBase is the placeholder the affected-tests command holds
// exactly once, for the commit a change starts from.
const AffectedTestsBase = "{base}"

// checkAffectedTestsBase: an affected-tests command holds {base} exactly
// once. The schema cannot say "exactly once" readably, so the library does.
func (s *Spec) checkAffectedTestsBase() []ValidationEntry {
	c := s.Tasks.TestCommands.AffectedTests
	if c == nil || *c == "" {
		return nil
	}
	if n := strings.Count(*c, AffectedTestsBase); n != 1 {
		return []ValidationEntry{{Category: "integrity", Check: "affected_tests_base", Artifact: TasksFileName,
			EntityID: "test_commands.affected_tests",
			Message: fmt.Sprintf("test_commands.affected_tests is %q: found %d %s placeholder(s), and exactly one is needed, where the commit the change starts from goes",
				*c, n, AffectedTestsBase)}}
	}
	return nil
}

// The checks of the cross-spec handoff rules, and of the warning that a
// handoff is not taken.
const (
	CheckHandoffDirection = "cross_spec_handoff_direction"
	CheckHandoffTaken     = "cross_spec_handoff_taken"
	CheckHandoffNotTaken  = "handoff_not_taken"
)

// handoffRules are the cross-spec rules of handoffs. A handoff goes to a
// package of the spec root that depends on the sender, directly or
// transitively: in dependency order any other lands first, and nothing is
// left to wire it. A takes_handoffs entry names a handoff its package
// declares to the taker. Each is an error of the package that wrote it. A
// handoff no task of its receiver takes is a warning of the receiver.
func handoffRules(specs []*Spec, byID map[string]*Spec, graph *DependencyGraph) (errs, warns []ValidationEntry) {
	if graph == nil {
		graph = &DependencyGraph{}
	}
	for _, s := range specs {
		for _, h := range handoffsOf(s) {
			why := ""
			switch {
			case byID[h.To] == nil:
				why = "names no package under the spec root"
			case h.To == s.SpecID:
				why = "names the package itself"
			case !graph.DependsOn(h.To, s.SpecID):
				why = fmt.Sprintf("names spec %s, which does not depend on spec %s, directly or transitively, so it can land first and leave nothing to wire the handoff", h.To, s.SpecID)
			}
			if why != "" {
				errs = append(errs, ValidationEntry{Category: "integrity", Check: CheckHandoffDirection, Artifact: RequirementsFileName,
					EntityID: s.SpecID, Message: fmt.Sprintf("spec %s's handoff %s (%s) to %q %s", s.SpecID, h.Id, h.What, h.To, why)})
				continue
			}
			if !takes(byID[h.To], s.SpecID+":"+h.Id) {
				warns = append(warns, ValidationEntry{Category: "integrity", Check: CheckHandoffNotTaken, Artifact: TasksFileName,
					EntityID: h.To, Message: fmt.Sprintf("spec %s hands %s to this package, %s, and no task takes it: list \"%s:%s\" in the takes_handoffs of the task that wires it",
						s.SpecID, h.Id, h.What, s.SpecID, h.Id)})
			}
		}
		for _, task := range tasksOf(s) {
			for _, take := range task.TakesHandoffs {
				from, id, _ := strings.Cut(take, ":")
				why := ""
				switch sender := byID[from]; {
				case sender == nil:
					why = fmt.Sprintf("spec %s is no package under the spec root", from)
				case !slices.ContainsFunc(handoffsOf(sender), func(h Handoff) bool { return h.Id == id }):
					why = fmt.Sprintf("spec %s declares no handoff %s", from, id)
				default:
					for _, h := range handoffsOf(sender) {
						if h.Id == id && h.To != s.SpecID {
							why = fmt.Sprintf("spec %s's handoff %s is addressed to spec %s, not to this package", from, id, h.To)
						}
					}
				}
				if why != "" {
					errs = append(errs, ValidationEntry{Category: "integrity", Check: CheckHandoffTaken, Artifact: TasksFileName,
						EntityID: s.SpecID, Message: fmt.Sprintf("task %d takes %q, but %s", task.Id, take, why)})
				}
			}
		}
	}
	return errs, warns
}

// StrictHandoffs is v with each handoff_not_taken warning made an error, as
// a caller that will not start a package leaving a handoff untaken asks.
func StrictHandoffs(v ValidationResult) ValidationResult {
	out := ValidationResult{Errors: slices.Clone(v.Errors)}
	for _, w := range v.Warnings {
		if w.Check == CheckHandoffNotTaken {
			out.Errors = append(out.Errors, w)
		} else {
			out.Warnings = append(out.Warnings, w)
		}
	}
	out.Valid = len(out.Errors) == 0
	return out
}

// takes reports whether a task of the package takes the handoff ref.
func takes(s *Spec, ref string) bool {
	for _, task := range tasksOf(s) {
		if slices.Contains(task.TakesHandoffs, ref) {
			return true
		}
	}
	return false
}

// handoffsOf is the package's handoffs, none when it has no requirements.
func handoffsOf(s *Spec) []Handoff {
	if s.Requirements == nil {
		return nil
	}
	return s.Requirements.Handoffs
}

// checkC1: spec_id and spec_name agree across prd.md, the JSON files and the
// folder name.
func (s *Spec) checkC1() []ValidationEntry {
	var errors []ValidationEntry

	type artifactID struct {
		name     string
		specID   string
		specName string
	}
	var artifacts []artifactID
	if s.Requirements != nil {
		artifacts = append(artifacts, artifactID{RequirementsFileName, s.Requirements.SpecId, s.Requirements.SpecName})
	}
	if s.Tasks != nil {
		artifacts = append(artifacts, artifactID{TasksFileName, s.Tasks.SpecId, s.Tasks.SpecName})
	}
	if s.Progress != nil {
		artifacts = append(artifacts, artifactID{ProgressFileName, s.Progress.SpecId, s.Progress.SpecName})
	}
	for _, a := range artifacts {
		if a.specID != s.SpecID {
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C1", Artifact: a.name,
				Message: fmt.Sprintf("%s declares spec_id %q but prd.md declares %q", a.name, a.specID, s.SpecID),
			})
		}
		if a.specName != s.SpecName {
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C1", Artifact: a.name,
				Message: fmt.Sprintf("%s declares spec_name %q but prd.md declares %q", a.name, a.specName, s.SpecName),
			})
		}
	}

	// A spec inside a spec root lives in a {NN}_{snake_case_name} directory and
	// that name must agree with the identity. A spec loaded from anywhere else
	// — a fixture directory, a scratch copy — has no folder identity to
	// disagree with, and DiscoverSpecs would not pick it up either, so the
	// check applies only to directories that are named like spec directories.
	if s.Dir != "" {
		base := filepath.Base(s.Dir)
		if prefix, name, err := ParseSpecDirName(base); err == nil {
			if prefix != s.SpecID {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C1", Artifact: "prd.md",
					Message: fmt.Sprintf("spec directory %q has prefix %q but spec_id is %q", base, prefix, s.SpecID),
				})
			}
			if name != s.SpecName {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C1", Artifact: "prd.md",
					Message: fmt.Sprintf("spec directory %q has name %q but spec_name is %q", base, name, s.SpecName),
				})
			}
		}
	}

	return errors
}

// checkC2: every id matches its format, a criterion id is under its
// requirement, and every id is unique within its family.
func (s *Spec) checkC2() []ValidationEntry {
	var errors []ValidationEntry

	bad := func(artifact, id, msg string) ValidationEntry {
		return ValidationEntry{
			Category: "integrity", Check: "C2", Artifact: artifact,
			Message: msg, EntityID: id,
		}
	}

	seenReq := map[string]bool{}
	seenCriterion := map[string]bool{}
	for _, r := range requirementsOf(s) {
		if !requirementIDPattern.MatchString(r.Id) {
			errors = append(errors, bad(RequirementsFileName, r.Id,
				fmt.Sprintf("requirement ID %q does not match REQ-{N}", r.Id)))
		}
		if seenReq[r.Id] {
			errors = append(errors, bad(RequirementsFileName, r.Id,
				fmt.Sprintf("duplicate requirement ID %q", r.Id)))
		}
		seenReq[r.Id] = true

		for _, c := range r.Criteria {
			if !criterionIDPattern.MatchString(c.Id) {
				errors = append(errors, bad(RequirementsFileName, c.Id,
					fmt.Sprintf("criterion ID %q does not match REQ-{N}.{C}", c.Id)))
				continue
			}
			if m := criterionParentRe.FindStringSubmatch(c.Id); m != nil && m[1] != r.Id {
				errors = append(errors, bad(RequirementsFileName, c.Id,
					fmt.Sprintf("criterion ID %q is listed under requirement %q", c.Id, r.Id)))
			}
			if seenCriterion[c.Id] {
				errors = append(errors, bad(RequirementsFileName, c.Id,
					fmt.Sprintf("duplicate criterion ID %q", c.Id)))
			}
			seenCriterion[c.Id] = true
		}
	}

	seenPath := map[string]bool{}
	for _, p := range executionPathsOf(s) {
		if !pathIDPattern.MatchString(p.Id) {
			errors = append(errors, bad(RequirementsFileName, p.Id,
				fmt.Sprintf("execution path ID %q does not match PATH-{N}", p.Id)))
		}
		if seenPath[p.Id] {
			errors = append(errors, bad(RequirementsFileName, p.Id,
				fmt.Sprintf("duplicate execution path ID %q", p.Id)))
		}
		seenPath[p.Id] = true
	}

	seenTask := map[int]bool{}
	for _, task := range tasksOf(s) {
		if task.Id < 1 {
			errors = append(errors, bad(TasksFileName, fmt.Sprint(task.Id),
				fmt.Sprintf("task ID %d is not a positive integer", task.Id)))
		}
		if seenTask[task.Id] {
			errors = append(errors, bad(TasksFileName, fmt.Sprint(task.Id),
				fmt.Sprintf("duplicate task ID %d", task.Id)))
		}
		seenTask[task.Id] = true
	}

	seenHandoff := map[string]bool{}
	for _, h := range handoffsOf(s) {
		if seenHandoff[h.Id] {
			errors = append(errors, bad(RequirementsFileName, h.Id, fmt.Sprintf("duplicate handoff ID %q", h.Id)))
		}
		seenHandoff[h.Id] = true
	}
	return errors
}

// checkC3: every check.same_as names a criterion of this spec whose check is
// its own and is not the criterion itself, or a path of this spec.
func (s *Spec) checkC3(idx specIndex) []ValidationEntry {
	var errors []ValidationEntry
	for _, r := range requirementsOf(s) {
		for _, c := range r.Criteria {
			if c.Check.SameAs == nil {
				continue
			}
			ref := *c.Check.SameAs
			var msg string
			switch {
			case ref == c.Id:
				msg = fmt.Sprintf("criterion %s says its check is the same as its own", c.Id)
			case idx.criterionIDs[ref] && !idx.ownCheck[ref]:
				msg = fmt.Sprintf("criterion %s says its check is the same as %s's, whose check is itself shared; "+
					"name the criterion that carries the check", c.Id, ref)
			case idx.criterionIDs[ref], idx.pathIDs[ref]:
				continue
			default:
				msg = fmt.Sprintf("criterion %s says its check is the same as %q, which is neither a criterion nor an execution path of this spec", c.Id, ref)
			}
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C3", Artifact: RequirementsFileName, EntityID: c.Id, Message: msg,
			})
		}
	}
	return errors
}

// checkC4C5 covers the plan-side rules:
//
//	C4 every task.criteria entry resolves to a requirement or a criterion;
//	   every depends_on entry resolves to a lower task id
//	C5 every criterion is owned by at least one task
func (s *Spec) checkC4C5(idx specIndex) []ValidationEntry {
	var errors []ValidationEntry

	ownedCriteria := map[string]bool{}
	taskIDs := map[int]bool{}
	for _, h := range handoffsOf(s) {
		for _, ref := range h.Criteria {
			if !idx.criterionIDs[ref] {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C4", Artifact: RequirementsFileName, EntityID: h.Id,
					Message: fmt.Sprintf("handoff %s depends on criterion %q, which is no criterion of this spec", h.Id, ref),
				})
			}
		}
	}

	for _, task := range tasksOf(s) {
		taskIDs[task.Id] = true
	}

	for _, task := range tasksOf(s) {
		for _, ref := range task.Criteria {
			switch {
			case idx.criterionIDs[ref]:
				ownedCriteria[ref] = true
			case idx.requirementIDs[ref]:
				// A requirement ID means all of its criteria (§7.2).
				for _, cid := range idx.criteriaOfReq[ref] {
					ownedCriteria[cid] = true
				}
			default:
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C4", Artifact: TasksFileName,
					EntityID: fmt.Sprint(task.Id),
					Message: fmt.Sprintf("task %d claims criteria %q, which is neither a requirement nor a criterion of this spec",
						task.Id, ref),
				})
			}
		}

		for _, dep := range task.DependsOn {
			if !taskIDs[dep] {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C4", Artifact: TasksFileName,
					EntityID: fmt.Sprint(task.Id),
					Message:  fmt.Sprintf("task %d depends on task %d, which does not exist", task.Id, dep),
				})
				continue
			}
			// Referencing only lower IDs makes the graph acyclic by
			// construction, so no separate cycle search is needed.
			if dep >= task.Id {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C4", Artifact: TasksFileName,
					EntityID: fmt.Sprint(task.Id),
					Message:  fmt.Sprintf("task %d depends on task %d; depends_on must reference lower task IDs", task.Id, dep),
				})
			}
		}
	}

	for _, id := range idx.criterionOrder {
		if !ownedCriteria[id] {
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C5", Artifact: RequirementsFileName,
				EntityID: id,
				Message: fmt.Sprintf("criterion %s is not owned by any task; add %s, or its requirement, to the criteria "+
					"of the task that builds it", id, id),
			})
		}
	}

	return errors
}

// checkC6: every unwanted criterion has a non-empty contract.
func (s *Spec) checkC6() []ValidationEntry {
	var errors []ValidationEntry
	for _, r := range requirementsOf(s) {
		for _, c := range r.Criteria {
			if c.Pattern == CriterionPatternUnwanted && c.ContractText() == "" {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "C6", Artifact: RequirementsFileName,
					EntityID: c.Id,
					Message:  fmt.Sprintf("unwanted criterion %s has no contract", c.Id),
				})
			}
		}
	}
	return errors
}

// checkC7: a full package has at least one execution path; a lite package
// has no execution path and at most three requirements.
func (s *Spec) checkC7() []ValidationEntry {
	if s.Requirements == nil {
		return nil
	}
	var errors []ValidationEntry
	bad := func(artifact, msg string) {
		errors = append(errors, ValidationEntry{Category: "integrity", Check: "C7", Artifact: artifact, EntityID: s.SpecID, Message: msg})
	}
	paths := len(s.Requirements.ExecutionPaths)
	if !s.IsLite() {
		if paths == 0 {
			bad(RequirementsFileName, "the package has no execution path; a full package has at least one, and a package of at most three "+
				"requirements with no end-to-end path may declare profile: lite in its PRD frontmatter")
		}
		return errors
	}
	if paths > 0 {
		bad(RequirementsFileName, fmt.Sprintf("the package is lite and has %d execution path(s); a lite package has none: "+
			"remove them, or remove profile from the PRD frontmatter to make the package full", paths))
	}
	if n := len(s.Requirements.Requirements); n > maxRequirementsLite {
		bad("prd.md", fmt.Sprintf("the package is lite and has %d requirements; a lite package has at most %d: "+
			"remove profile from the PRD frontmatter to make the package full", n, maxRequirementsLite))
	}
	return errors
}

// checkC8: every entry of progress.json names a task of the plan or the
// integration task, each at most once.
func (s *Spec) checkC8() []ValidationEntry {
	if s.Progress == nil {
		return nil
	}
	var errors []ValidationEntry
	known := map[int]bool{}
	for _, t := range tasksOf(s) {
		known[t.Id] = true
	}
	if id, ok := s.IntegrationTaskID(); ok {
		known[id] = true
	}
	seen := map[int]bool{}
	for _, e := range s.Progress.Tasks {
		switch {
		case !known[e.Id]:
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C8", Artifact: ProgressFileName, EntityID: fmt.Sprint(e.Id),
				Message: fmt.Sprintf("%s records a state for task %d, which is neither a task of the plan nor the integration task", ProgressFileName, e.Id),
			})
		case seen[e.Id]:
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "C8", Artifact: ProgressFileName, EntityID: fmt.Sprint(e.Id),
				Message: fmt.Sprintf("%s records a state for task %d twice", ProgressFileName, e.Id),
			})
		}
		seen[e.Id] = true
	}
	return errors
}

// crossFileWarnings collects the never-blocking warnings of §10.2: scope
// limits, and criteria that describe an error outcome without a contract.
func (s *Spec) crossFileWarnings() []ValidationEntry {
	var warnings []ValidationEntry
	warn := func(check, artifact, entity, msg string) {
		warnings = append(warnings, ValidationEntry{
			Category: "warning", Check: check, Artifact: artifact, EntityID: entity, Message: msg,
		})
	}

	// §6.5.4 — spec and requirement scope.
	if n := len(requirementsOf(s)); n > maxRequirementsPerSpec {
		warn("scope_limit", RequirementsFileName, s.SpecID,
			fmt.Sprintf("spec has %d requirements (limit %d); consider splitting it", n, maxRequirementsPerSpec))
	}
	for _, r := range requirementsOf(s) {
		if n := len(r.Criteria); n > maxCriteriaPerReq {
			warn("scope_limit", RequirementsFileName, r.Id,
				fmt.Sprintf("requirement %s has %d criteria (limit %d); consider splitting it", r.Id, n, maxCriteriaPerReq))
		}
	}

	// The contract hint of §10.2. Vague wording is not the validator's: no
	// rule matches words (§0, rule 3); the generator and the review rubrics
	// look for it (§6.5.5).
	for _, r := range requirementsOf(s) {
		for _, c := range r.Criteria {
			if c.Pattern != CriterionPatternUnwanted && c.ContractText() == "" && errorKeywordRe.MatchString(c.Action) {
				warn("missing_contract", RequirementsFileName, c.Id,
					fmt.Sprintf("criterion %s describes an error outcome but has no contract", c.Id))
			}
		}
	}

	// §7.3.3 — task size and count.
	if n := len(tasksOf(s)); n > maxTasksPerSpec {
		warn("scope_limit", TasksFileName, s.SpecID,
			fmt.Sprintf("spec has %d tasks (limit %d); consider splitting it", n, maxTasksPerSpec))
	}
	for _, task := range tasksOf(s) {
		if n := len(task.Steps); n > maxStepsPerTask {
			warn("scope_limit", TasksFileName, fmt.Sprint(task.Id),
				fmt.Sprintf("task %d has %d steps (limit %d); consider splitting it", task.Id, n, maxStepsPerTask))
		}
	}

	return warnings
}

// Validate runs schema validation followed by the cross-file rules and
// returns the combined result.
func (s *Spec) Validate() ValidationResult {
	schemaResult := s.ValidateSchema()

	// Cross-file rules resolve IDs against artifacts the schema has already
	// rejected, so running them on a schema-invalid spec only adds noise.
	renderedErrs, renderedWarns := s.checkRendered()
	if !schemaResult.Valid {
		return ValidationResult{
			Valid:    false,
			Errors:   append(schemaResult.Errors, renderedErrs...),
			Warnings: append(schemaResult.Warnings, renderedWarns...),
		}
	}

	crossFile := s.ValidateCrossFile()
	errs := append(crossFile.Errors, renderedErrs...)
	return ValidationResult{
		Valid:    len(errs) == 0,
		Errors:   errs,
		Warnings: append(append(schemaResult.Warnings, crossFile.Warnings...), renderedWarns...),
	}
}

// checkRendered is the rule rendered: the RenderedFileName in the directory a
// package was read from must be its render. A missing file is a warning, the
// ordinary state of a package written before every save wrote one. A file that
// differs, or that is there and cannot be read, is an error while the package
// can still be saved and so rendered again, and a warning once it is sealed,
// superseded or archived: a change of the renderer must not make history
// invalid (ADR 14). A package not read from disk has no file to compare.
func (s *Spec) checkRendered() (errs, warns []ValidationEntry) {
	if !s.fromDisk || s.Dir == "" {
		return nil, nil
	}
	call := fmt.Sprintf("speca %s --render", filepath.Base(s.Dir))
	entry := func(category, msg string) ValidationEntry {
		return ValidationEntry{Category: category, Check: "rendered", Artifact: RenderedFileName, EntityID: s.SpecID, Message: msg}
	}
	got, err := os.ReadFile(filepath.Join(s.Dir, RenderedFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, []ValidationEntry{entry("warning", fmt.Sprintf("%s is missing; run %s", RenderedFileName, call))}
	}
	var msg string
	switch {
	case err != nil:
		msg = fmt.Sprintf("%s cannot be read (%v); run %s", RenderedFileName, err, call)
	default:
		line, same := firstDifferingLine(s.RenderedBytes(), got)
		if same {
			return nil, nil
		}
		msg = fmt.Sprintf("%s is not the package's render from line %d on; run %s", RenderedFileName, line, call)
	}
	if s.Status == "draft" || s.Status == "active" {
		return []ValidationEntry{entry("integrity", msg)}, nil
	}
	return nil, []ValidationEntry{entry("warning", msg)}
}

// firstDifferingLine is the 1-based number of the first line where got differs
// from want, and whether they are equal.
func firstDifferingLine(want, got []byte) (int, bool) {
	if bytes.Equal(want, got) {
		return 0, true
	}
	w, g := bytes.Split(want, []byte("\n")), bytes.Split(got, []byte("\n"))
	for i := range min(len(w), len(g)) {
		if !bytes.Equal(w[i], g[i]) {
			return i + 1, false
		}
	}
	return min(len(w), len(g)) + 1, false
}

// ---------------------------------------------------------------------------
// ValidateCrossSpec — §10.3
// ---------------------------------------------------------------------------

// ValidateCrossSpec checks the rules that span specs: every declared
// dependency exists, the dependency graph is acyclic, and each dependency edge
// between two packages that have execution paths is reflected by a shared
// actor in their paths (error).
//
// The v2 glossary comparison is gone with the glossary; the v1 external-API
// signature and return-contract comparisons were gone before it.
func ValidateCrossSpec(specs []*Spec, graph *DependencyGraph) ValidationResult {
	var errors, warnings []ValidationEntry

	specByID := make(map[string]*Spec, len(specs))
	for _, s := range specs {
		specByID[s.SpecID] = s
	}

	// Every dependencies[].spec must exist in the spec root.
	for _, s := range specs {
		if s.Tasks == nil {
			continue
		}
		for _, dep := range s.Tasks.Dependencies {
			if _, ok := specByID[dep.Spec]; !ok {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "cross_spec_unknown_dependency",
					Artifact: TasksFileName, EntityID: s.SpecID,
					Message: fmt.Sprintf("spec %s declares a dependency on spec %q, which does not exist in the spec root", s.SpecID, dep.Spec),
				})
			}
		}
	}

	moreErrs, moreWarns := handoffRules(specs, specByID, graph)
	errors, warnings = append(errors, moreErrs...), append(warnings, moreWarns...)

	if len(specs) <= 1 {
		return ValidationResult{Valid: len(errors) == 0, Errors: errors, Warnings: warnings}
	}

	// The dependency graph must be acyclic.
	if graph != nil {
		if _, err := graph.TopologicalSort(); err != nil {
			errors = append(errors, ValidationEntry{
				Category: "integrity", Check: "cross_spec_cycle",
				Message: fmt.Sprintf("the spec dependency graph is not acyclic: %s", err),
			})
		}
	}

	// Along each dependency edge between two packages that have paths, the
	// downstream spec must have at least one execution path step whose actor
	// also appears in an upstream path. A lite package has no path and is
	// held to nothing here, on either side.
	if graph != nil {
		seen := map[[2]string]bool{}
		for _, edge := range graph.Edges {
			key := [2]string{edge.FromSpec, edge.ToSpec}
			if seen[key] {
				continue
			}
			seen[key] = true

			// FromSpec depends on ToSpec, so ToSpec is upstream.
			downstream, upstream := specByID[edge.FromSpec], specByID[edge.ToSpec]
			if downstream == nil || upstream == nil ||
				downstream.Requirements == nil || upstream.Requirements == nil ||
				len(downstream.Requirements.ExecutionPaths) == 0 {
				continue
			}

			upstreamActors := map[string]bool{}
			for _, p := range upstream.Requirements.ExecutionPaths {
				for _, step := range p.Steps {
					upstreamActors[strings.ToLower(step.Actor)] = true
				}
			}
			if len(upstreamActors) == 0 {
				continue
			}

			found := false
			for _, p := range downstream.Requirements.ExecutionPaths {
				for _, step := range p.Steps {
					if upstreamActors[strings.ToLower(step.Actor)] {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				errors = append(errors, ValidationEntry{
					Category: "integrity", Check: "cross_spec_actor",
					EntityID: downstream.SpecID,
					Message: fmt.Sprintf("spec %s depends on spec %s but has no execution path step whose actor appears in spec %s",
						downstream.SpecID, upstream.SpecID, upstream.SpecID),
				})
			}
		}
	}

	return ValidationResult{Valid: len(errors) == 0, Errors: errors, Warnings: warnings}
}

// ---------------------------------------------------------------------------
// Spec.ValidateStructured
// ---------------------------------------------------------------------------

// ValidateStructured runs full validation and returns a structured map for
// CLI consumption with "valid", "errors" and, when non-empty, "warnings".
//
// Schema errors carry "artifact", "message" and optionally "path"; integrity
// errors carry "check" and "message".
func (s *Spec) ValidateStructured() map[string]any {
	result := s.Validate()

	errorMaps := make([]map[string]any, 0, len(result.Errors))
	for _, e := range result.Errors {
		var entry map[string]any
		if e.Category == "integrity" {
			entry = map[string]any{
				"category": "integrity",
				"check":    e.Check,
				"message":  e.Message,
			}
			if e.EntityID != "" {
				entry["entity_id"] = e.EntityID
			}
		} else {
			entry = map[string]any{
				"category": "schema",
				"artifact": e.Artifact,
				"message":  e.Message,
			}
			if e.Path != "" {
				entry["path"] = e.Path
			}
			if e.Value != "" {
				entry["value"] = e.Value
			}
		}
		errorMaps = append(errorMaps, entry)
	}

	output := map[string]any{
		"valid":  len(result.Errors) == 0,
		"errors": errorMaps,
	}

	if len(result.Warnings) > 0 {
		warningMaps := make([]map[string]any, 0, len(result.Warnings))
		for _, w := range result.Warnings {
			warningMaps = append(warningMaps, map[string]any{
				"category":  "warning",
				"message":   w.Message,
				"entity_id": w.EntityID,
			})
		}
		output["warnings"] = warningMaps
	}

	return output
}

// DependencyGraph is the inter-spec dependency graph built from the
// dependencies list of each spec's tasks.json.
type DependencyGraph struct {
	Edges []DependencyEdge
}

// DependencyEdge is a directed edge: FromSpec depends on ToSpec.
type DependencyEdge struct {
	FromSpec string
	ToSpec   string
	Reason   string
}
