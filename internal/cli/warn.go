package cli

import "github.com/agent-fox-dev/spec/envelope"

// The declared warning codes, which are defined in package envelope so that a
// program reading the output can name them. Every declared code has exactly
// one stage in warnStages, so a code cannot be used without a stage, and
// cannot be recorded against two different stages by accident. A call site
// that fits none of them gets a new constant in package envelope and in
// warnStages, never a bare string.
const (
	WarnInputTruncated           = envelope.WarnInputTruncated
	WarnInputLooksLikePath       = envelope.WarnInputLooksLikePath
	WarnCommentsUnreadable       = envelope.WarnCommentsUnreadable
	WarnCommentsTruncated        = envelope.WarnCommentsTruncated
	WarnEffortClamped            = envelope.WarnEffortClamped
	WarnCodeSearchUnavailable    = envelope.WarnCodeSearchUnavailable
	WarnSpecsDirUnreadable       = envelope.WarnSpecsDirUnreadable
	WarnProjectLanguageUnknown   = envelope.WarnProjectLanguageUnknown
	WarnNameFlagIgnored          = envelope.WarnNameFlagIgnored
	WarnRepoMapBuildFailed       = envelope.WarnRepoMapBuildFailed
	WarnScopeRenamed             = envelope.WarnScopeRenamed
	WarnScopeCountMismatch       = envelope.WarnScopeCountMismatch
	WarnRelevantFilesUnavailable = envelope.WarnRelevantFilesUnavailable
	WarnToolErrors               = envelope.WarnToolErrors
	WarnSplitPlanForeign         = envelope.WarnSplitPlanForeign
	WarnSplitPlanUnreadable      = envelope.WarnSplitPlanUnreadable
	WarnSplitPlanStale           = envelope.WarnSplitPlanStale
	WarnSplitPlanUpdateFailed    = envelope.WarnSplitPlanUpdateFailed
	WarnSplitPlanNotRemoved      = envelope.WarnSplitPlanNotRemoved
	WarnArchitectureNotWritten   = envelope.WarnArchitectureNotWritten
	WarnActivationFailed         = envelope.WarnActivationFailed
	WarnCommentNotPosted         = envelope.WarnCommentNotPosted
	WarnOutputNotWritten         = envelope.WarnOutputNotWritten
	WarnPRDReviewIncomplete      = envelope.WarnPRDReviewIncomplete
	WarnSpecInvalid              = envelope.WarnSpecInvalid
	WarnSectionAbsent            = envelope.WarnSectionAbsent
	WarnOverBudget               = envelope.WarnOverBudget
	WarnTaskNotReady             = envelope.WarnTaskNotReady
	WarnTaskStateForced          = envelope.WarnTaskStateForced
)

// warnStages is the one place a code's stage is decided.
var warnStages = map[WarnCode]string{
	WarnInputTruncated:           "input",
	WarnInputLooksLikePath:       "input",
	WarnCommentsUnreadable:       "input",
	WarnCommentsTruncated:        "input",
	WarnEffortClamped:            "preflight",
	WarnCodeSearchUnavailable:    "preflight",
	WarnSpecsDirUnreadable:       "preflight",
	WarnProjectLanguageUnknown:   "preflight",
	WarnNameFlagIgnored:          "usage",
	WarnRepoMapBuildFailed:       "repo_map",
	WarnScopeRenamed:             "prd",
	WarnScopeCountMismatch:       "prd",
	WarnRelevantFilesUnavailable: "prd",
	WarnToolErrors:               "phase",
	WarnSplitPlanForeign:         "split",
	WarnSplitPlanUnreadable:      "split",
	WarnSplitPlanStale:           "split",
	WarnSplitPlanUpdateFailed:    "split",
	WarnSplitPlanNotRemoved:      "split",
	WarnArchitectureNotWritten:   "write",
	WarnActivationFailed:         "activate",
	WarnCommentNotPosted:         "report",
	WarnOutputNotWritten:         "emit",
	WarnPRDReviewIncomplete:      "prd_review",
	WarnSpecInvalid:              "validate",
	WarnSectionAbsent:            "render",
	WarnOverBudget:               "render",
	WarnTaskNotReady:             "task",
	WarnTaskStateForced:          "task",
}

// WarnStage returns the stage a code is recorded against, and whether the
// code is declared.
func WarnStage(code WarnCode) (string, bool) {
	s, ok := warnStages[code]
	return s, ok
}
