package envelope

// SpecResult is the result of the spec command: the package a run wrote, and
// for an input that was more than one spec's worth of work, the further
// packages and the whole plan.
//
// The fields of the first package this run wrote sit at the top level, so a
// run on an input that is one spec's worth of work reports exactly that
// package. Under --detail summary only some of the fields are written (the
// ones a caller acts on next); Detail says which view this is.
type SpecResult struct {
	Package

	// FollowOnSpecs are the further packages this run wrote, in order, when
	// the input was more than one spec's worth of work.
	FollowOnSpecs []Package `json:"follow_on_specs,omitempty"`
	// Split is every scope of the split with its state: done, invalid,
	// failed or pending. Empty when the input was one spec's worth of work.
	Split []ScopeReport `json:"split,omitempty"`
	// SplitPlan is the plan file left in the spec root while the split is
	// unfinished. Running spec on the same input again resumes from it.
	SplitPlan string `json:"split_plan,omitempty"`
	// Stage is "preflight" on a --preflight run, which stops before any
	// model phase; it is absent on an ordinary run.
	Stage string `json:"stage,omitempty"`
	// DryRun records that nothing was written.
	DryRun bool `json:"dry_run,omitempty"`
	// Preflight and Estimate are set only by a --preflight run whose checks
	// all passed: the checks that were performed, and what the real run would
	// spend at most.
	Preflight []PreflightCheck `json:"preflight,omitempty"`
	Estimate  *Estimate        `json:"estimate,omitempty"`
}

// StagePreflight is the Stage of a --preflight run.
const StagePreflight = "preflight"

