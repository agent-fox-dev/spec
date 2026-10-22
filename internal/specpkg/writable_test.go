package specpkg

import (
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

const (
	bodyWithIntent   = "# A package\n\n## Intent\n\nDo the thing.\n\n## Goals\n\n- one\n"
	bodyEditedIntent = "# A package\n\n## Intent\n\nDo another thing.\n\n## Goals\n\n- one\n"
	bodyNoIntent     = "# A package\n\n## Goals\n\n- one\n"
)

var statuses = []string{"draft", "active", "sealed", "superseded", "archived"}

func hashOf(t *testing.T, body string) *string {
	t.Helper()
	h, err := afspec.ComputeIntentHash(body)
	if err != nil {
		t.Fatalf("ComputeIntentHash: %v", err)
	}
	return &h
}

func specOf(status string, hash *string, body string) *afspec.Spec {
	return &afspec.Spec{Status: status, IntentHash: hash, PRDBody: body}
}

// TS-13-11: IntentDrift is true only for an active package whose hash is set and
// is not the hash of its intent, or whose intent is gone.
func TestIntentDriftIsTrueOnlyForAnActivePackageWhoseHashFailsOrDiffers_TS13_11(t *testing.T) {
	matching := hashOf(t, bodyWithIntent)
	other := "0000000000000000000000000000000000000000000000000000000000000000"
	for name, tc := range map[string]struct {
		spec *afspec.Spec
		want bool
	}{
		"active, set, matching":      {specOf("active", matching, bodyWithIntent), false},
		"active, set, different":     {specOf("active", &other, bodyWithIntent), true},
		"active, set, edited intent": {specOf("active", matching, bodyEditedIntent), true},
		"active, set, no intent":     {specOf("active", matching, bodyNoIntent), true},
		"active, unset":              {specOf("active", nil, bodyWithIntent), false},
		"active, unset, no intent":   {specOf("active", nil, bodyNoIntent), false},
		// The library refuses to save an active package whose hash is set, empty
		// included, and is not the hash of its intent.
		"active, empty hash": {specOf("active", ptr(""), bodyWithIntent), true},
	} {
		if got := IntentDrift(tc.spec); got != tc.want {
			t.Errorf("%s: IntentDrift = %v; want %v", name, got, tc.want)
		}
	}
	for _, status := range []string{"draft", "sealed", "superseded", "archived"} {
		if IntentDrift(specOf(status, &other, bodyWithIntent)) || IntentDrift(specOf(status, &other, bodyNoIntent)) {
			t.Errorf("%s: IntentDrift is true, and only an active package drifts", status)
		}
	}
}

func ptr(s string) *string { return &s }

// TS-13-12: for every status, every state of the hash and every state of the
// intent, Writable is whether the package is a draft, or active without drift,
// and IntentDrift is the rule it is stated by, worked out here on its own.
func TestWritableIsDraftOrActiveWithoutDrift_TS13_12(t *testing.T) {
	hashes := map[string]*string{
		"unset":     nil,
		"matching":  hashOf(t, bodyWithIntent),
		"different": ptr("1111111111111111111111111111111111111111111111111111111111111111"),
		"empty":     ptr(""),
	}
	bodies := map[string]string{"the intent": bodyWithIntent, "an edited intent": bodyEditedIntent, "no intent": bodyNoIntent}
	checked := 0
	for _, status := range statuses {
		for hashName, hash := range hashes {
			for bodyName, body := range bodies {
				spec := specOf(status, hash, body)
				computed, err := afspec.ComputeIntentHash(body)
				drift := status == "active" && hash != nil && (err != nil || computed != *hash)
				writable := status == "draft" || (status == "active" && !drift)
				if got := IntentDrift(spec); got != drift {
					t.Errorf("%s, %s hash, %s: IntentDrift = %v; want %v", status, hashName, bodyName, got, drift)
				}
				if got := Writable(spec); got != writable {
					t.Errorf("%s, %s hash, %s: Writable = %v; want %v", status, hashName, bodyName, got, writable)
				}
				checked++
			}
		}
	}
	if checked != len(statuses)*len(hashes)*len(bodies) {
		t.Errorf("checked %d combinations", checked)
	}
}

// TS-13-13: neither function dereferences a nil package.
func TestANilPackageIsNeitherDriftedNorWritable_TS13_13(t *testing.T) {
	if IntentDrift(nil) || Writable(nil) {
		t.Error("a nil package is drifted or writable")
	}
}
