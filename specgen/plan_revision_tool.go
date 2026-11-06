package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/schema"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
)

// ToolSubmitPlanRevision is the terminating tool of the plan revision phase.
const ToolSubmitPlanRevision = "submit_plan_revision"

// planArtifacts are the plan's two artifacts by the argument that carries
// each, in the order they are generated.
var planArtifacts = []struct {
	arg  string
	step afspec.GenerationStep
}{
	{"requirements", afspec.StepRequirements},
	{"tasks", afspec.StepTasks},
}

// planRevisionSink is the guarded destination the plan revision tool writes
// into: the revised plan and the answers that produced it.
type planRevisionSink struct {
	mu           sync.Mutex
	plan         afspec.PartialSpec
	dispositions []Disposition
	done         bool
}

func (s *planRevisionSink) set(plan afspec.PartialSpec, dispositions []Disposition) {
	s.mu.Lock()
	s.plan, s.dispositions, s.done = plan, dispositions, true
	s.mu.Unlock()
}

func (s *planRevisionSink) get() (afspec.PartialSpec, []Disposition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan, s.dispositions, s.done
}

// planRevisionSchema is the plan revision's contract: the two artifacts,
// whole, and an answer to every finding. It has no field for the PRD.
func planRevisionSchema() *schema.Schema {
	props := []schema.Field{}
	for _, a := range planArtifacts {
		s, err := ArtifactSchema(a.step)
		if err != nil {
			panic(err) // the embedded schemas are fixed at build time
		}
		props = append(props, schema.Prop(a.arg, s.Describe(fmt.Sprintf(
			"The complete revised %s, every entry included, the ones you did not change too.", afspec.ArtifactFileName(a.step)))))
	}
	props = append(props, schema.Prop("dispositions", dispositionsSchema()))
	return schema.Object(props...)
}

// newPlanRevisionTool is the terminator of the revision of current, the plan
// of the PRD prd, answering findings, the findings of this round. It takes
// the two artifacts whole and turns back a revision that changes what a plan
// revision may not — the PRD, the spec's identity, the actors of the
// execution paths, which the later scopes of a split were handed — or whose
// plan breaks the format's own rules, so the phase asks again until it
// passes. The answers are checked as a PRD revision's are.
func newPlanRevisionTool(sink *planRevisionSink, prd PRD, current *afspec.PartialSpec, findings []Finding, ws *workspace.Workspace) core.Tool {
	// identity holds the changes to a spec id or name that PrepareArguments
	// took out of a submission so its schema check could pass; Execute turns
	// the submission back for them. A phase submits one call at a time.
	var identity []string
	return core.Tool{
		Name: ToolSubmitPlanRevision,
		Description: "Submit the revised plan — requirements and tasks, each whole — and your " +
			"answer to every finding, and end this phase. Call this once, after you have weighed every finding.",
		InputSchema: planRevisionSchema(),
		Terminating: true,
		// The artifacts reach the reviser with their $schema, version and
		// identity in them, which the artifact schemas do not declare, so it
		// may copy them: they are taken out before the schema check, as the
		// artifact tool takes them out, and a changed id or name is kept to
		// refuse.
		PrepareArguments: func(args map[string]any) map[string]any {
			identity = nil
			for _, a := range planArtifacts {
				m, ok := args[a.arg].(map[string]any)
				if !ok {
					continue
				}
				identity = append(identity, identityChanges(m, a.step, current)...)
				delete(m, schemaProperty)
				for _, name := range programFields {
					delete(m, name)
				}
				dictionariesAsSubmitted(m)
			}
			return args
		},
		PromptGuidelines: []string{
			"Submit the revision by calling " + ToolSubmitPlanRevision + "; do not write it as prose.",
			"Answer every finding exactly once, fixed, contested, escalated or declined, with the fields that answer needs.",
		},
		Execute: func(_ context.Context, in json.RawMessage) core.ToolResult {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(in, &raw); err != nil {
				return core.ErrResult("invalid_arguments", err.Error())
			}
			problems := slices.Clone(identity)
			identity = nil
			for key := range raw {
				if key == "dispositions" || slices.ContainsFunc(planArtifacts, func(a struct {
					arg  string
					step afspec.GenerationStep
				}) bool {
					return a.arg == key
				}) {
					continue
				}
				problems = append(problems, fmt.Sprintf("the revision carries %q, and a plan revision changes the plan "+
					"alone: the PRD and everything outside the two artifacts are not its to change", key))
			}
			sort.Strings(problems)

			revised := *current
			for _, a := range planArtifacts {
				var content map[string]any
				if err := json.Unmarshal(raw[a.arg], &content); err != nil || content == nil {
					problems = append(problems, fmt.Sprintf("%s is missing or not an object: submit both artifacts whole", a.arg))
					continue
				}
				entriesAsDictionaries(content)
				if a.step == afspec.StepTasks {
					omitDefaultDependencies(content)
				}
				problems = append(problems, identityChanges(content, a.step, current)...)
				content["spec_id"], content["spec_name"] = current.SpecID, current.SpecName
				if uri := artifactSchemaURI(a.step); uri != "" {
					content[schemaProperty] = uri
				}
				content["schema_version"] = afspec.SchemaVersion
				decoded, err := afspec.DecodeArtifact(a.step, content)
				if err != nil {
					problems = append(problems, err.Error())
					continue
				}
				switch v := decoded.(type) {
				case *afspec.RequirementsV3Json:
					revised.Requirements = v
				case *afspec.TasksV3Json:
					revised.Tasks = v
				}
			}
			var answers []Disposition
			if err := json.Unmarshal(raw["dispositions"], &answers); err != nil {
				problems = append(problems, "dispositions is not a list of answers: "+err.Error())
			}
			problems = append(problems, frozenChanges(current, &revised)...)
			if len(problems) > 0 {
				return refusePlan(problems)
			}
			if v := afspec.ValidateGenerationStepComplete(afspec.StepTasks, revised); !v.Valid {
				return refusePlan([]string{"the revised plan breaks the format's rules, each to fix:\n" + ruleLines(v.Errors)})
			}

			normalizeAnswers(answers)
			rendered := renderPlan(&revised)
			if bad, ok := checkDispositions(findings, answers, evidenceSources{prd.Body + "\n" + renderPlan(current), "", ws}, rendered); !ok {
				return bad
			}
			fixes := slices.ContainsFunc(answers, func(d Disposition) bool { return d.Disposition == envelope.DispositionFixed })
			if fixes && reflect.DeepEqual(planJSON(current), planJSON(&revised)) {
				return core.ErrResult("unchanged_plan", "the revision answers findings fixed, and left the plan as it was: "+
					"answering a finding fixed means changing the plan to remedy it; change it, or answer the finding another way.")
			}
			sink.set(revised, answers)
			res := core.OKResult(map[string]any{"accepted": true})
			res.Terminate = true
			return res
		},
	}
}

