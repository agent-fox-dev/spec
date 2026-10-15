package cli

import "github.com/agent-fox-dev/spec/envelope"

// The shape of the JSON a command writes is defined in package envelope,
// which is public so that a program reading the output can decode it into the
// same types. The shell builds the output from them under these names.

// Exit codes. Something other than a human reads them.
const (
	ExitOK     = envelope.ExitOK
	ExitFailed = envelope.ExitFailed
	ExitUsage  = envelope.ExitUsage
)

// StatusFor maps an exit code to its status string.
func StatusFor(code int) string { return envelope.StatusFor(code) }

// Envelope is the object a command writes, with its result of whatever type
// the command produces.
type Envelope = envelope.Envelope[any]

type (
	ErrorInfo      = envelope.ErrorInfo
	FixHint        = envelope.FixHint
	Warning        = envelope.Warning
	WarnCode       = envelope.WarnCode
	Artifact       = envelope.Artifact
	ArtifactKind   = envelope.ArtifactKind
	SideEffect     = envelope.SideEffect
	Next           = envelope.Next
	PreflightCheck = envelope.PreflightCheck
	Estimate       = envelope.Estimate
	InputInfo      = envelope.InputInfo
	ModelInfo      = envelope.ModelInfo
	PhaseModelInfo = envelope.PhaseModelInfo
	UsageInfo      = envelope.UsageInfo
	PhaseInfo      = envelope.PhaseInfo
)

// The artifact kinds the commands produce.
const (
	ArtifactSpecPackage  = envelope.ArtifactSpecPackage
	ArtifactComment      = envelope.ArtifactComment
	ArtifactMarkdownFile = envelope.ArtifactMarkdownFile
)

// SameInputPlaceholder stands for the original input in a suggested
// invocation whose input may be arbitrarily large (raw text, stdin).
const SameInputPlaceholder = envelope.SameInputPlaceholder

// RetryableFor reports whether an error of the given category can succeed if
// re-run unchanged.
func RetryableFor(category string) bool { return envelope.RetryableFor(category) }
