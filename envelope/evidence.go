package envelope

// TaskEvidence is one entry of a task's evidence in speca's report: a
// criterion the task owns (for the integration task, an execution path), the
// tests that cover it and the runs speca recorded of each.
type TaskEvidence struct {
	Covers string         `json:"covers" description:"the id of a criterion the task owns, or for the integration task of an execution path"`
	NoRed  string         `json:"no_red,omitempty" description:"the exemption: why the entry's tests need not be seen failing first; absent when there is none"`
	Tests  []EvidenceTest `json:"tests" description:"the tests that cover it, each with the runs recorded of it"`
}

// EvidenceTest is one test of an evidence entry, with the run in which speca
// saw it fail and the run in which it saw it pass, each absent until
// recorded.
type EvidenceTest struct {
	Test   string       `json:"test" description:"the test identifier, as the evidence command takes it"`
	Failed *EvidenceRun `json:"failed,omitempty" description:"the run in which speca saw the test fail; absent until then, and for an exempt test or one of the integration task"`
	Passed *EvidenceRun `json:"passed,omitempty" description:"the run in which speca saw the test pass when the task closed; absent until then"`
}

// EvidenceRun is when a run of a test was, and on which tree.
type EvidenceRun struct {
	At   string `json:"at" description:"when the run was, RFC 3339"`
	Tree string `json:"tree,omitempty" description:"the git tree id of the working tree the run was on, as the gate computes it; absent outside a git work tree"`
}
