package envelope

// EvidenceCheck is the answer of speca's verify form: every test recorded as
// evidence for a done task, run again on the tree as it is.
type EvidenceCheck struct {
	Verdict string              `json:"verdict" description:"pass when every test is pass, fail when one is not, not_run when no done task has evidence or the tests could not be run"`
	Reason  string              `json:"reason,omitempty" description:"not_run: why no test was checked; absent otherwise"`
	Tasks   []EvidenceCheckTask `json:"tasks" description:"each done task that has evidence, in plan order, with its tests' statuses; [] when none was checked"`
	Log     string              `json:"log,omitempty" description:"the log of the run, under the gate's state directory; absent when no test ran or the log could not be written"`
}

// EvidenceCheckTask is one done task of an evidence check.
type EvidenceCheckTask struct {
	ID       int                  `json:"id" description:"the task id"`
	Evidence []EvidenceCheckEntry `json:"evidence" description:"each entry of its evidence"`
}

// EvidenceCheckEntry is one entry of a task's evidence, checked.
type EvidenceCheckEntry struct {
	Covers string              `json:"covers" description:"the criterion (for the integration task, the path) the entry covers"`
	Tests  []EvidenceCheckTest `json:"tests" description:"its tests, each with its status"`
}

// EvidenceCheckTest is one test of an evidence check and its status.
type EvidenceCheckTest struct {
	Test   string `json:"test" description:"the test identifier"`
	Status string `json:"status" description:"pass, fail, error, missing or skipped"`
}
