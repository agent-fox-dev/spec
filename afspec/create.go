package afspec

// Schema URIs for each artifact type (format version 3).
const (
	requirementsSchemaURI = "https://agent-fox.dev/schemas/requirements.v3.json"
	tasksSchemaURI        = "https://agent-fox.dev/schemas/tasks.v3.json"
	progressSchemaURI     = "https://agent-fox.dev/schemas/progress.v3.json"
)

// SchemaVersion is the format version this library reads and writes.
const SchemaVersion = 3

// The two profiles of a package (format v3 §4.1). ProfileFull is the default
// when the frontmatter names none.
const (
	ProfileFull = "full"
	ProfileLite = "lite"
)

// CreateSpec creates a new draft Spec whose two contract artifacts carry a
// valid $schema, spec_id, spec_name and schema_version so that Save + LoadSpec
// round-trips. The artifact bodies are empty: a spec with no requirements and
// no tasks is *incomplete*, not schema-invalid, and Validate reports it as
// such (§10, bootstrap mode) until a generation run fills it in. The progress
// is empty too: every task is pending until a save writes progress.json.
//
// Validation of specID and specName is deferred to Spec.Validate.
func CreateSpec(specID, specName string) *Spec {
	return &Spec{
		SpecID:        specID,
		SpecName:      specName,
		Status:        "draft",
		SchemaVersion: SchemaVersion,
		Requirements: &RequirementsV3Json{
			Schema:         requirementsSchemaURI,
			SpecId:         specID,
			SpecName:       specName,
			SchemaVersion:  SchemaVersion,
			Requirements:   []Requirement{},
			ExecutionPaths: []ExecutionPath{},
		},
		Tasks: &TasksV3Json{
			Schema:        tasksSchemaURI,
			SpecId:        specID,
			SpecName:      specName,
			SchemaVersion: SchemaVersion,
			TestCommands:  TestCommands{},
			Dependencies:  []Dependency{},
			Tasks:         []Task{},
		},
	}
}

// IsScaffold reports whether the spec is still an empty scaffold: no
// requirements and no tasks. A scaffold is incomplete rather than invalid,
// which is what lets a tool save a spec before a generation run has produced
// its content.
func (s *Spec) IsScaffold() bool {
	return s.Requirements != nil && len(s.Requirements.Requirements) == 0 &&
		s.Tasks != nil && len(s.Tasks.Tasks) == 0
}

// IsLite reports whether the package carries the lite profile: at most three
// requirements, no execution path, no smoke check and no integration task
// (format v3 §4.1). A package that names no profile is full.
func (s *Spec) IsLite() bool { return s.Profile == ProfileLite }
