package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/schema"

	"github.com/agent-fox-dev/spec/afspec"
)

// ArtifactToolName is the tool that submits one generation step's artifact.
func ArtifactToolName(step afspec.GenerationStep) string { return "submit_" + string(step) }

// artifactSchemas caches the converted tool schema for each generation step.
// The conversion walks the whole document and is pure, so it runs once per
// step per process; sync.Map rather than a plain map because the cache is
// read from whatever goroutine a phase runs on.
var artifactSchemas sync.Map // afspec.GenerationStep -> *artifactSchemaEntry

type artifactSchemaEntry struct {
	once   sync.Once
	schema *schema.Schema
	uri    string
	err    error
}

// schemaProperty is the artifact's own `$schema` field: the URI of the schema
// the file is validated against.
//
// It is the one property of a v2 artifact the model is never asked for. A
// tool schema cannot even declare a property by that name — the vendors
// restrict property names to `^[a-zA-Z0-9_.-]{1,64}$`, and Anthropic rejects
// the whole request over it — and asking a model to reproduce a URL from
// memory was never the right shape anyway. The converter drops it and the
// submit handler writes it, from the schema document's own `$id`, so the
// value cannot drift from the schema it names.
const schemaProperty = "$schema"

// programFields are the other top-level fields of an artifact that the
// pipeline already knows: the format version and the spec's identity. Like
// $schema they are written by the submit handler and are not asked of the
// model: one that has to restate a constant gets it wrong often enough (a
// missing schema_version rejected the first submission in four of five scopes
// of one run), and each miss costs a regeneration of the artifact.
var programFields = []string{"spec_id", "spec_name", "schema_version"}

// dropProgramFields takes the program-supplied fields out of a converted tool
// schema, at the top level only. Declared as optional they would each be a
// union with null in the strict subset, and a request may carry only 16 of
// those across all its strict tools. A model that sends one out of habit is
// not rejected for it: the tool's PrepareArguments deletes it before the
// arguments are validated, and the submit handler writes the real value.
func dropProgramFields(s *schema.Schema) {
	for _, name := range programFields {
		delete(s.Properties, name)
	}
	s.PropertyOrder = slices.DeleteFunc(s.PropertyOrder, func(n string) bool { return slices.Contains(programFields, n) })
	s.Required = slices.DeleteFunc(s.Required, func(n string) bool { return slices.Contains(programFields, n) })
}

// dictionaryEntries names the two fields of an entry, for each top-level
// property the format declares as a dictionary object (term -> definition).
//
// A strict tool schema cannot declare a dictionary: strict mode requires
// every key of an object to be listed, and a dictionary's keys are the data.
// So the tool asks for a list of entries instead, and the submit handler turns
// the list back into the object the format stores.
var dictionaryEntries = map[string][2]string{"glossary": {"term", "definition"}}

// dictionariesAsEntries rewrites each top-level dictionary property of a
// converted tool schema into a list of entries. A dictionary with no entry
// form is an error, so a format change that adds one fails here, by name,
// rather than on every request.
func dictionariesAsEntries(s *schema.Schema, step afspec.GenerationStep) error {
	for _, name := range s.PropertyList() {
		p := s.Properties[name]
		if p == nil || p.AdditionalProperties == nil || p.AdditionalProperties.Schema == nil {
			continue
		}
		f, ok := dictionaryEntries[name]
		if !ok {
			return fmt.Errorf("specgen: the %s schema declares %q as a dictionary object, which a strict "+
				"tool schema cannot carry, and this package has no entry form for it", step, name)
		}
		desc := strings.TrimSpace(p.Description + " One entry per " + f[0] + ".")
		s.Properties[name] = schema.Array(schema.Object(
			schema.Prop(f[0], schema.String("The "+f[0]+".")),
			schema.Prop(f[1], p.AdditionalProperties.Schema),
		), desc)
	}
	return nil
}

