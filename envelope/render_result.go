package envelope

// The sections a render is made of, in the order the format renders them.
// specr's --section takes these names, and RenderResult.Sections lists them.
const (
	SectionPRD          = "prd"
	SectionArchitecture = "architecture"
	SectionRequirements = "requirements"
	SectionTestSpec     = "test_spec"
	SectionTasks        = "tasks"
)

// Sections is every section name, in the order the format renders them.
func Sections() []string {
	return []string{SectionPRD, SectionArchitecture, SectionRequirements, SectionTestSpec, SectionTasks}
}

// What --max-tokens can drop, in the order it drops it. They are the values
// of RenderResult.Reduced.
const (
	ReducedArchitecture = "architecture"
	ReducedTestDetail   = "test_detail"
)

// The scopes of a render, the values of RenderResult.Scope.
const (
	ScopeSpec = "spec"
	ScopeTask = "task"
)

// RenderResult is specr's result: what was rendered, the plan's tasks, and the
// Markdown itself, last, so a reader meets what it is before what it says.
type RenderResult struct {
	SpecDir   string     `json:"spec_dir" description:"the package directory, relative to --dir when inside it; runnable as specr's input"`
	SpecID    string     `json:"spec_id" description:"the spec id, from the PRD frontmatter"`
	SpecName  string     `json:"spec_name" description:"the spec name, from the PRD frontmatter"`
	Title     string     `json:"title" description:"the spec title, from the PRD frontmatter"`
	Status    string     `json:"status" description:"the package's lifecycle status: draft, active, sealed, superseded or archived"`
	Valid     bool       `json:"valid" description:"whether the package passes the format's validation; an invalid package still renders in full, and a spec_invalid warning names the failing rules"`
	Scope     string     `json:"scope" description:"what was rendered: spec (the whole package) or task (the package scoped to one task)"`
	Task      *TaskInfo  `json:"task,omitempty" description:"the task the render is scoped to; present exactly when scope is task"`
	Sections  []string   `json:"sections" description:"the sections the Markdown holds, in order; a subset of prd, architecture, requirements, test_spec, tasks"`
	MaxTokens int        `json:"max_tokens,omitempty" description:"the --max-tokens budget the render was held to; absent when there was none"`
	Reduced   []string   `json:"reduced,omitempty" description:"the detail --max-tokens dropped to fit, in the order it is dropped: architecture (the whole section), test_detail (each test's given, when, real components and pseudocode); absent when nothing was dropped"`
	Tokens    int        `json:"tokens" description:"the estimated token count of markdown (bytes / 4)"`
	Tasks     []TaskInfo `json:"tasks" description:"every task of the plan, in order: the ids --task takes"`
	// MarkdownFile is where --markdown-file wrote the Markdown.
	MarkdownFile string `json:"markdown_file,omitempty" description:"the absolute path --markdown-file wrote the Markdown to"`
	Markdown     string `json:"markdown" description:"the rendered Markdown"`
}

// TaskInfo is one task of the plan, by id, without its body.
type TaskInfo struct {
	ID        int    `json:"id" description:"the task id, as --task takes it"`
	Title     string `json:"title" description:"the task title"`
	Kind      string `json:"kind" description:"implement, or integration for the plan's last task"`
	State     string `json:"state" description:"pending, in_progress, done or dropped"`
	Optional  bool   `json:"optional,omitempty" description:"true when the task may be skipped"`
	DependsOn []int  `json:"depends_on,omitempty" description:"the tasks that must be done first"`
	Ready     bool   `json:"ready,omitempty" description:"true for a pending task whose dependencies are all done; absent otherwise"`
}