// Package describes one specification package.
type Package struct {
	// SpecDir is the created package, relative to the repository root when
	// it is inside it.
	SpecDir  string `json:"spec_dir"`
	SpecID   string `json:"spec_id"`
	SpecName string `json:"spec_name"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Source   string `json:"source"`
	// Artifacts are the files written, in the order they were produced.
	Artifacts []string `json:"artifacts"`

	// Counts summarize the package without reproducing it. A caller that
	// wants the content reads the files.
	Requirements   int `json:"requirements"`
	Criteria       int `json:"criteria"`
	ExecutionPaths int `json:"execution_paths"`
	Tests          int `json:"tests"`
	Tasks          int `json:"tasks"`

	// Validation is the format's own verdict on the package.
	Validation ValidationReport `json:"validation"`
	// Traceability is derived from test.verifies and task.tests, never
	// stored — the format makes both computed, so reporting them here is the
	// only place they exist.
	Traceability TraceReport `json:"traceability"`

	// OpenQuestions are the decisions the PRD phase made under uncertainty
	// and would like a person to check. An empty list means the input and
	// the codebase settled everything.
	OpenQuestions []OpenQuestion `json:"open_questions,omitempty"`

	// CommentURL is set when the finished PRD was posted back to the issue.
	CommentURL string `json:"comment_url,omitempty"`

	// RelevantFiles are the files the PRD phase identified as important for
	// later generation phases. They live only in the envelope and the
	// run-time pipeline, never inside the spec package directory.
	RelevantFiles []RelevantFile `json:"relevant_files,omitempty"`

	// Review is how the PRD was reviewed and revised before the package was
	// generated from it. It is present exactly when the run had
	// --adversarial on.
	Review *Review `json:"review,omitempty"`

	// Detail records which view of this result was emitted: "summary" or
	// "full". It is present on both.
	Detail string `json:"detail"`
}

// SetDetail records which view of the result is emitted.
func (p *Package) SetDetail(d string) { p.Detail = d }

// Scope states, as reported in SpecResult.Split.
const (
	// ScopeDone means the package exists and validates.
	ScopeDone = "done"
	// ScopeInvalid means the package exists and does not validate. It is
	// not written again; the errors name the rules to fix by hand.
	ScopeInvalid = "invalid"
	// ScopeFailed means this run stopped on this scope. The plan is kept,
	// and running spec on the same input again resumes here.
	ScopeFailed = "failed"
	// ScopePending means the scope was not reached.
	ScopePending = "pending"
)

// ScopeReport is one scope of a split and where it stands.
type ScopeReport struct {
	Name    string `json:"name"`
	Scope   string `json:"scope"`
	Status  string `json:"status"`
	SpecID  string `json:"spec_id,omitempty"`
	SpecDir string `json:"spec_dir,omitempty"`
	// The package's own state, so the summary view, which describes only the
	// first package under result, still says whether every other one is
	// ready. Absent for a scope this run did not write.
	Valid              *bool `json:"valid,omitempty"`
	ErrorCount         int   `json:"error_count,omitempty"`
	OpenQuestionsCount int   `json:"open_questions_count,omitempty"`
	TraceGaps          int   `json:"trace_gaps,omitempty"`
}

// ValidationReport is afspec's verdict, flattened for JSON.
type ValidationReport struct {
	Valid        bool             `json:"valid"`
	ErrorCount   int              `json:"error_count"`
	WarningCount int              `json:"warning_count"`
	Errors       []ValidationItem `json:"errors,omitempty"`
	Warnings     []ValidationItem `json:"warnings,omitempty"`
}

// ValidationItem is one finding, carrying the rule that produced it.
type ValidationItem struct {
	Check    string `json:"check"`
	Artifact string `json:"artifact"`
	Entity   string `json:"entity,omitempty"`
	Message  string `json:"message"`
}

// TraceReport is the derived coverage summary.
type TraceReport struct {
	CriteriaCovered   int      `json:"criteria_covered"`
	CriteriaUncovered []string `json:"criteria_uncovered,omitempty"`
	PathsCovered      int      `json:"paths_covered"`
	PathsUncovered    []string `json:"paths_uncovered,omitempty"`
	TestsUnowned      []string `json:"tests_unowned,omitempty"`
}

// OpenQuestion is one decision made under uncertainty.
//
// It is the shape the "self-resolve, flag low confidence" contract takes: the
// question was answered, the answer is in the PRD, and this records that a
// different answer would have produced different work. A caller that wants a
// human in the loop reads this array; a caller that does not still gets a
// finished spec.
type OpenQuestion struct {
	Question string `json:"question"`
	Decision string `json:"decision"`
	Why      string `json:"why_unsure"`
}

// RelevantFile is one file the PRD phase identified as important for later
// generation phases.
type RelevantFile struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

// Review is the adversarial review of one package's PRD: every round, every
// finding, and what became of it. It lives only in the result, never in the
// package directory.
type Review struct {
	// RoundsRun counts the rounds started, a review with no findings
	// included. It is 0 when the budget stopped the run before the first
	// review.
	RoundsRun int `json:"rounds_run"`
	// RoundsAllowed is the round limit, --adversarial-rounds or 1.
	RoundsAllowed int `json:"rounds_allowed"`
	// StopReason is the one reason the rounds stopped: one of the three
	// ReviewStop constants.
	StopReason string `json:"stop_reason"`
	// The totals over every round, derived from the rounds. FindingsCount is
	// the sum of the other five.
	FindingsCount   int `json:"findings_count"`
	FixedCount      int `json:"fixed_count"`
	ContestedCount  int `json:"contested_count"`
	EscalatedCount  int `json:"escalated_count"`
	DeclinedCount   int `json:"declined_count"`
	UnansweredCount int `json:"unanswered_count"`
	// Rounds are the rounds in order. The --detail summary view leaves them
	// out and keeps the counts.
	Rounds []ReviewRound `json:"rounds,omitempty"`
}

// ReviewRound is one review and the revision that answered it.
type ReviewRound struct {
	Round int `json:"round"`
	// Clean is true when the round's review raised no critical and no major
	// finding.
	Clean    bool            `json:"clean"`
	Findings []ReviewFinding `json:"findings"`
}

// ReviewFinding is one flaw a reviewer reported and what became of it.
type ReviewFinding struct {
	// ID is R<round>-F<n>, assigned by the run in the order the reviewer
	// submitted the findings.
	ID       string `json:"id"`
	Severity string `json:"severity"`
	// Rubric is the standard the finding applies: one of the Rubric
	// constants. The reviewer sets it and the reviser cannot change it.
	Rubric   string `json:"rubric"`
	Section  string `json:"section"`
	Flaw     string `json:"flaw"`
	Evidence string `json:"evidence"`
	Remedy   string `json:"remedy"`
	// Reopens is the id of an earlier contested finding this one raises
	// again with evidence the contest did not answer.
	Reopens string `json:"reopens,omitempty"`
	// Disposition is the reviser's answer: one of the Disposition constants.
	// The fields below are the ones that answer carries, and absent for the
	// others.
	Disposition string `json:"disposition"`
	// Reason is why a contested finding does not hold, why a declined one is
	// not remedied in this package, or why an escalation's decision is
	// uncertain.
	Reason string `json:"reason,omitempty"`
	// CounterEvidence is a contest's: a quote found in the PRD or the input,
	// or a repository path that exists, with what it shows.
	CounterEvidence string `json:"counter_evidence,omitempty"`
	// Question, Options and Decision are an escalation's: the question a
	// person should answer, the two or three options, and the one the reviser
	// took provisionally.
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	Decision string   `json:"decision,omitempty"`
}

// A finding's severity.
const (
	// SeverityCritical means the package would specify the wrong thing.
	SeverityCritical = "critical"
	// SeverityMajor means a requirement would be missing or untestable.
	SeverityMajor = "major"
	// SeverityMinor is wording and clarity.
	SeverityMinor = "minor"
)

// A finding's disposition: the reviser's answer to it.
const (
	// DispositionFixed means the PRD was changed to remedy the finding.
	DispositionFixed = "fixed"
	// DispositionContested means the finding does not hold, shown by
	// counter-evidence the run found.
	DispositionContested = "contested"
	// DispositionEscalated means the finding holds and its remedy takes a
	// decision a person should own: the reviser took one provisionally and
	// the question is handed to the caller.
	DispositionEscalated = "escalated"
	// DispositionDeclined means the finding holds and is not remedied in this
	// package, for the reason given. A critical or a steering finding cannot
	// be declined.
	DispositionDeclined = "declined"
	// DispositionUnanswered means no revision finished for its round.
	DispositionUnanswered = "unanswered"
)

// A finding's rubric: the standard it applies.
const (
	// RubricPRD is a flaw of the PRD itself.
	RubricPRD = "prd"
	// RubricSteering is a contradiction of a directive of the project's
	// steering.
	RubricSteering = "steering"
)

// Why the rounds of a review stopped.
const (
	// ReviewStopConverged means two consecutive rounds were clean: their
	// reviews raised nothing critical or major.
	ReviewStopConverged = "converged"
	// ReviewStopRoundsExhausted means the round limit was reached without two
	// consecutive clean rounds. A limit of one always stops so, or as
	// incomplete.
	ReviewStopRoundsExhausted = "rounds_exhausted"
	// ReviewStopIncomplete means a review or revision did not finish, or the
	// total budget stopped the rounds. It wins over every other reason.
	ReviewStopIncomplete = "incomplete"
)
