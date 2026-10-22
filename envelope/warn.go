package envelope

// WarnCode identifies one warning cause. It is an open set: a decoder must
// accept a code it has no constant for.
type WarnCode string

// The warning codes the commands raise.
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
	WarnPRDReviewIncomplete      WarnCode = "prd_review_incomplete"
	// speca's task form.
	WarnTaskNotReady    WarnCode = "task_not_ready"
	WarnTaskStateForced WarnCode = "task_state_forced"
	// specr's codes.
	WarnSpecInvalid   WarnCode = "spec_invalid"
	WarnSectionAbsent WarnCode = "section_absent"
	WarnOverBudget    WarnCode = "over_budget"
)
