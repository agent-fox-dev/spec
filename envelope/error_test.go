package envelope

import "testing"

// TS-11-49: CategoryLifecycle is "lifecycle", and re-running a refused call
// unchanged cannot succeed; the other categories keep their retryability.
func TestCategoryLifecycleIsNotRetryable_TS11_49(t *testing.T) {
	if CategoryLifecycle != "lifecycle" {
		t.Errorf("CategoryLifecycle = %q; want lifecycle", CategoryLifecycle)
	}
	if RetryableFor(CategoryLifecycle) {
		t.Error("a lifecycle refusal is retryable")
	}
	for category, want := range map[string]bool{
		CategoryAPI: true, CategoryAborted: true,
		CategoryUsage: false, CategoryInput: false, CategoryInvalidSpec: false, CategoryDisk: false, CategoryInternal: false,
	} {
		if got := RetryableFor(category); got != want {
			t.Errorf("RetryableFor(%q) = %v; want %v", category, got, want)
		}
	}
}
