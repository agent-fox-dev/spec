package envelope

// AdminResult is speca's result, for every form of the command: the report on
// one package, the verdict on one package (--validate), the list of every
// package (--list), the render form (--render), the migrate form (--migrate)
// and the task and status forms, which are the report on the package after a
// change, with the changes they made. A form fills the fields it produces and
// the rest are absent: Valid is a pointer so that false is written, and the slices that
// must show as [] in their own form are omitted when nil rather than empty.
type AdminResult struct {
	SpecDir          string            `json:"spec_dir,omitempty" description:"the package directory, relative to --dir when inside it, absolute otherwise; runnable as the input of specr and speca; the report and the verdict"`
	SpecID           string            `json:"spec_id,omitempty" description:"the spec id, from the PRD frontmatter; the report and the verdict"`
	SpecName         string            `json:"spec_name,omitempty" description:"the spec name, from the PRD frontmatter; the report and the verdict"`
	Title            string            `json:"title,omitempty" description:"the spec title, from the PRD frontmatter; the report and the verdict"`
	Status           string            `json:"status,omitempty" description:"the package's lifecycle status: draft, active, sealed, superseded or archived; the report and the verdict"`
	CreatedAt        string            `json:"created_at,omitempty" description:"when the package was created, RFC 3339, from the PRD frontmatter; the report and the verdict"`
	UpdatedAt        string            `json:"updated_at,omitempty" description:"when a task of the package last changed state, RFC 3339, from progress.json; absent when none has; the report and the verdict"`
	Profile          string            `json:"profile,omitempty" description:"the package's profile, full or lite, from the PRD frontmatter; absent when the frontmatter names none, which is full"`
	IntentHash       string            `json:"intent_hash,omitempty" description:"the SHA-256 of the PRD's ## Intent section, recorded when the package was activated; absent while unset"`
	ContractHash     map[string]string `json:"contract_hash,omitempty" description:"the SHA-256 of each contract file, keyed requirements.json and tasks.json, over its canonical encoding without extension keys, recorded when the package was activated; absent for a draft and for a package activated without it; the report and the verdict"`
	ContractDrift    *bool             `json:"contract_drift,omitempty" description:"for a package with a contract_hash, true when requirements.json or tasks.json no longer hashes to it, so that every task change, every evidence call and sealing are refused, and false when both match; absent for a package without a contract_hash; the report and the verdict"`
	EvidenceRequired bool              `json:"evidence_required,omitempty" description:"true when the package is active and has a contract_hash, so that each task closes on evidence: a test per criterion seen failing, then passing; absent otherwise; the report and the verdict"`
	Supersedes       []string          `json:"supersedes,omitempty" description:"the spec ids this package supersedes; absent when it supersedes none"`

	Valid        *bool    `json:"valid,omitempty" description:"whether the package validates. In the report, its own verdict, from the schemas and the cross-file rules C1 to C8; with --validate, the combined verdict: true exactly when no error was kept from the package's own rules or the cross-spec rules. Absent under --list, where each entry holds its own"`
	FailingRules []string `json:"failing_rules,omitempty" description:"the rules the package's errors break, in order of first appearance; the report only, and present only when valid is false"`
	IntentDrift  bool     `json:"intent_drift,omitempty" description:"true when the package is active, has an intent_hash, and its PRD's ## Intent section no longer hashes to it or is gone, so that every save is refused; absent otherwise; the report and the verdict"`

	Tasks         []TaskInfo  `json:"tasks,omitzero" description:"every task of the plan, in order, the derived integration task last, with its state and whether it is ready; [] when the plan has none; the report only"`
	TaskCounts    *TaskCounts `json:"task_counts,omitempty" description:"the tasks counted by state, the integration task among them, and their total; the report only"`
	NextTask      *TaskInfo   `json:"next_task,omitempty" description:"the task to work on now: the first in_progress task, else the first ready one; absent when there is none; the report only"`
	AllowedStatus []string    `json:"allowed_status,omitzero" description:"the lifecycle states the package may move to from its status, in the order draft, active, sealed, superseded, archived; [] for a terminal status; the report only"`
	DependsOn     []string    `json:"depends_on,omitzero" description:"the spec ids in the package's own tasks.json dependencies, in file order without duplicates; [] when it has none; the report only"`
	Dependents    []string    `json:"dependents,omitzero" description:"the spec ids of the other packages under the spec root that depend on this one, sorted without duplicates; [] when none does, absent when the spec root could not be read; the report only"`

	Validation   *ValidationReport `json:"validation,omitempty" description:"the format's verdict: valid, error_count, warning_count, and every error and warning with its check (the rule), artifact, entity and message; the package's own findings first, then the cross-spec findings that concern it; --validate only"`
	Traceability *TraceReport      `json:"traceability,omitempty" description:"the derived coverage: criteria_covered, criteria_uncovered, paths_covered, paths_uncovered and criteria_unowned; --validate only"`
	Hints        []ValidationItem  `json:"hints,omitzero" description:"the authoring hints, which are not the format's rules and never change valid: a vague word in a criterion (vague_language), a step or touches entry naming a line of a file (line_reference), and a test command from an ecosystem the repository does not have (foreign_test_command), each with its check, artifact, entity and message; [] when there are none; --validate only"`

	Changes     []Change `json:"changes,omitempty" description:"what the call changed, each with its kind, what it left and what it entered: the tasks in plan order for the task form; status, then supersedes, then moved for the status form, in the order they were written; with --dry-run, what it would have changed; absent when nothing changed; the task and status forms only"`
	EvidenceLog string   `json:"evidence_log,omitempty" description:"the log of the evidence command's run, its stdout and stderr, under the gate's state directory; absent when no test ran or the log could not be written; the evidence form and a close on evidence"`
	DryRun      bool     `json:"dry_run,omitempty" description:"true under --dry-run: every check ran and nothing was written, and the result is what the call would have produced; the task and status forms only"`

	Packages []PackageInfo `json:"packages,omitzero" description:"one entry for every package under the spec root in number order, then with --all those under archive/; [] when there are none; --list only"`

	Rendered []RenderedInfo `json:"rendered,omitzero" description:"what --render did to each package's spec.md: the one package named, or with no reference every package under the spec root in number order, then with --all those under archive/; [] when the spec root holds none; with --dry-run, what it would have done; --render only"`

	Migrated []MigratedInfo `json:"migrated,omitzero" description:"what --migrate did to each package: the one package named, or with no reference every package under the spec root in number order, then with --all those under archive/; [] when the spec root holds none; with --dry-run, what it would have done; --migrate only"`

	NextSpec *PackageInfo  `json:"next_spec,omitempty" description:"the package to implement now: of the packages under the spec root that are open (draft or active, with a task pending or in progress), the first in dependency order, ties broken by number, whose every dependency is finished; an entry as --list writes it; absent when there is none; --next only"`
	AllDone  *bool         `json:"all_done,omitempty" description:"true when there is nothing left to implement: no package under the spec root is open and every one of them loads as version 3; when it is false and next_spec is absent, every open package waits on something, and waiting says on what; always written under --next"`
	Waiting  []WaitingInfo `json:"waiting,omitzero" description:"the packages under the spec root that are open but cannot start yet, or cannot be judged, in number order, each with why; [] when there are none; --next only"`

	Gate *GateResult `json:"gate,omitempty" description:"the gate's verdict: its level, pass, fail or not_run, every command that ran with its exit code, seconds and log, a failing command's tail, a reused pass, and for a full gate the slowest units; --gate only"`

	EvidenceCheck *EvidenceCheck `json:"evidence_check,omitempty" description:"the verify form's answer: its verdict (pass when every test recorded for a done task passes on the tree as it is, fail when one does not, not_run with reason when no done task has evidence or the tests could not be run), the log of the run, and each done task with each covers and its tests' statuses; --verify-evidence only"`

	GateReport *GateReport `json:"gate_report,omitempty" description:"the package's gate runs since a commit: the full and task runs with their seconds and how many were reused, the full gate's seconds at the baseline and on the last tree, and the last full gate's slowest units; --gate-report only"`
}

