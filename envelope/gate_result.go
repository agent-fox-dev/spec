package envelope

// The gate's levels and verdicts, as speca's gate form writes them.
const (
	GateLevelTask = "task"
	GateLevelFull = "full"

	GateVerdictPass   = "pass"
	GateVerdictFail   = "fail"
	GateVerdictNotRun = "not_run"
)

// GateResult is the verdict of speca's gate form (--gate): the package's
// linter command, then its all_tests command, run in the repository, or a pass
// recorded for the same tree and reused.
type GateResult struct {
	Level    string        `json:"level" description:"the gate's level: task (the gate after an implement task) or full (the baseline, after the integration task, after the conformance fixes and on the merged tree); both run linter, then all_tests"`
	Verdict  string        `json:"verdict" description:"pass (every command exited 0, or a recorded pass was reused), fail (a command exited with another code or ran past --timeout) or not_run (a command could not start: its program was not found, its string could not be split, or test_commands names no such command); exit code 0 carries every verdict"`
	CleanEnv bool          `json:"clean_env" description:"true when the commands ran with --clean-env: an empty temporary HOME, no global or system git configuration and GIT_TERMINAL_PROMPT=0"`
	Reason   string        `json:"reason,omitempty" description:"why the gate is not_run: the command that could not start and why; present exactly when verdict is not_run"`
	Reused   *GateReuse    `json:"reused,omitempty" description:"the recorded pass this gate returned without running anything, because the tree, the directory of it the commands run in, the level, the two command strings and the clean environment matched it; absent when the commands ran"`
	Commands []GateCommand `json:"commands" description:"one entry per command that ran, in run order: linter, then all_tests; the gate stops at the first that does not exit 0, and a command that could not start has no entry; [] when a recorded pass was reused"`
	Seconds  float64       `json:"seconds" description:"the gate's wall-clock seconds, the sum of its commands' seconds, to the millisecond; 0 when a recorded pass was reused"`
	Slowest  []GateUnit    `json:"slowest,omitzero" description:"a full gate only: the five units of the all_tests output with the most seconds, slowest first, equal ones in output order; a unit is a Go test package (ok or FAIL lines with a duration); [] when the output holds none; for a reused pass, the recorded run's list; absent for a task gate"`
}

// GateCommand is one command of a gate that ran.
type GateCommand struct {
	Name     string  `json:"name" description:"which command it is: linter or all_tests"`
	ExitCode *int    `json:"exit_code,omitempty" description:"the code the command exited with; absent when it ran past --timeout and was ended"`
	Seconds  float64 `json:"seconds" description:"the command's wall-clock seconds, to the millisecond"`
	Log      string  `json:"log,omitempty" description:"the absolute path of the file holding everything the command printed, stdout and stderr together, under the state directory; absent when the file could not be written"`
	Tail     *string `json:"tail,omitempty" description:"the last 40 lines of the command's output, all of them when it printed fewer, an empty string when it printed nothing; present exactly when the command exited with a code other than 0 or was ended by the timeout"`
	TimedOut bool    `json:"timed_out,omitempty" description:"true when the command was still running when --timeout had passed, and was ended with every process it started; absent otherwise"`
}

// GateReuse names the recorded pass a gate was answered from.
type GateReuse struct {
	Commit  string  `json:"commit" description:"the commit that was checked out when the recorded gate ran; empty when the repository had no commit"`
	At      string  `json:"at" description:"when the recorded gate ran, RFC 3339"`
	Seconds float64 `json:"seconds" description:"the recorded gate's own seconds"`
}

// GateUnit is one unit a test tool reported a duration for: for Go, a test
// package.
type GateUnit struct {
	Unit    string  `json:"unit" description:"the unit's name: for Go, the import path of the test package"`
	Seconds float64 `json:"seconds" description:"the seconds the tool reported for it"`
}

// GateReport is the result of speca's gate report (--gate-report): the gate
// runs recorded for one package since a commit, summed for the merge commit.
type GateReport struct {
	Since               string     `json:"since" description:"the full id of the commit the runs are counted from: --since, or the merge base of HEAD and the base branch (the branch refs/remotes/origin/HEAD names, else main)"`
	Full                GateRuns   `json:"full" description:"the full gates counted"`
	Task                GateRuns   `json:"task" description:"the task gates counted"`
	BaselineFullSeconds *float64   `json:"baseline_full_seconds,omitempty" description:"the seconds of the earliest full gate counted, the baseline; a reused gate's are those of the run it came from; absent when no full gate is counted"`
	LastFullSeconds     *float64   `json:"last_full_seconds,omitempty" description:"the seconds of the latest full gate counted, the last tree; a reused gate's are those of the run it came from; absent when no full gate is counted"`
	Slowest             []GateUnit `json:"slowest" description:"the slowest units of the latest full gate counted, as its verdict held them; [] when none is counted or it held none"`
}

// GateRuns counts the gates of one level a report counted: the runs at the
// commit and at its descendants on the branch, that ended in pass or fail.
type GateRuns struct {
	Runs    int     `json:"runs" description:"the gates counted, reused ones included"`
	Seconds float64 `json:"seconds" description:"the sum of the gates' own seconds, a reused gate adding 0"`
	Reused  int     `json:"reused" description:"how many of them were answered from a recorded pass"`
}
