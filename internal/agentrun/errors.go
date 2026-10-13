package agentrun

import (
	"fmt"

	"github.com/agent-fox-dev/agentkit-go/core"
)

// Error categories. They are the vocabulary the CLI's JSON envelope reports,
// and they exist so a caller can decide whether re-running could help without
// pattern-matching a message.
const (
	// CategoryAuth is a missing or rejected credential. Re-running changes
	// nothing until the environment does.
	CategoryAuth = "auth"
	// CategoryModel is a model that could not be resolved or served.
	CategoryModel = "model"
	// CategoryBudget is the per-phase spend ceiling. Raising it may help.
	CategoryBudget = "budget"
	// CategoryMaxTurns is the per-phase turn ceiling. It usually means the
	// model kept failing validation rather than that the work was large.
	CategoryMaxTurns = "max_turns"
	// CategoryAPI is a provider or transport failure.
	CategoryAPI = "api"
	// CategoryAborted is a cancelled run, or one that outlived its timeout.
	CategoryAborted = "aborted"
	// CategoryRefusal is a model that declined the request. Re-running the
	// same input is unlikely to help.
	CategoryRefusal = "refusal"
	// CategoryNoResult is a run that ended without calling its terminating
	// tool: the model answered in prose, or stopped at a ceiling.
	CategoryNoResult = "no_result"
	// CategoryInternal is a bug in this program.
	CategoryInternal = "internal"
)

// Error is one failure with a category attached.
type Error struct {
	Phase      string
	Detail     string
	Cat        string
	Cause      error
	StopReason core.RunStopReason
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Phase == "" {
		return e.Detail
	}
	return e.Phase + ": " + e.Detail
}

// Category reports the failure class.
func (e *Error) Category() string { return e.Cat }

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// RunStopReason reports the stop reason if this error was caused by a loop termination.
func (e *Error) RunStopReason() core.RunStopReason {
	if e == nil {
		return ""
	}
	return e.StopReason
}

func newError(phase, category string, cause error, format string, args ...any) *Error {
	return &Error{
		Phase:  phase,
		Detail: fmt.Sprintf(format, args...),
		Cat:    category,
		Cause:  cause,
	}
}

// CategoryOf reports the category of err, or CategoryInternal when err
// carries none.
func CategoryOf(err error) string {
	type categorized interface{ Category() string }
	for e := err; e != nil; {
		if c, ok := e.(categorized); ok {
			return c.Category()
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return CategoryInternal
}
