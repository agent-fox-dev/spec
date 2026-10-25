package agentrun

import (
	"errors"
	"fmt"
	"testing"
)

// The category of an error is found through any wrapping, and an error that
// carries none is internal.
func TestCategoryOf(t *testing.T) {
	budget := newError("prd", CategoryBudget, nil, "over")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, CategoryInternal},
		{"plain", errors.New("boom"), CategoryInternal},
		{"categorized", budget, CategoryBudget},
		{"wrapped", fmt.Errorf("run: %w", budget), CategoryBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CategoryOf(tc.err); got != tc.want {
				t.Errorf("CategoryOf = %s, want %s", got, tc.want)
			}
		})
	}
}

// An error names its phase when it has one, and unwraps to its cause.
func TestError_NamesItsPhaseAndUnwraps(t *testing.T) {
	cause := errors.New("connection reset")
	if got := newError("prd", CategoryAPI, cause, "%v", cause); got.Error() != "prd: connection reset" || !errors.Is(got, cause) {
		t.Errorf("error = %q, unwraps to cause = %v", got, errors.Is(got, cause))
	}
	if got := newError("", CategoryModel, nil, "no model resolved"); got.Error() != "no model resolved" {
		t.Errorf("error without a phase = %q", got)
	}
	var none *Error
	if none.Error() != "" || none.RunStopReason() != "" {
		t.Error("a nil Error is not empty")
	}
}
