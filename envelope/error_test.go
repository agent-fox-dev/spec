package envelope_test

import (
	"errors"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
)

// Only a provider failure and a cancelled run can succeed when re-run
// unchanged; a lifecycle refusal, like every other category, cannot.
func TestRetryableFor(t *testing.T) {
	for category, want := range map[string]bool{
		envelope.CategoryAPI: true, envelope.CategoryAborted: true,
		envelope.CategoryUsage: false, envelope.CategoryInput: false, envelope.CategoryInvalidSpec: false,
		envelope.CategoryDisk: false, envelope.CategoryInternal: false, envelope.CategoryBudget: false,
		envelope.CategoryLifecycle: false, "": false,
	} {
		t.Run(category, func(t *testing.T) {
			if got := envelope.RetryableFor(category); got != want {
				t.Errorf("RetryableFor(%q) = %v, want %v", category, got, want)
			}
		})
	}
}

type budgetStop struct{ usd float64 }

func (b budgetStop) Error() string           { return "over budget" }
func (b budgetStop) TotalBudgetUSD() float64 { return b.usd }

// In the process that ran the command, an ErrorInfo unwraps to the failure it
// reports, and reads the total budget off it.
func TestErrorInfo_CarriesItsCause(t *testing.T) {
	cause := budgetStop{usd: 2.5}
	info := (&envelope.ErrorInfo{Message: "stopped", Category: envelope.CategoryBudget}).WithCause(cause)
	if !errors.Is(info, cause) {
		t.Error("the ErrorInfo does not unwrap to its cause")
	}
	if got := info.TotalBudgetUSD(); got != 2.5 {
		t.Errorf("TotalBudgetUSD = %v, want 2.5", got)
	}

	other := (&envelope.ErrorInfo{Message: "m"}).WithCause(errors.New("disk full"))
	if got := other.TotalBudgetUSD(); got != 0 {
		t.Errorf("a failure that is not a budget stop reports a total budget of %v", got)
	}

	var none *envelope.ErrorInfo
	if none.Error() != "" || none.Unwrap() != nil || none.TotalBudgetUSD() != 0 {
		t.Error("a nil ErrorInfo is not empty")
	}
}
