package envelope

import "errors"

// Error categories: the kind of failure, which says whether re-running could
// help. The set is open; a category with no constant here is still valid.
const (
	// CategoryUsage is an invocation that was wrong. Nothing was read.
	CategoryUsage = "usage"
	// CategoryAuth is a missing or rejected credential. Re-running changes
	// nothing until the environment does.
	CategoryAuth = "auth"
	// CategoryModel is a model that could not be resolved or served.
	CategoryModel = "model"
	// CategoryBudget is the per-phase spend ceiling. Raising it may help.
	CategoryBudget = "budget"
	// CategoryMaxTurns is the per-phase turn ceiling.
	CategoryMaxTurns = "max_turns"
	// CategoryAPI is a provider or transport failure. It is retryable.
	CategoryAPI = "api"
	// CategoryAborted is a cancelled run, or one that outlived its timeout.
	// It is retryable.
	CategoryAborted = "aborted"
	// CategoryRefusal is a model that declined the request.
	CategoryRefusal = "refusal"
	// CategoryNoResult is a run that ended without calling its terminating
	// tool.
	CategoryNoResult = "no_result"
	// CategoryInternal is a bug in the command.
	CategoryInternal = "internal"
	// CategoryInput is an input that could not be read.
	CategoryInput = "input"
	// CategoryDisk is a filesystem failure.
	CategoryDisk = "disk"
	// CategoryInvalidSpec means the package was generated and does not
	// satisfy the format's cross-file rules. The files are on disk and the
	// errors name the rules.
	CategoryInvalidSpec = "invalid_spec"
)

// CategoryLifecycle is a call that was read and well-formed, and that the
// package's lifecycle or its task state machine does not allow: a move the
// machine refuses, or a write to a package that can no longer take one.
// Nothing was written. Running the same call again changes nothing, and the
// error's fix hint, where there is one, lists what would be allowed.
const CategoryLifecycle = "lifecycle"

// RetryableFor reports whether an error of the given category can succeed if
// re-run unchanged. Only "api" and "aborted" are retryable.
func RetryableFor(category string) bool {
	return category == CategoryAPI || category == CategoryAborted
}

// FixHint is a machine-usable remedy for a failure.
type FixHint struct {
	Flag    string   `json:"flag,omitempty" description:"the flag to change"`
	Env     []string `json:"env,omitempty" description:"the environment variables, one of which supplies what is missing"`
	Valid   []string `json:"valid,omitempty" description:"the values the flag accepts here"`
	Current float64  `json:"current,omitempty" description:"the flag's value on this run"`
	Suggest float64  `json:"suggest,omitempty" description:"a value to try instead"`
}

// ErrorInfo says what failed and where. It is present exactly when ok is
// false. It is an error, so a caller can return it as it is.
type ErrorInfo struct {
	// Stage is the pipeline step that failed: usage, input, preflight, prd,
	// requirements, test_spec, tasks, plan, scaffold, write, validate, budget,
	// load; and, for speca's task form, status (the package cannot take a
	// write) and task (the state machine refuses a move).
	Stage string `json:"stage" description:"the step that failed"`
	// Category says whether re-running could help.
	Category  string   `json:"category" description:"the kind of failure, which says whether re-running could help; an open set"`
	Message   string   `json:"message" description:"what failed, in words"`
	Retryable bool     `json:"retryable" description:"whether re-running the identical command could succeed"`
	Resumable bool     `json:"resumable" description:"whether re-running the same input continues the work rather than starting it over"`
	FixHint   *FixHint `json:"fix_hint,omitempty" description:"a mechanical remedy, where one is known"`
	err       error
}

func (e *ErrorInfo) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap returns the failure the command raised this from. It is set only in
// the process that ran the command; an ErrorInfo decoded from JSON has none.
func (e *ErrorInfo) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// WithCause records the failure e reports, for Unwrap, and returns e.
func (e *ErrorInfo) WithCause(err error) *ErrorInfo {
	e.err = err
	return e
}

// TotalBudgetUSD is the --total-budget ceiling a budget stop was checked
// against, read from the failure it wraps; zero for any other failure and for
// an ErrorInfo decoded from JSON.
func (e *ErrorInfo) TotalBudgetUSD() float64 {
	var tb interface{ TotalBudgetUSD() float64 }
	if e == nil || e.err == nil || !errors.As(e.err, &tb) {
		return 0
	}
	return tb.TotalBudgetUSD()
}