// dictionariesAsSubmitted is the repair shim for a model that sends a
// dictionary in the form the format stores, as the artifacts it is shown do:
// the object becomes the list of entries the tool asks for, in key order.
// Under strict mode the API never lets that shape through; a tool sent
// without the flag can receive it.
func dictionariesAsSubmitted(args map[string]any) {
	for name, f := range dictionaryEntries {
		dict, ok := args[name].(map[string]any)
		if !ok {
			continue
		}
		list := make([]any, 0, len(dict))
		for _, k := range slices.Sorted(maps.Keys(dict)) {
			list = append(list, map[string]any{f[0]: k, f[1]: dict[k]})
		}
		args[name] = list
	}
}

// entriesAsDictionaries is dictionariesAsEntries in reverse, on submitted
// arguments: each list of entries becomes the object the format stores. A
// later entry for the same key wins.
func entriesAsDictionaries(content map[string]any) {
	for name, f := range dictionaryEntries {
		list, ok := content[name].([]any)
		if !ok {
			continue
		}
		dict := make(map[string]any, len(list))
		for _, e := range list {
			if m, ok := e.(map[string]any); ok {
				if k, ok := m[f[0]].(string); ok {
					dict[k] = m[f[1]]
				}
			}
		}
		content[name] = dict
	}
}

// ArtifactSchema returns the tool schema for one generation step, converted
// from the JSON Schema afspec embeds.
//
// The schema is not re-authored here. afspec/schemas/*.json is the format's
// definition and the same bytes validate the artifact after generation; a
// second, hand-written copy for the tool declaration would be a copy that can
// disagree with the thing it is supposed to describe.
func ArtifactSchema(step afspec.GenerationStep) (*schema.Schema, error) {
	v, _ := artifactSchemas.LoadOrStore(step, &artifactSchemaEntry{})
	entry := v.(*artifactSchemaEntry)
	entry.once.Do(func() {
		name, ok := afspec.SchemaNameForStep(step)
		if !ok {
			entry.err = fmt.Errorf("specgen: no schema for generation step %q", step)
			return
		}
		raw, ok := afspec.Schemas()[name]
		if !ok {
			entry.err = fmt.Errorf("specgen: schema %q is not embedded", name)
			return
		}
		s, dropped, err := ToolSchema(raw)
		if err != nil {
			entry.err = err
			return
		}
		// The only property this package can supply for the model is the
		// artifact's own $schema. Anything else the converter had to drop is
		// a field the model would be asked for by the format and never shown
		// by the tool — an unwinnable repair loop — so it is a refusal here,
		// in a message that names the property, rather than a run that fails
		// three phases later without saying why.
		for _, name := range dropped {
			if name != schemaProperty {
				entry.err = fmt.Errorf("specgen: the %s schema declares a property named %q, "+
					"which a tool schema cannot carry (the vendors require "+
					"^[a-zA-Z0-9_.-]{1,64}$) and this package has no value for", step, name)
				return
			}
		}
		uri := schemaID(raw)
		if uri == "" && len(dropped) > 0 {
			entry.err = fmt.Errorf("specgen: the %s schema has no $id, so the %s field of the "+
				"artifact cannot be filled in", step, schemaProperty)
			return
		}
		dropProgramFields(s)
		if err := dictionariesAsEntries(s, step); err != nil {
			entry.err = err
			return
		}
		if _, err := schema.StrictSubset(s); err != nil {
			entry.err = fmt.Errorf("specgen: the %s tool schema has no strict form: %w", step, err)
			return
		}
		entry.schema, entry.uri = s, uri
	})
	return entry.schema, entry.err
}

// artifactSchemaURI is the value the submit handler writes into the
// artifact's $schema field: the $id of the schema it will be validated
// against, read from the same embedded document.
func artifactSchemaURI(step afspec.GenerationStep) string {
	if _, err := ArtifactSchema(step); err != nil {
		return ""
	}
	v, _ := artifactSchemas.Load(step)
	return v.(*artifactSchemaEntry).uri
}

