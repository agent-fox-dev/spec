package afspec

import (
	"errors"
	"testing"
)

// Every error of the library unwraps to *SpecError, so a caller can catch them
// all with one errors.As.
func TestErrors_UnwrapToSpecError(t *testing.T) {
	base := &SpecError{Msg: "boom"}
	if base.Error() != "boom" {
		t.Errorf("SpecError.Error() = %q; want its message", base.Error())
	}
	for _, err := range []error{
		&LoadError{Msg: "load", File: "prd.md", Err: base},
		&LoadError{Msg: "load", Err: base},
		&SaveError{Msg: "save", Err: base},
		&LifecycleError{Msg: "lifecycle", Err: base},
		&IntentError{Msg: "intent", Err: base},
		&BootstrapError{Msg: "bootstrap", Err: base},
	} {
		var specErr *SpecError
		if !errors.As(err, &specErr) {
			t.Errorf("%T does not unwrap to *SpecError", err)
		}
		if err.Error() == "" {
			t.Errorf("%T has an empty Error()", err)
		}
	}
}
