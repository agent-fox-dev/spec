package afspec

import "fmt"

// defaultCriterionSystem is used when neither a criterion nor its requirement
// names a `system` (format v3 §6.2.1).
const defaultCriterionSystem = "system"

func strPtr(s string) *string { return &s }

// UbiquitousCriterion constructs a criterion with the ubiquitous pattern:
// "THE {system} SHALL {action}". Correctness properties are written this way,
// with an action that starts with "for any …". The check is the caller's to
// set (OwnCheck, SameAs).
func UbiquitousCriterion(id, system, action string) Criterion {
	return Criterion{
		Id:      id,
		Pattern: CriterionPatternUbiquitous,
		System:  optionalSystem(system),
		Action:  action,
	}
}

// EventDrivenCriterion constructs a criterion with the event_driven pattern:
// "WHEN {condition}, THE {system} SHALL {action}".
func EventDrivenCriterion(id, condition, system, action string) Criterion {
	return Criterion{
		Id:        id,
		Pattern:   CriterionPatternEventDriven,
		Condition: strPtr(condition),
		System:    optionalSystem(system),
		Action:    action,
	}
}

// StateDrivenCriterion constructs a criterion with the state_driven pattern:
// "WHILE {condition}, THE {system} SHALL {action}".
func StateDrivenCriterion(id, condition, system, action string) Criterion {
	return Criterion{
		Id:        id,
		Pattern:   CriterionPatternStateDriven,
		Condition: strPtr(condition),
		System:    optionalSystem(system),
		Action:    action,
	}
}

// UnwantedCriterion constructs a criterion with the unwanted pattern:
// "IF {condition}, THEN THE {system} SHALL {action}". The contract is
// mandatory for this pattern (§6.5.2, rule C6).
func UnwantedCriterion(id, condition, system, action, contract string) Criterion {
	return Criterion{
		Id:        id,
		Pattern:   CriterionPatternUnwanted,
		Condition: strPtr(condition),
		System:    optionalSystem(system),
		Action:    action,
		Contract:  strPtr(contract),
	}
}

// OptionalCriterion constructs a criterion with the optional pattern:
// "WHERE {condition}, THE {system} SHALL {action}".
func OptionalCriterion(id, condition, system, action string) Criterion {
	return Criterion{
		Id:        id,
		Pattern:   CriterionPatternOptional,
		Condition: strPtr(condition),
		System:    optionalSystem(system),
		Action:    action,
	}
}

// WithContract returns a copy of the criterion carrying the given contract.
// A contract is required for unwanted criteria and recommended whenever the
// result is consumed by something else.
func (c Criterion) WithContract(contract string) Criterion {
	c.Contract = strPtr(contract)
	return c
}

// WithCheck returns a copy of the criterion carrying its own check of the
// given kind, with the setup and the pseudocode when they are not empty
// (format v3 §6.2.2).
func (c Criterion) WithCheck(kind CheckKind, setup []string, pseudocode string) Criterion {
	c.Check = OwnCheck(kind, setup, pseudocode)
	return c
}

// WithSameCheckAs returns a copy of the criterion whose check is the same as
// the named criterion's own check, or the named path's smoke check.
func (c Criterion) WithSameCheckAs(id string) Criterion {
	c.Check = SameAs(id)
	return c
}

// OwnCheck is a criterion's own check: its kind, and the setup and the
// pseudocode when they are not empty.
func OwnCheck(kind CheckKind, setup []string, pseudocode string) Check {
	k := kind
	ch := Check{Kind: &k}
	if len(setup) > 0 {
		ch.Setup = append([]string{}, setup...)
	}
	if pseudocode != "" {
		ch.Pseudocode = strPtr(pseudocode)
	}
	return ch
}

// SameAs is a shared check: the same as the named criterion's own check, or
// the named path's smoke check.
func SameAs(id string) Check { return Check{SameAs: strPtr(id)} }

