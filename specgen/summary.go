package specgen

import "github.com/agent-fox-dev/spec/internal/cli"

// summaryValidation is ValidationReport reduced to what a caller acts on
// next: whether the package validates and what to fix if not. Dropped:
// warning_count and warnings.
type summaryValidation struct {
	Valid      bool             `json:"valid"`
	ErrorCount int              `json:"error_count"`
	Errors     []ValidationItem `json:"errors,omitempty"`
}

// summaryTrace is TraceReport reduced to the gaps a caller would act on, plus
// the covered counts: without them a package with no gaps prints `{}`, which
// looks the same as a trace nobody computed.
type summaryTrace struct {
	CriteriaCovered   int      `json:"criteria_covered"`
	PathsCovered      int      `json:"paths_covered"`
	CriteriaUncovered []string `json:"criteria_uncovered,omitempty"`
	PathsUncovered    []string `json:"paths_uncovered,omitempty"`
	TestsUnowned      []string `json:"tests_unowned,omitempty"`
}

// summaryResult is the trimmed view of Result kept under --detail summary.
// Everything under result but split[] describes the first package of a
// split. follow_on_specs and split_plan are dropped: split[] names every
// scope with its status and, for the packages this run wrote, whether it
// validates, its error and open-question counts and its trace gaps. Every
// other field of Result is still computed exactly as under --detail full.
type summaryResult struct {
	SpecDir       string            `json:"spec_dir"`
	SpecID        string            `json:"spec_id"`
	SpecName      string            `json:"spec_name"`
	Status        string            `json:"status"`
	Artifacts     []string          `json:"artifacts"`
	Validation    summaryValidation `json:"validation"`
	Traceability  summaryTrace      `json:"traceability"`
	OpenQuestions []OpenQuestion    `json:"open_questions,omitempty"`
	// Review is the first package's review without its rounds: whether the
	// review finished and how much it changed, at a glance.
	Review *Review       `json:"review,omitempty"`
	Split  []ScopeReport `json:"split,omitempty"`
	// Stage, Preflight and Estimate are the whole point of a --preflight
	// run, so the default view keeps them. They are absent on every
	// ordinary run.
	Stage     string               `json:"stage,omitempty"`
	Preflight []cli.PreflightCheck `json:"preflight,omitempty"`
	Estimate  *cli.Estimate        `json:"estimate,omitempty"`
	Detail    string               `json:"detail"`
}

// SummaryView implements cli.Summarizable. It is defined on Result
// (rather than on the embedded Package) because Split lives on Result
// alone.
func (r *Result) SummaryView() any {
	return summaryResult{
		SpecDir:   r.SpecDir,
		SpecID:    r.SpecID,
		SpecName:  r.SpecName,
		Status:    r.Status,
		Artifacts: r.Package.Artifacts,
		Validation: summaryValidation{
			Valid:      r.Validation.Valid,
			ErrorCount: r.Validation.ErrorCount,
			Errors:     r.Validation.Errors,
		},
		Traceability: summaryTrace{
			CriteriaCovered:   r.Traceability.CriteriaCovered,
			PathsCovered:      r.Traceability.PathsCovered,
			CriteriaUncovered: r.Traceability.CriteriaUncovered,
			PathsUncovered:    r.Traceability.PathsUncovered,
			TestsUnowned:      r.Traceability.TestsUnowned,
		},
		OpenQuestions: r.OpenQuestions,
		Review:        reviewCounts(r.Review),
		Split:         r.Split,
		Stage:         r.Stage,
		Preflight:     r.Preflight,
		Estimate:      r.Estimate,
		Detail:        "summary",
	}
}

// reviewCounts is a review without its rounds, nil for none.
func reviewCounts(r *Review) *Review {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Rounds = nil
	return &cp
}