// ruleLines names every broken rule with the artifact and the entity it is
// about, one to a line.
func ruleLines(entries []afspec.ValidationEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "- [%s] %s", e.Check, e.Artifact)
		if e.EntityID != "" {
			fmt.Fprintf(&b, " %s", e.EntityID)
		}
		if e.Path != "" {
			fmt.Fprintf(&b, " at %s", e.Path)
		}
		fmt.Fprintf(&b, ": %s\n", e.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}

// identityChanges names a spec id or name an artifact carries that is not
// the plan's own.
func identityChanges(content map[string]any, step afspec.GenerationStep, current *afspec.PartialSpec) []string {
	var out []string
	for _, id := range []struct{ key, want string }{{"spec_id", current.SpecID}, {"spec_name", current.SpecName}} {
		if v, ok := content[id.key]; ok && v != id.want {
			out = append(out, fmt.Sprintf("it changes %s in %s from %q to %v", id.key, afspec.ArtifactFileName(step), id.want, v))
		}
	}
	return out
}

// refusePlan turns a plan revision back with every problem it has.
func refusePlan(problems []string) core.ToolResult {
	return core.ErrResult("invalid_plan_revision", "the revision was not accepted: "+strings.Join(problems, "; ")+
		". Fix every one of them and submit both artifacts again.")
}

// frozenChanges names what a plan revision changed that it may not: the
// actors of the execution paths, compared by name ignoring case so that
// reordering the paths or a path's steps is no change.
func frozenChanges(current, revised *afspec.PartialSpec) []string {
	var out []string
	added, removed := actorDiff(actorsOf(current.Requirements), actorsOf(revised.Requirements))
	if len(added)+len(removed) > 0 {
		out = append(out, fmt.Sprintf("it changes the actors of the execution paths (added: %s; removed: %s), which "+
			"the later scopes of a split were handed", listOrNone(added), listOrNone(removed)))
	}
	return out
}

// actorDiff is the actors of after not in before and of before not in after,
// by name ignoring case.
func actorDiff(before, after []string) (added, removed []string) {
	has := func(list []string, a string) bool {
		return slices.ContainsFunc(list, func(b string) bool { return strings.EqualFold(a, b) })
	}
	for _, a := range after {
		if !has(before, a) && !has(added, a) {
			added = append(added, a)
		}
	}
	for _, b := range before {
		if !has(after, b) && !has(removed, b) {
			removed = append(removed, b)
		}
	}
	return added, removed
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// planJSON is the plan as it would be written, to tell a changed plan from an
// unchanged one.
func planJSON(p *afspec.PartialSpec) string {
	b, _ := json.Marshal([]any{p.Requirements, p.Tasks})
	return string(b)
}
