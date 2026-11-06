package specgen

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-fox-dev/agentkit-go/core"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
)

// ToolSubmitPlanReview is the terminating tool of the plan review phase.
const ToolSubmitPlanReview = "submit_plan_review"

// planRubrics are the standards a finding of the plan review applies.
var planRubrics = []string{envelope.RubricZeroDecision, envelope.RubricGrounding, envelope.RubricSteering}

// planReviewRequest is what a plan review is sent: the run context of a PRD
// review, whose input it never shows, and the plan and its grounding facts.
type planReviewRequest struct {
	reviewRequest
	// Plan is the plan under review: requirements, test spec and tasks.
	Plan *afspec.PartialSpec
	// Facts are the grounding facts, computed before the first round.
	Facts []groundingFact
}

// planRevisionRequest is what a plan revision is sent: what the review was
// sent, and the round's findings.
type planRevisionRequest struct {
	planReviewRequest
	Findings []Finding
}

// newPlanReviewTool is the terminator of round round's plan review. A
// finding is held to the PRD review's rules and to two of the plan's own: its
// section names a place in the plan, and a grounding finding about a missing
// path names a path the grounding facts list.
func newPlanReviewTool(sink *reviewSink, round int, earlier []Finding, plan *afspec.PartialSpec, facts []groundingFact, steering string) core.Tool {
	places := placesOf(plan)
	listed := map[string]bool{}
	for _, f := range facts {
		listed[f.Path] = true
	}
	return findingsTool(findingsContract{
		name: ToolSubmitPlanReview,
		description: "Submit the flaws you found in the plan and end this phase. Call this once, after you " +
			"have checked the plan against the PRD, the grounding facts and the code.",
		schema: reviewSchema(planRubrics, "The place in the plan by id: a task by its number ('task 3'), a test id, "+
			"a criterion id or an execution path id"),
		rubrics:  planRubrics,
		steering: steering,
		extra: func(f *Finding, pos int) []string {
			var problems []string
			if f.Section != "" && !places.holds(f.Section) {
				problems = append(problems, fmt.Sprintf("finding %d has section %q, which names no place in this plan: "+
					"name a task by its number, a test id, a criterion id or an execution path id of it", pos, f.Section))
			}
			if f.Rubric == envelope.RubricGrounding && saysMissing(f) {
				if paths := pathsNamed(f.Flaw + " " + f.Evidence); !slices.ContainsFunc(paths, func(p string) bool { return listed[p] }) {
					named := "no path"
					if len(paths) > 0 {
						named = paths[0]
					}
					problems = append(problems, fmt.Sprintf("finding %d says a path does not exist and names %s, which the "+
						"grounding facts do not list: the evidence of such a finding is the grounding fact for a path "+
						"the plan's tasks name", pos, named))
				}
			}
			return problems
		},
	}, sink, round, earlier)
}

// planPlaces are the ids a plan finding's section may name.
type planPlaces struct {
	ids   map[string]bool
	tasks map[int]bool
}

func placesOf(plan *afspec.PartialSpec) planPlaces {
	p := planPlaces{ids: map[string]bool{}, tasks: map[int]bool{}}
	if plan == nil {
		return p
	}
	if plan.Requirements != nil {
		for _, r := range plan.Requirements.Requirements {
			for _, c := range r.Criteria {
				p.ids[c.Id] = true
			}
		}
		for _, path := range plan.Requirements.ExecutionPaths {
			p.ids[path.Id] = true
		}
	}
	if plan.TestSpec != nil {
		for _, t := range plan.TestSpec.Tests {
			p.ids[t.Id] = true
		}
	}
	if plan.Tasks != nil {
		for _, t := range plan.Tasks.Tasks {
			p.tasks[t.Id] = true
		}
	}
	return p
}

var taskSection = regexp.MustCompile(`(?i)^task\s+(\d+)\b`)

// holds reports whether section names a place in the plan: a task by its
// number, or, as its first word, a test, criterion or execution path id.
func (p planPlaces) holds(section string) bool {
	if m := taskSection.FindStringSubmatch(section); m != nil {
		n, _ := strconv.Atoi(m[1])
		return p.tasks[n]
	}
	first := strings.FieldsFunc(section, func(r rune) bool { return r == ' ' || r == ':' || r == ',' || r == '(' })
	return len(first) > 0 && p.ids[first[0]]
}

// saysMissing reports whether a finding says a path does not exist.
func saysMissing(f *Finding) bool {
	text := strings.ToLower(f.Flaw + " " + f.Evidence)
	return strings.Contains(text, "does not exist") || strings.Contains(text, "doesn't exist") ||
		strings.Contains(text, "not exist")
}

var pathWord = regexp.MustCompile("`([^`\\s]+)`|([\\w.\\-]+/[\\w./\\-]+|[\\w\\-]+\\.[a-z]{1,5})")

// pathsNamed are the paths a text names: in backticks, or words holding a
// slash or a file extension.
func pathsNamed(text string) []string {
	var out []string
	for _, m := range pathWord.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if p == "" {
			p = m[2]
		}
		p = strings.TrimRight(p, ".,;:")
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}
