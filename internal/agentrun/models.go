package agentrun

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/agentkit-go/catalog"
	"github.com/agent-fox-dev/agentkit-go/core"
)

// ModelTier classifies models by capability level. A tier is what a spec
// author actually chooses between — cheap, balanced, best — and keeping it is
// the point: the alternative is asking whoever runs spec to know which model
// id is current this month.
type ModelTier string

const (
	// TierSimple represents lightweight, cost-effective models.
	TierSimple ModelTier = "SIMPLE"
	// TierStandard represents balanced capability models.
	TierStandard ModelTier = "STANDARD"
	// TierAdvanced represents the most capable models.
	TierAdvanced ModelTier = "ADVANCED"
)

// Tiers is the declaration order of the tiers, cheapest first.
var Tiers = []ModelTier{TierSimple, TierStandard, TierAdvanced}

// The phases of a run a model and an effort can be chosen for.
const (
	PhasePRD = "prd"
	// PhasePRDReview and PhasePRDRevision are the adversarial review of a
	// PRD and its answer (--adversarial). No tier lists them, so they run on
	// the tier's base entry: the PRD phase's model and effort.
	PhasePRDReview    = "prd_review"
	PhasePRDRevision  = "prd_revision"
	PhaseRequirements = "requirements"
	PhaseTestSpec     = "test_spec"
	PhaseTasks        = "tasks"
	PhaseArchitecture = "architecture"
)

// Phases are the phases a model can be chosen for, in the order a package is
// built.
var Phases = []string{PhasePRD, PhasePRDReview, PhasePRDRevision, PhaseRequirements, PhaseTestSpec, PhaseTasks, PhaseArchitecture}

// tierEntry pairs a catalog model id with the effort the tier prescribes.
// Separating model from effort lets two tiers share a model while running it
// at different depth.
type tierEntry struct {
	id     string
	effort core.Effort
}

// tier is what a tier name prescribes: a model and effort for every phase,
// and for some phases another one.
type tier struct {
	base   tierEntry
	phases map[string]tierEntry
}

func (t tier) entry(phase string) tierEntry {
	if e, ok := t.phases[phase]; ok {
		return e
	}
	return t.base
}

// tierTable maps a tier to catalog model ids and efforts. It is an alias
// table over AgentKit's catalog, not a registry: the catalog supplies the
// context window, output cap, prices and which efforts the model takes.
//
// The PRD and the requirements are where a spec's judgment is made: what the
// work is, how it splits, what each criterion demands. The test spec, the
// tasks and the architecture turn those into structure the validator checks,
// so a tier runs them at a lower effort, or, for ADVANCED, on the faster model:
// every turn of a phase pays for its effort, the turns that only read a file
// included.
var tierTable = map[ModelTier]tier{
	TierSimple: {
		base: tierEntry{"claude-sonnet-5-5", core.EffortMedium},
		phases: map[string]tierEntry{
			PhaseTestSpec:     {"claude-sonnet-5-5", core.EffortLow},
			PhaseTasks:        {"claude-sonnet-5-5", core.EffortLow},
			PhaseArchitecture: {"claude-sonnet-5-5", core.EffortLow},
		},
	},
	TierStandard: {
		base: tierEntry{"claude-sonnet-5-5", core.EffortHigh},
		phases: map[string]tierEntry{
			PhaseTestSpec:     {"claude-sonnet-5-5", core.EffortMedium},
			PhaseTasks:        {"claude-sonnet-5-5", core.EffortMedium},
			PhaseArchitecture: {"claude-sonnet-5-5", core.EffortMedium},
		},
	},
	TierAdvanced: {
		base: tierEntry{"claude-opus-5-5", core.EffortXHigh},
		phases: map[string]tierEntry{
			PhaseRequirements: {"claude-opus-5-5", core.EffortHigh},
			PhaseTestSpec:     {"claude-sonnet-5-5", core.EffortHigh},
			PhaseTasks:        {"claude-sonnet-5-5", core.EffortHigh},
			PhaseArchitecture: {"claude-sonnet-5-5", core.EffortHigh},
		},
	},
}

func lookupTier(spec string) (tier, bool) {
	t, ok := tierTable[ModelTier(strings.ToUpper(strings.TrimSpace(spec)))]
	return t, ok
}

// EffortOff is the --effort value that sends no effort at all, leaving the
// model's own default.
const EffortOff = "off"

// Efforts are the efforts a model can be asked for, lowest first.
var Efforts = []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh, core.EffortXHigh, core.EffortMax}

// EffortValues are the accepted --effort values: off, then Efforts.
func EffortValues() []string {
	out := []string{EffortOff}
	for _, e := range Efforts {
		out = append(out, string(e))
	}
	return out
}

// ParseEffort reads an --effort value, case-insensitively. "off" is the
// empty Effort, which sends no effort.
func ParseEffort(raw string) (core.Effort, error) {
	norm := strings.ToLower(strings.TrimSpace(raw))
	if norm == EffortOff {
		return "", nil
	}
	for _, e := range Efforts {
		if string(e) == norm {
			return e, nil
		}
	}
	return "", fmt.Errorf("invalid effort %q; accepted values: %s", raw, strings.Join(EffortValues(), ", "))
}

// Model is a resolved model: the catalog row and the effort the run asks for.
type Model struct {
	// Spec is what was asked for: a tier name or a model id.
	Spec string
	// Row is the catalog row.
	Row core.Model
	// Effort is the effort the run sends; empty sends none.
	Effort core.Effort
	// Clamped is set when the asked-for effort was lowered to one the model
	// takes: the effort asked for, for the warning.
	Clamped core.Effort
}

// ID is the catalog id of the model.
func (m Model) ID() string { return m.Row.ID }