// IsShared reports whether the check is the same as another's.
func (ch Check) IsShared() bool { return ch.SameAs != nil }

// KindText returns the kind of an own check, or "" for a shared one.
func (ch Check) KindText() string {
	if ch.Kind == nil {
		return ""
	}
	return string(*ch.Kind)
}

// PseudocodeText returns the pseudocode, or the empty string when absent.
func (ch Check) PseudocodeText() string {
	if ch.Pseudocode == nil {
		return ""
	}
	return *ch.Pseudocode
}

// PseudocodeText returns the smoke check's pseudocode, or the empty string
// when absent.
func (sm Smoke) PseudocodeText() string {
	if sm.Pseudocode == nil {
		return ""
	}
	return *sm.Pseudocode
}

// optionalSystem drops an empty system so that the field is omitted rather
// than written as an empty string, which the schema rejects.
func optionalSystem(system string) *string {
	if system == "" {
		return nil
	}
	return strPtr(system)
}

// SystemName returns the acting component the criterion itself names,
// defaulting to "system" when it omits it. A criterion inside a requirement
// takes the requirement's system first: see Requirement.Resolve.
func (c Criterion) SystemName() string {
	if c.System == nil || *c.System == "" {
		return defaultCriterionSystem
	}
	return *c.System
}

// Resolve returns a copy of c with the requirement's system filled in when c
// names none (format v3 §6.2.1), so that the copy renders as the package
// means it.
func (r Requirement) Resolve(c Criterion) Criterion {
	if (c.System == nil || *c.System == "") && r.System != nil && *r.System != "" {
		c.System = strPtr(*r.System)
	}
	return c
}

// ConditionText returns the condition clause, or the empty string when absent.
func (c Criterion) ConditionText() string {
	if c.Condition == nil {
		return ""
	}
	return *c.Condition
}

// ContractText returns the contract, or the empty string when absent.
func (c Criterion) ContractText() string {
	if c.Contract == nil {
		return ""
	}
	return *c.Contract
}

// RenderEARSSentence renders the criterion as its EARS sentence (§6.2.1):
//
//   - ubiquitous:    "THE {system} SHALL {action}"
//   - event_driven:  "WHEN {condition}, THE {system} SHALL {action}"
//   - state_driven:  "WHILE {condition}, THE {system} SHALL {action}"
//   - unwanted:      "IF {condition}, THEN THE {system} SHALL {action}"
//   - optional:      "WHERE {condition}, THE {system} SHALL {action}"
//
// The contract is not part of the sentence; renderers append it on a second
// line as "→ {contract}" (§11.2). Use RenderEARS for both at once. The system
// is the criterion's own; a renderer resolves the requirement's first.
func (c Criterion) RenderEARSSentence() string {
	core := fmt.Sprintf("THE %s SHALL %s", c.SystemName(), c.Action)

	switch c.Pattern {
	case CriterionPatternUbiquitous:
		return core
	case CriterionPatternEventDriven:
		return fmt.Sprintf("WHEN %s, %s", c.ConditionText(), core)
	case CriterionPatternStateDriven:
		return fmt.Sprintf("WHILE %s, %s", c.ConditionText(), core)
	case CriterionPatternUnwanted:
		return fmt.Sprintf("IF %s, THEN %s", c.ConditionText(), core)
	case CriterionPatternOptional:
		return fmt.Sprintf("WHERE %s, %s", c.ConditionText(), core)
	default:
		return core
	}
}

// RenderEARS renders the criterion as its EARS sentence followed, when a
// contract is present, by a second line "→ {contract}" (§11.2). A contract the
// coder cannot see does not exist, so every renderer goes through this.
func (c Criterion) RenderEARS() string {
	sentence := c.RenderEARSSentence()
	if contract := c.ContractText(); contract != "" {
		return sentence + "\n   → " + contract
	}
	return sentence
}
