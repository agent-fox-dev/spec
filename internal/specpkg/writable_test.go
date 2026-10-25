package specpkg

import (
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
)

// IntentDrift is true only for an active package whose hash is set and is not
// the hash of its intent, or whose intent is gone.
func TestIntentDrift(t *testing.T) {
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
		"active, empty hash":         {specOf("active", ptr(""), bodyWithIntent), true},
		"draft, different":           {specOf("draft", &other, bodyWithIntent), false},
		"sealed, different":          {specOf("sealed", &other, bodyWithIntent), false},
		"superseded, no intent":      {specOf("superseded", &other, bodyNoIntent), false},
		"archived, different":        {specOf("archived", &other, bodyWithIntent), false},
		"nil package is not drifted": {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IntentDrift(tc.spec); got != tc.want {
				t.Errorf("IntentDrift = %v; want %v", got, tc.want)
			}
		})
	}
}

// For every status, every state of the hash and every state of the intent,
// Writable is whether the package is a draft, or active without drift, with
// drift worked out here on its own; a nil package cannot take a write.
func TestWritable_IsDraftOrActiveWithoutDrift(t *testing.T) {
	hashes := map[string]*string{
		"unset":     nil,
		"matching":  hashOf(t, bodyWithIntent),
		"different": ptr("1111111111111111111111111111111111111111111111111111111111111111"),
		"empty":     ptr(""),
	}
	bodies := map[string]string{"the intent": bodyWithIntent, "an edited intent": bodyEditedIntent, "no intent": bodyNoIntent}
	for _, status := range []string{"draft", "active", "sealed", "superseded", "archived"} {
		for hashName, hash := range hashes {
			for bodyName, body := range bodies {
				t.Run(status+", "+hashName+" hash, "+bodyName, func(t *testing.T) {
					spec := specOf(status, hash, body)
					computed, err := afspec.ComputeIntentHash(body)
					drift := status == "active" && hash != nil && (err != nil || computed != *hash)
					writable := status == "draft" || (status == "active" && !drift)
					if got := IntentDrift(spec); got != drift {
						t.Errorf("IntentDrift = %v; want %v", got, drift)
					}
					if got := Writable(spec); got != writable {
						t.Errorf("Writable = %v; want %v", got, writable)
					}
				})
			}
		}
	}
	if Writable(nil) {
		t.Error("a nil package is writable")
	}
}
