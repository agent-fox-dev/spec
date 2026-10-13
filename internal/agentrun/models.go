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

// tierEntry pairs a catalog model id with the effort the tier prescribes.
// Separating model from effort lets two tiers share a model while running it
// at different depth.
type tierEntry struct {
	id     string
	effort core.Effort
}

// tierTable maps a tier to a catalog model id and effort. It is an alias
// table over AgentKit's catalog, not a registry: the catalog supplies the
// context window, output cap, prices and which efforts the model takes.
var tierTable = map[ModelTier]tierEntry{
	TierSimple:   {id: "claude-sonnet-5-5", effort: core.EffortMedium},
	TierStandard: {id: "claude-sonnet-5-5", effort: core.EffortHigh},
	TierAdvanced: {id: "claude-opus-5-5", effort: core.EffortXHigh},
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

// ResolveModel resolves a tier name or a model id to its catalog row and an
// effort.
//
// A tier name (case-insensitive) is looked up in the tier table and carries
// the tier's effort. Anything else must be a model the catalog lists, with or
// without an "anthropic/" prefix: an unlisted id would run with no price, so
// no budget could ever stop it. effort, when set (explicit is true), replaces
// the tier's; an empty explicit effort sends none. An effort the model does
// not take is lowered to the highest one it does, and Clamped records what
// was asked for; a model that takes no effort at or below it gets none.
func ResolveModel(spec string, effort core.Effort, explicit bool) (Model, error) {
	if strings.TrimSpace(spec) == "" {
		return Model{}, newError("", CategoryModel, nil, "the model name must not be empty")
	}
	id := spec
	var want core.Effort
	if entry, ok := tierTable[ModelTier(strings.ToUpper(spec))]; ok {
		id, want = entry.id, entry.effort
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