// WaitingInfo is one entry of AdminResult.Waiting: a package under the spec
// root that speca --next did not choose, and why it could not.
type WaitingInfo struct {
	SpecDir string   `json:"spec_dir" description:"the package directory, relative to --dir when inside it, absolute otherwise; runnable as the input of specr and speca"`
	SpecID  string   `json:"spec_id,omitempty" description:"the spec id, from the PRD frontmatter; absent when the package did not load"`
	WaitsOn []string `json:"waits_on,omitempty" description:"the spec ids of its dependencies that are not finished, or that no package under the spec root or archive/ has, in the order its tasks.json names them; absent when it waits on nothing but itself (it does not load, or is in another format version)"`
	Reason  string   `json:"reason" description:"why it cannot start, in words: the dependencies it waits on, or that it does not load, or that it is in another format version"`
}

// MigratedInfo is one entry of AdminResult.Migrated: a package, and what the
// --migrate form did to it.
type MigratedInfo struct {
	SpecDir     string `json:"spec_dir" description:"the package directory, relative to --dir when inside it, absolute otherwise; runnable as the input of specr and speca"`
	SpecID      string `json:"spec_id,omitempty" description:"the spec id, from the PRD frontmatter; absent when the package did not load"`
	SpecName    string `json:"spec_name,omitempty" description:"the spec name, from the PRD frontmatter; absent when the package did not load"`
	Status      string `json:"status,omitempty" description:"the package's lifecycle status; absent when the package did not load"`
	FromVersion int    `json:"from_version,omitempty" description:"the schema_version the package declared before the call; absent when the package did not load"`
	Migrated    string `json:"migrated" description:"written (the package was version 2 and was converted to version 3 in place), unchanged (it is version 3 already, and was left alone) or failed (it is neither, it could not be read, it has a gap the conversion refuses, or it could not be written: reason says why); under --dry-run, what would have happened"`
	Reason      string `json:"reason,omitempty" description:"why the package failed, in the library's or the file system's words; present exactly when migrated is failed"`
	Archived    bool   `json:"archived,omitempty" description:"true for a package migrated from archive/ with --all; absent otherwise"`
}

