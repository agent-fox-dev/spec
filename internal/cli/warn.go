package cli

// WarnCode identifies one warning cause. Every declared code has exactly one
// stage in warnStages, so a code cannot be used without a stage, and cannot
// be recorded against two different stages by accident.
type WarnCode string

// The declared warning codes. A call site that fits none of them gets a new
// constant here and in warnStages, never a bare string.
const (
	WarnInputTruncated           WarnCode = "input_truncated"
	WarnInputLooksLikePath       WarnCode = "input_looks_like_path"
	WarnCommentsUnreadable       WarnCode = "comments_unreadable"
	WarnCommentsTruncated        WarnCode = "comments_truncated"
	WarnEffortClamped            WarnCode = "effort_clamped"
	WarnCodeSearchUnavailable    WarnCode = "code_search_unavailable"
	WarnSpecsDirUnreadable       WarnCode = "specs_dir_unreadable"
	WarnProjectLanguageUnknown   WarnCode = "project_language_unknown"
	WarnNameFlagIgnored          WarnCode = "name_flag_ignored"
	WarnRepoMapBuildFailed       WarnCode = "repo_map_build_failed"
	WarnScopeRenamed             WarnCode = "scope_renamed"
	WarnScopeCountMismatch       WarnCode = "scope_count_mismatch"
	WarnRelevantFilesUnavailable WarnCode = "relevant_files_unavailable"
	WarnToolErrors               WarnCode = "tool_errors"
	WarnSplitPlanForeign         WarnCode = "split_plan_foreign"
	WarnSplitPlanUnreadable      WarnCode = "split_plan_unreadable"
	WarnSplitPlanStale           WarnCode = "split_plan_stale"
	WarnSplitPlanUpdateFailed    WarnCode = "split_plan_update_failed"
	WarnSplitPlanNotRemoved      WarnCode = "split_plan_not_removed"
	WarnArchitectureNotWritten   WarnCode = "architecture_not_written"
	WarnActivationFailed         WarnCode = "activation_failed"
	WarnCommentNotPosted         WarnCode = "comment_not_posted"
	WarnOutputNotWritten         WarnCode = "output_not_written"
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
}

// WarnStage returns the stage a code is recorded against, and whether the
// code is declared.
func WarnStage(code WarnCode) (string, bool) {
	s, ok := warnStages[code]
	return s, ok
}
