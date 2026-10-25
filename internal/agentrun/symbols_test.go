package agentrun

import (
	"testing"
)

// The probe names one of the two backends for a workspace, and fails only when
// there is none to probe.
func TestDetectSymbolBackend(t *testing.T) {
	got, err := DetectSymbolBackend(openWorkspace(t, t.TempDir()))
	if err != nil || (got != SymbolBackendTreeSitter && got != SymbolBackendGoOnly) {
		t.Errorf("DetectSymbolBackend = %q, %v; want one of the two backends", got, err)
	}
	if _, err := DetectSymbolBackend(nil); err == nil {
		t.Error("detection without a workspace succeeded")
	}
}