// schemaID reads a schema document's $id.
func schemaID(raw []byte) string {
	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return doc.ID
}

// artifactSink is the guarded destination one generation step writes into.
type artifactSink struct {
	mu      sync.Mutex
	content map[string]any
	done    bool
	// rejected counts the submissions the handler turned back, and last is
	// the most recent reason, so a phase that ends without an accepted
	// artifact can say what it was stuck on.
	rejected int
	last     string
	// held is the submission most recently turned back, which the step's
	// patch tool edits instead of the model regenerating all of it.
	held map[string]any
	// reviewed is set once the step's warnings have been sent back: they are
	// sent back once, and the next submission is accepted with them.
	reviewed bool
}

// reject records a submission turned back and holds it for the patch tool.
func (a *artifactSink) reject(content map[string]any, reason string) {
	a.mu.Lock()
	a.rejected++
	a.last = reason
	a.held = content
	a.mu.Unlock()
}

// heldSubmission is the submission most recently turned back, or nil.
func (a *artifactSink) heldSubmission() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.held
}

// reviewDue reports whether the step's warnings are still to be sent back,
// and marks them sent: the caller sends them, or there were none to send.
func (a *artifactSink) reviewDue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	due := !a.reviewed
	a.reviewed = true
	return due
}

// rejectionDetail is "" when nothing was rejected, else a sentence naming how
// many submissions failed and the first line of the last reason.
func (a *artifactSink) rejectionDetail() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rejected == 0 {
		return ""
	}
	last := a.last
	if lines := strings.Split(last, "\n"); len(lines) > 1 {
		// Keep the count line and the first problem, not the whole list.
		last = strings.TrimSpace(lines[0])
		for _, l := range lines[1:] {
			// A group heading ends with a colon; the first problem does not.
			if l = strings.TrimSpace(l); l != "" && !strings.HasSuffix(l, ":") {
				last += " " + l
				break
			}
		}
	}
	return fmt.Sprintf("%d submission(s) were rejected; the last failure: %s", a.rejected, last)
}

func (a *artifactSink) set(c map[string]any) {
	a.mu.Lock()
	a.content, a.done = c, true
	a.mu.Unlock()
}

func (a *artifactSink) get() (map[string]any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.content, a.done
}

// submitArtifactTool builds the terminating tool of one generation step.
//
// This is where the repair loop lives. The alternative — call the model, pull
// the tool_use block out of the response, validate it, and on a failure
// assemble a second conversation by hand out of the original prompt, the
// model's message and a synthetic tool_result — is the shape this replaces.
// Here the validation runs inside the handler and a violation is returned as
// a tool error. The loop appends it to the transcript the model is already
// holding and asks again, which is the same conversation the hand-assembled
// one was imitating, with two differences that matter: the system prompt and
// the tool schemas are byte-identical across the attempt and its correction,
// so the provider's cache prefix survives; and "how many attempts does the
// model get" and "how many turns may this phase take" became one number.
func submitArtifactTool(step afspec.GenerationStep, s *schema.Schema,
	partial *afspec.PartialSpec, sink *artifactSink, audit func(map[string]any) error) core.Tool {

	name := ArtifactToolName(step)
	return core.Tool{
		Name: name,
		Description: fmt.Sprintf("Submit the complete %s artifact and end this phase. "+
			"It is validated against the format v2 schema and every cross-file rule "+
			"decidable at this point; a violation comes back to you as an error to fix.", step),
		InputSchema: s,
		Terminating: true,
		// The tool schema cannot declare $schema and forbids undeclared
		// properties, yet the artifacts already generated reach the model
		// with the field in them, so it may copy it. It is dropped before
		// validation rather than refused: the handler writes it anyway.
		PrepareArguments: func(args map[string]any) map[string]any {
			delete(args, schemaProperty)
			for _, name := range programFields {
				delete(args, name)
			}
			dictionariesAsSubmitted(args)
			return args
		},
		PromptGuidelines: []string{
			"Submit the " + string(step) + " artifact by calling " + name + "; do not write JSON as prose.",
		},
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var content map[string]any
			if err := json.Unmarshal(in, &content); err != nil {
				return core.ErrResult("invalid_json", fmt.Sprintf(
					"the %s arguments are not a JSON object: %v", step, err))
			}
			if content == nil {
				content = map[string]any{}
			}
			entriesAsDictionaries(content)
			return settleArtifact(content, step, partial, sink, audit)
		},
	}
}