// RenderedInfo is one entry of AdminResult.Rendered: a package, and what the
// --render form did to its spec.md, the render every save writes beside the
// package's artifacts.
type RenderedInfo struct {
	SpecDir  string `json:"spec_dir" description:"the package directory, relative to --dir when inside it, absolute otherwise; runnable as the input of specr and speca"`
	SpecID   string `json:"spec_id,omitempty" description:"the spec id, from the PRD frontmatter; absent when the package did not load"`
	SpecName string `json:"spec_name,omitempty" description:"the spec name, from the PRD frontmatter; absent when the package did not load"`
	Status   string `json:"status,omitempty" description:"the package's lifecycle status; absent when the package did not load"`
	Rendered string `json:"rendered" description:"written (spec.md was absent or differed from the render, and was written), unchanged (it was the render already, and was left alone) or failed (the package could not be loaded, or spec.md could not be written: reason says why); under --dry-run, what would have happened"`
	Reason   string `json:"reason,omitempty" description:"why the package failed, in the library's or the file system's words; present exactly when rendered is failed"`
	Archived bool   `json:"archived,omitempty" description:"true for a package rendered from archive/ with --all; absent otherwise"`
}

// TaskCounts is a plan's tasks counted by state.
type TaskCounts struct {
	Pending    int `json:"pending" description:"the tasks that are pending"`
	InProgress int `json:"in_progress" description:"the tasks that are in progress"`
	Done       int `json:"done" description:"the tasks that are done"`
	Dropped    int `json:"dropped" description:"the tasks that are dropped"`
	Total      int `json:"total" description:"every task of the plan: the four counts added"`
}

// PackageInfo is one entry of AdminResult.Packages. A package that did not
// load holds spec_dir and load_error and nothing else.
type PackageInfo struct {
	SpecDir    string      `json:"spec_dir" description:"the package directory, relative to --dir when inside it, absolute otherwise; runnable as the input of specr and speca"`
	SpecID     string      `json:"spec_id,omitempty" description:"the spec id, from the PRD frontmatter; absent when the package did not load"`
	SpecName   string      `json:"spec_name,omitempty" description:"the spec name, from the PRD frontmatter; absent when the package did not load"`
	Title      string      `json:"title,omitempty" description:"the spec title, from the PRD frontmatter; absent when the package did not load"`
	Status     string      `json:"status,omitempty" description:"the package's lifecycle status; absent when the package did not load"`
	Valid      *bool       `json:"valid,omitempty" description:"whether the package validates on its own rules; absent when the package did not load"`
	TaskCounts *TaskCounts `json:"task_counts,omitempty" description:"the tasks counted by state, the integration task among them; absent when the package did not load"`
	NextTask   *TaskInfo   `json:"next_task,omitempty" description:"the task to work on now: the first in_progress task, else the first ready one; absent when there is none"`
	Archived   bool        `json:"archived,omitempty" description:"true for a package listed from archive/, whatever its status (a superseded package there is archived: true with status superseded); absent otherwise"`
	LoadError  string      `json:"load_error,omitempty" description:"why the package did not load, in the library's words; present exactly when it did not, and then the only field besides spec_dir"`
}

// Change is one change a call made to a package, or under --dry-run would have
// made. The set of kinds is open. speca's task form writes the kind task. Its
// status form writes status (the package's lifecycle status, from and to),
// supersedes (the replacing package's spec, and the id added to its supersedes
// list) and moved (the package's directory, from and to), in the order it made
// them.
type Change struct {
	Kind   string `json:"kind,omitempty" description:"what changed: task (the state of a task), evidence (the evidence recorded for a task), status (the package's lifecycle status), supersedes (an id added to the supersedes list of the replacing package) or moved (the package's directory); the set is open"`
	ID     int    `json:"id,omitempty" description:"task and evidence: the id of the task that changed"`
	From   string `json:"from,omitempty" description:"task: the state it was in (pending, in_progress, done or dropped); status: the status it was in; moved: the directory it was in"`
	To     string `json:"to,omitempty" description:"task: the state it was set to; status: the status it was set to; moved: the directory it was moved to"`
	Forced bool   `json:"forced,omitempty" description:"task: true when --force set a state the task state machine would have refused; absent otherwise"`
	Spec   string `json:"spec,omitempty" description:"supersedes: the spec_id of the replacing package, whose supersedes list was changed"`
	Added  string `json:"added,omitempty" description:"supersedes: the spec_id that was added to that list, the package that was superseded"`
}
