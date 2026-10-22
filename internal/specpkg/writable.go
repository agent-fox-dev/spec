package specpkg

import "github.com/agent-fox-dev/spec/afspec"

// IntentDrift reports whether the intent of an active package has been changed
// since it was activated: its status is active, it has an intent_hash, and the
// hash of its PRD's ## Intent section either cannot be computed (the section is
// gone) or is another. It is the rule by which the library refuses to save an
// active package, so a caller learns here why. A nil package has not drifted.
func IntentDrift(spec *afspec.Spec) bool {
	if spec == nil || spec.Status != string(afspec.PrdFrontmatterV2JsonStatusActive) || spec.IntentHash == nil {
		return false
	}
	hash, err := afspec.ComputeIntentHash(spec.PRDBody)
	return err != nil || hash != *spec.IntentHash
}

// Writable reports whether a package can take a write: it is a draft, or it is
// active and its intent has not drifted. A sealed, superseded or archived package
// cannot, and neither can an active one whose intent drifted. speca's task and
// status writes accept only a writable package, and specr suggests a call of
// speca only for one, so the two ask this one question. A nil package cannot.
func Writable(spec *afspec.Spec) bool {
	if spec == nil {
		return false
	}
	switch spec.Status {
	case string(afspec.PrdFrontmatterV2JsonStatusDraft):
		return true
	case string(afspec.PrdFrontmatterV2JsonStatusActive):
		return !IntentDrift(spec)
	}
	return false
}
