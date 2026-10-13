package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// $schema they are written by the submit handler and are not required of the
// model: one that has to restate a constant gets it wrong often enough (a
// missing schema_version rejected the first submission in four of five scopes
// of one run), and each miss costs a regeneration of the artifact.
var programFields = []string{"spec_id", "spec_name", "schema_version"}

// optionalProgramFields takes the program-supplied fields out of a converted
// tool schema's required list, at the top level only. They stay declared:
// additionalProperties is false, so a model that still sends one out of habit
// is not rejected for it, and the submit handler overwrites whatever it sent.
func optionalProgramFields(s *schema.Schema) {
	required := s.Required[:0]
	for _, name := range s.Required {
		if !slices.Contains(programFields, name) {
			required = append(required, name)
		}
	}
	s.Required = required
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
		optionalProgramFields(s)
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
}

func (a *artifactSink) reject(reason string) {
	a.mu.Lock()
	a.rejected++
	a.last = reason
	a.mu.Unlock()
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
			// The artifact's $schema is a fact about the format rather than a
			// judgement about this spec, so it is written here rather than
			// asked for: the model is not shown the field, and what it would
			// have typed cannot be wrong.
			if uri := artifactSchemaURI(step); uri != "" {
				if content == nil {
					content = map[string]any{}
				}
				content[schemaProperty] = uri
			}
			// So are the format version and the spec's identity: the pipeline
			// knows all three, and the model's copy could only differ.
			if content == nil {
				content = map[string]any{}
			}
			content["schema_version"] = afspec.SchemaVersion
			if partial.SpecID != "" {
				content["spec_id"] = partial.SpecID
			}
			if partial.SpecName != "" {
				content["spec_name"] = partial.SpecName
			}
			if code, err := checkArtifact(content, step, partial, audit); err != nil {
				sink.reject(err.Error())
				return core.ErrResult(code, err.Error())
			}
			sink.set(content)
			res := core.OKResult(map[string]any{"accepted": true, "artifact": string(step)})
			res.Terminate = true
			return res
		},
	}
}

// validateArtifactContent decodes a model's arguments for one generation
// step, runs the artifact's v2 schema and every cross-file rule decidable at
// that point, and records the typed artifact in partial on success.
//
// A step that fails leaves partial untouched, so the next attempt validates
// against the same upstream artifacts as the one before it.
func validateArtifactContent(content map[string]any, step afspec.GenerationStep, partial *afspec.PartialSpec) error {
	_, err := checkArtifact(content, step, partial, nil)
	return err
}

// checkArtifact is every check a submission must pass, run in ONE pass: the
// schema, the cross-file rules (even when the schema fails) and the project
// audit. A rejection costs the model a full regeneration of the artifact, so
// it must name every problem at once rather than one class per attempt. The
// returned code is the tool error's: validation_failed when the format's own
// rules fail, project_mismatch when only the audit does.
func checkArtifact(content map[string]any, step afspec.GenerationStep, partial *afspec.PartialSpec,
	audit func(map[string]any) error) (string, error) {

	var schemaErrs, ruleErrs []afspec.ValidationEntry
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
		for _, e := range afspec.ValidateGenerationStepComplete(step, candidate).Errors {
			if e.Category == "schema" {
				schemaErrs = append(schemaErrs, e)
			} else {
				ruleErrs = append(ruleErrs, e)
			}
		}
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