// settleArtifact accepts a submission, from the submit tool or the patch
// tool, or turns it back with every problem it has.
//
// A submission that passes every rule is turned back once more when the
// validator has warnings on it — vague wording, a missing contract, a scope
// past the format's limits — because a warning nobody reads is the same
// wording in the finished spec. The model fixes what it agrees with, or
// keeps the artifact as it is; either way the next submission is accepted.
func settleArtifact(content map[string]any, step afspec.GenerationStep, partial *afspec.PartialSpec,
	sink *artifactSink, audit func(map[string]any) error) core.ToolResult {

	// The artifact's $schema is a fact about the format rather than a
	// judgement about this spec, so it is written here rather than asked
	// for: the model is not shown the field, and what it would have typed
	// cannot be wrong.
	if uri := artifactSchemaURI(step); uri != "" {
		content[schemaProperty] = uri
	}
	// So are the format version and the spec's identity: the pipeline knows
	// all three, and the model's copy could only differ.
	content["schema_version"] = afspec.SchemaVersion
	if partial.SpecID != "" {
		content["spec_id"] = partial.SpecID
	}
	if partial.SpecName != "" {
		content["spec_name"] = partial.SpecName
	}
	code, err := checkArtifact(content, step, partial, audit, sink.reviewDue())
	if err != nil {
		sink.reject(content, err.Error())
		msg := err.Error()
		if code != codeReviewWarnings {
			msg += fmt.Sprintf("\n\nTo correct it, call %s with operations on just the fields that fail, "+
				"or call %s with the whole artifact again.", PatchToolName(step), ArtifactToolName(step))
		}
		return core.ErrResult(code, msg)
	}
	sink.set(content)
	res := core.OKResult(map[string]any{"accepted": true, "artifact": string(step)})
	res.Terminate = true
	return res
}

// codeReviewWarnings is the tool error of a submission that passes every rule
// and is sent back once for its warnings.
const codeReviewWarnings = "review_warnings"

// reviewedChecks are the warnings sent back to the step whose artifact they
// are on. Each is something the model can fix in place.
var reviewedChecks = []string{"vague_language", "missing_contract", "scope_limit"}

// maxReviewedWarnings bounds the list sent back: past it, the model is told
// how many more there are.
const maxReviewedWarnings = 25

// reviewedWarnings are the warnings on step's own artifact that the model is
// asked to review.
func reviewedWarnings(all []afspec.ValidationEntry, step afspec.GenerationStep) []afspec.ValidationEntry {
	file := afspec.ArtifactFileName(step)
	var out []afspec.ValidationEntry
	for _, w := range all {
		if w.Artifact == file && slices.Contains(reviewedChecks, w.Check) {
			out = append(out, w)
		}
	}
	return out
}

func reviewMessage(step afspec.GenerationStep, warnings []afspec.ValidationEntry) error {
	var b strings.Builder
	fmt.Fprintf(&b, "the %s artifact passes every rule, and the validator has %d warning(s) on it: "+
		"wording or sizing that a reader of the spec, or the coder implementing it, would trip over.\n",
		step, len(warnings))
	shown := warnings
	if len(shown) > maxReviewedWarnings {
		shown = shown[:maxReviewedWarnings]
	}
	b.WriteString(afspec.FormatValidationEntries(shown))
	if n := len(warnings) - len(shown); n > 0 {
		fmt.Fprintf(&b, "- and %d more of the same kinds\n", n)
	}
	fmt.Fprintf(&b, "Fix the ones worth fixing: a vague word becomes the observable behaviour it stands for. "+
		"Use %s to change just those fields, or %s for the whole artifact. To keep the artifact as it is, "+
		"call %s with no operations. Your next submission is accepted with whatever warnings remain.",
		PatchToolName(step), ArtifactToolName(step), PatchToolName(step))
	return errors.New(b.String())
}

