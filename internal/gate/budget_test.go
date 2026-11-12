package gate

import "testing"

// A gate exceeds its budget only when it takes longer, and says by how much.
func TestMeasureBudget_ExceedsOnlyPastTheBudget(t *testing.T) {
	for _, tc := range []struct {
		seconds  float64
		budget   int
		exceeded bool
		over     float64
	}{
		{119.9, 120, false, 0},
		{120, 120, false, 0},
		{120.5, 120, true, 0.5},
		{30, 20, true, 10},
	} {
		b := MeasureBudget(tc.seconds, tc.budget)
		if b.Seconds != tc.budget || b.Exceeded != tc.exceeded {
			t.Errorf("%v of %d: %+v; want exceeded %v", tc.seconds, tc.budget, b, tc.exceeded)
		}
		if tc.exceeded != (b.OverBySeconds != nil) || (b.OverBySeconds != nil && *b.OverBySeconds != tc.over) {
			t.Errorf("%v of %d: over_by_seconds %v; want %v", tc.seconds, tc.budget, b.OverBySeconds, tc.over)
		}
	}
}