// String is the model and its effort, as "id/effort", or "id" with none.
func (m Model) String() string {
	if m.Effort == "" {
		return m.ID()
	}
	return m.ID() + "/" + string(m.Effort)
}

// ResolveModel resolves a tier name or a model id to its catalog row and an
// effort: the run's default, the model a phase runs on when nothing chooses
// another for it. ResolvePlan resolves every phase.
//
// A tier name (case-insensitive) carries the tier's base model and effort.
// Anything else must be a model the catalog lists, with or without an
// "anthropic/" prefix: an unlisted id would run with no price, so no budget
// could ever stop it. effort, when set (explicit is true), replaces the
// tier's; an empty explicit effort sends none. An effort the model does not
// take is lowered to the highest one it does, and Clamped records what was
// asked for; a model that takes no effort at or below it gets none.
func ResolveModel(spec string, effort core.Effort, explicit bool) (Model, error) {
	return resolvePhase(spec, "", effort, explicit)
}

// resolvePhase resolves spec for one phase: a tier's entry for that phase
// (its base for phase ""), or a model id.
func resolvePhase(spec, phase string, effort core.Effort, explicit bool) (Model, error) {
	if strings.TrimSpace(spec) == "" {
		return Model{}, newError("", CategoryModel, nil, "the model name must not be empty")
	}
	id := spec
	var want core.Effort
	if t, ok := lookupTier(spec); ok {
		e := t.base
		if phase != "" {
			e = t.entry(phase)
		}
		id, want = e.id, e.effort
	}
	row, ok := catalog.Lookup(id)
	if !ok {
		return Model{}, newError("", CategoryModel, nil,
			"model %q is neither a tier (%s) nor a model in the catalog", spec, tierNames())
	}
	if explicit {
		want = effort
	}
	m := Model{Spec: spec, Row: row}
	if want == "" {
		return m, nil
	}
	got := clampEffort(row, want)
	m.Effort = got
	if got != want && explicit {
		m.Clamped = want
	}
	return m, nil
}

// Plan is the model and effort of every phase of a run.
type Plan struct {
	// Default is what --model resolved to.
	Default Model
	// Phases holds every phase of Phases, resolved.
	Phases map[string]Model
}

// For is the model the phase runs on: its own, or the default.
func (p Plan) For(phase string) Model {
	if m, ok := p.Phases[phase]; ok {
		return m
	}
	return p.Default
}

// PhaseChoice is one --phase-model value: PHASE=MODEL[:EFFORT].
type PhaseChoice struct {
	Phase string
	// Model is a tier name or a catalog model id.
	Model string
	// Effort is the effort asked for; HasEffort says one was, "off" included.
	Effort    core.Effort
	HasEffort bool
}

// ParsePhaseChoice reads PHASE=MODEL[:EFFORT]. The effort suffix is split off
// only when it names an effort, so a model id is never cut in two.
func ParsePhaseChoice(raw string) (PhaseChoice, error) {
	phase, model, ok := strings.Cut(strings.TrimSpace(raw), "=")
	phase = strings.ToLower(strings.TrimSpace(phase))
	if !ok || phase == "" {
		return PhaseChoice{}, fmt.Errorf("%q is not PHASE=MODEL[:EFFORT]", raw)
	}
	if !slices.Contains(Phases, phase) {
		return PhaseChoice{}, fmt.Errorf("%q names no phase; the phases are %s", raw, strings.Join(Phases, ", "))
	}
	c := PhaseChoice{Phase: phase, Model: strings.TrimSpace(model)}
	if i := strings.LastIndex(c.Model, ":"); i >= 0 {
		if e, err := ParseEffort(c.Model[i+1:]); err == nil {
			c.Model, c.Effort, c.HasEffort = strings.TrimSpace(c.Model[:i]), e, true
		}
	}
	if c.Model == "" {
		return PhaseChoice{}, fmt.Errorf("%q names no model", raw)
	}
	return c, nil
}

// ResolvePlan resolves the model and effort of every phase.
//
// spec (a tier or a model id) decides every phase: a tier through its entry
// for that phase, a model id the same for all. effort, when explicit, replaces
// every phase's. A PhaseChoice then decides its phase alone: its model (again
// a tier or an id), at its own effort when it names one, else at the explicit
// effort, else at its tier's. A later choice for the same phase wins.
func ResolvePlan(spec string, effort core.Effort, explicit bool, choices []PhaseChoice) (Plan, error) {
	def, err := ResolveModel(spec, effort, explicit)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Default: def, Phases: make(map[string]Model, len(Phases))}
	for _, phase := range Phases {
		m, err := resolvePhase(spec, phase, effort, explicit)
		if err != nil {
			return Plan{}, err
		}
		plan.Phases[phase] = m
	}
	for _, c := range choices {
		e, ex := effort, explicit
		if c.HasEffort {
			e, ex = c.Effort, true
		}
		m, err := resolvePhase(c.Model, c.Phase, e, ex)
		if err != nil {
			return Plan{}, fmt.Errorf("--phase-model %s: %w", c.Phase, err)
		}
		plan.Phases[c.Phase] = m
	}
	return plan, nil
}

// clampEffort is the highest effort at or below want that the model takes,
// or "" when it takes none. A row with no effort map takes every effort by
// name.
func clampEffort(row core.Model, want core.Effort) core.Effort {
	if row.Efforts == nil {
		return want
	}
	i := slices.Index(Efforts, want)
	for ; i >= 0; i-- {
		if v, ok := row.Efforts[Efforts[i]]; ok && v != nil {
			return Efforts[i]
		}
	}
	return ""
}

func tierNames() string {
	names := make([]string, len(Tiers))
	for i, t := range Tiers {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}