// validateArtifactContent decodes a model's arguments for one generation
// step, runs the artifact's v2 schema and every cross-file rule decidable at
// that point, and records the typed artifact in partial on success.
//
// A step that fails leaves partial untouched, so the next attempt validates
// against the same upstream artifacts as the one before it.
func validateArtifactContent(content map[string]any, step afspec.GenerationStep, partial *afspec.PartialSpec) error {
	_, err := checkArtifact(content, step, partial, nil, false)
	return err
}

// checkArtifact is every check a submission must pass, run in ONE pass: the
// schema, the cross-file rules (even when the schema fails) and the project
// audit. A rejection costs the model a full regeneration of the artifact, so
// it must name every problem at once rather than one class per attempt. The
// returned code is the tool error's: validation_failed when the format's own
// rules fail, project_mismatch when only the audit does.
func checkArtifact(content map[string]any, step afspec.GenerationStep, partial *afspec.PartialSpec,
	audit func(map[string]any) error, review bool) (string, error) {

	var schemaErrs, ruleErrs, warnings []afspec.ValidationEntry
	var decodeErr error
	candidate := *partial
	decoded, err := afspec.DecodeArtifact(step, content)
	if err != nil {
		decodeErr = err
	} else {
		// Validate a COPY of the accumulated state, so a rejected artifact
		// cannot be left behind for the next attempt to validate against.
		switch artifact := decoded.(type) {
		case *afspec.RequirementsV2Json:
			candidate.Requirements = artifact
		case *afspec.TestSpecV2Json:
			candidate.TestSpec = artifact
		case *afspec.TasksV2Json:
			candidate.Tasks = artifact
		}
		result := afspec.ValidateGenerationStepComplete(step, candidate)
		for _, e := range result.Errors {
			if e.Category == "schema" {
				schemaErrs = append(schemaErrs, e)
			} else {
				ruleErrs = append(ruleErrs, e)
			}
		}
		warnings = reviewedWarnings(result.Warnings, step)
	}
	var auditErr error
	if audit != nil {
		auditErr = audit(content)
	}

	problems := len(schemaErrs) + len(ruleErrs)
	if decodeErr != nil {
		problems++
	}
	if auditErr != nil {
		problems++
	}
	if problems == 0 {
		if review && len(warnings) > 0 {
			return codeReviewWarnings, reviewMessage(step, warnings)
		}
		*partial = candidate
		return "", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "the %s artifact has %d problem(s); fix every one of them before you submit again.\n", step, problems)
	if decodeErr != nil {
		fmt.Fprintf(&b, "- %v\n", decodeErr)
	}
	b.WriteString(afspec.FormatValidationEntries(schemaErrs))
	if len(ruleErrs) > 0 {
		if len(schemaErrs) > 0 {
			b.WriteString("Cross-file rules that fail as well (one may follow from a schema error above):\n")
		}
		b.WriteString(afspec.FormatValidationEntries(ruleErrs))
	}
	if auditErr != nil {
		if problems > 1 {
			b.WriteString("The project's own tooling:\n")
		}
		b.WriteString("- " + strings.ReplaceAll(strings.TrimSpace(auditErr.Error()), "\n", "\n  ") + "\n")
	}
	code := "validation_failed"
	if problems == 1 && auditErr != nil {
		code = "project_mismatch"
	}
	return code, errors.New(strings.TrimRight(b.String(), "\n"))
}
