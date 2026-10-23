package specgen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/issuex"
	"github.com/agent-fox-dev/spec/internal/project"
	"github.com/agent-fox-dev/spec/internal/repomap"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// DefaultSpecDirName is where spec packages live under the repository root.
const DefaultSpecDirName = ".specs"

// SpecDirEnv overrides the spec root. The --specs-dir flag wins over it.
const SpecDirEnv = "AF_SPEC_DIR"

// Options configure one spec generation run.
type Options struct {
	// Input is the classified argument: a product idea, a PRD file, or a
	// forge issue thread. Required.
	Input cli.Input
	// Workspace roots the read-only source access. Required.
	Workspace *workspace.Workspace
	// SpecsDir is where NN_name directories live. Empty means
	// <workspace>/.specs, or $AF_SPEC_DIR when that is set.
	SpecsDir string
	// Name overrides the spec name the model chooses. It must match
	// [a-z][a-z0-9]*(_[a-z0-9]+)*.
	Name string
	// Architecture runs the optional fifth phase and writes architecture.md.
	Architecture bool
	// Activate transitions a valid spec from draft to active, computing its
	// intent hash. A spec that does not validate is never activated.
	Activate bool
	// Comment posts the finished PRD back to the issue the input came from.
	Comment bool
	// DryRun writes nothing to disk and nothing to the forge. The generated
	// artifacts are still produced and reported.
	DryRun bool
	// TotalBudgetUSD caps the spend of the whole run. It is checked before
	// each scope's PRD phase after the first; an input that does not split has no interior boundary and is
	// bound by the one phase's ceiling alone. Zero means no cap beyond the
	// per-phase bound.
	TotalBudgetUSD float64

	// Adversarial reviews every PRD the run writes before its package is
	// generated: a fresh agent looks for its flaws and another revises it to
	// answer them, for up to AdversarialRounds rounds.
	Adversarial bool
	// AdversarialRounds is the round limit, 1 to 5. The command passes 1
	// when --adversarial is given without --adversarial-rounds.
	AdversarialRounds int

	// MaxParallel is how many packages of a split are generated at once. The
	// PRD phases still run one after another, each seeing the PRDs before it;
	// a scope's package is generated as soon as its PRD exists, beside the
	// next scope's PRD phase. One or less writes the scopes strictly one after
	// another, each scope's PRD waiting for the package before it.
	MaxParallel int

	// RepoMapTokens is the token budget of the repository map in every
	// phase's user prompt. Zero, the zero value, disables it.
	RepoMapTokens int

	// Index is the code-search index for this run. Nil means no indexed search.
	Index tools.Index
	// IndexUnavailable is why Index is nil: the message of the error the index
	// builder returned. RunPreflight reports it. Empty when the
	// index was built or the reason is not known.
	IndexUnavailable string

	// Runner drives the model phases. Required unless author is injected.
	Runner *agentrun.Runner
	// Forge is the forge client, GitHub or GitLab, used only by Comment.
	Forge issuex.Client
	// Run records warnings and per-phase cost.
	Run *cli.Run
	// Progress reports steps to stderr.
	Progress *cli.Progress

	// author is the model half. It is unexported and injected by tests.
	author author
	// reviewer is the model half of the adversarial review. It is
	// unexported and injected by tests; nil means the phases against Runner.
	reviewer reviewer
	// buildMap builds the repository map. It is unexported and injected by
	// tests; nil means repomap.Build.
	buildMap func(ctx context.Context, ws *workspace.Workspace, budget int, inputPaths []string) (string, error)
}

// Result is what the tool reports as JSON: the envelope package's SpecResult,
// which a program reading the output decodes into, and the one thing the
// pipeline keeps beside it.
//
// The fields of the first package this run wrote sit at the top level, so a
// run on an input that is one spec's worth of work reports exactly what it
// always did. An input that was more than that adds the further packages in
// FollowOnSpecs and the whole plan, scope by scope, in Split.
type Result struct {
	envelope.SpecResult

	// inputRef is how a next[] entry names the input to re-run it: the
	// literal file path or issue URL when the input had one, and the
	// needs_human.resume placeholder for raw text or stdin. It is not part
	// of the JSON result.
	inputRef string
}

// Summary returns one sentence describing the outcome in spec's vocabulary,
// or none when the run produced no package to describe: the envelope then
// says what failed instead.
func (r Result) Summary() string {
	if r.Stage == "preflight" {
		return fmt.Sprintf("spec: preflight passed (%d checks)", len(r.Preflight))
	}
	var parts []string

	// Package(s) written / split progress
	if len(r.Split) > 0 {
		done := 0
		for _, s := range r.Split {
			if s.Status == ScopeDone {
				done++
			}
		}
		parts = append(parts, fmt.Sprintf("spec: split %d of %d scopes", done, len(r.Split)))
	} else {
		// A run that failed before it wrote a package has a spec id and
		// decisions, but no directory, and wrote nothing.
		if r.SpecDir == "" {
			return ""
		}
		if len(r.FollowOnSpecs) > 0 {
			parts = append(parts, fmt.Sprintf("spec: wrote %s (+%d follow-on packages)", r.SpecDir, len(r.FollowOnSpecs)))
		} else {
			parts = append(parts, fmt.Sprintf("spec: wrote %s", r.SpecDir))
		}
	}

	totalQuestions := len(r.OpenQuestions)
	for _, f := range r.FollowOnSpecs {
		totalQuestions += len(f.OpenQuestions)
	}
	if totalQuestions > 0 {
		switch {
		case totalQuestions == 1:
			parts = append(parts, "1 open question")
		case len(r.FollowOnSpecs) > 0:
			// result.open_questions is the first package's; the rest are
			// counted per scope in split[].
			parts = append(parts, fmt.Sprintf("%d open questions across %d packages (per package in split[])",
				totalQuestions, 1+len(r.FollowOnSpecs)))
		default:
			parts = append(parts, fmt.Sprintf("%d open questions", totalQuestions))
		}
	}

	if clause := reviewClause(append([]Package{r.Package}, r.FollowOnSpecs...)); clause != "" {
		parts = append(parts, clause)
	}

	return strings.Join(parts, "; ")
}

// reviewClause counts the reviews of every package of the run, or is empty
// when none was reviewed.
func reviewClause(pkgs []Package) string {
	reviewed := false
	var rounds, findings, accepted int
	for _, p := range pkgs {
		if p.Review == nil {
			continue
		}
		reviewed = true
		rounds += p.Review.RoundsRun
		findings += p.Review.FindingsCount
		accepted += p.Review.AcceptedCount
	}
	if !reviewed {
		return ""
	}
	clause := fmt.Sprintf("review: %s, %s", countOf(rounds, "round"), countOf(findings, "finding"))
	if findings > 0 {
		clause += fmt.Sprintf(", %d accepted", accepted)
	}
	return clause
}

// countOf is n and noun, plural unless n is 1.
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Resumable implements cli.Resumabler. For specgen, this is true whenever
// SplitPlan is non-empty.
func (r Result) Resumable() bool {
	if r.Stage == "preflight" {
		return false
	}
	return r.SplitPlan != ""
}

// The result's parts are defined in package envelope.
type (
	// Package describes one specification package.
	Package = envelope.Package
	// ScopeReport is one scope of a split and where it stands.
	ScopeReport = envelope.ScopeReport
	// ValidationReport is afspec's verdict, flattened for JSON.
	ValidationReport = envelope.ValidationReport
	// ValidationItem is one finding, carrying the rule that produced it.
	ValidationItem = envelope.ValidationItem
	// TraceReport is the derived coverage summary.
	TraceReport = envelope.TraceReport
	// Review is the adversarial review of one package's PRD.
	Review = envelope.Review
	// ReviewRound is one review and the revision that answered it.
	ReviewRound = envelope.ReviewRound
	// Finding is one flaw a reviewer reported and what became of it.
	Finding = envelope.ReviewFinding
)

// Scope states, as reported in Result.Split.
const (
	// ScopeDone means the package exists and validates.
	ScopeDone = envelope.ScopeDone
	// ScopeInvalid means the package exists and does not validate. It is
	// not written again; the errors name the rules to fix by hand.
	ScopeInvalid = envelope.ScopeInvalid
	// ScopeFailed means this run stopped on this scope. The plan is kept,
	// and running spec on the same input again resumes here.
	ScopeFailed = envelope.ScopeFailed
	// ScopePending means the scope was not reached.
	ScopePending = envelope.ScopePending
)

// setSplit records the split report and fills each written scope's state from
// the package this run wrote for it.
func (r *Result) setSplit(report []ScopeReport) {
	pkgs := append([]Package{r.Package}, r.FollowOnSpecs...)
	for i := range report {
		if report[i].SpecDir == "" {
			continue
		}
		for _, p := range pkgs {
			if p.SpecDir != report[i].SpecDir {
				continue
			}
			valid := p.Validation.Valid
			report[i].Valid = &valid
			report[i].ErrorCount = p.Validation.ErrorCount
			report[i].OpenQuestionsCount = len(p.OpenQuestions)
			report[i].TraceGaps = len(p.Traceability.CriteriaUncovered) +
				len(p.Traceability.PathsUncovered) + len(p.Traceability.TestsUnowned)
			break
		}
	}
	r.Split = report
}

// Failure carries the stage and category of a failed run.
type Failure struct {
	Stage    string
	Category string
	Err      error
	// TotalBudget is the --total-budget ceiling a budget stop was checked
	// against, so the envelope's fix_hint can name it. Zero otherwise.
	TotalBudget float64
}

// TotalBudgetUSD is the run-level ceiling behind a stage "budget" failure.
func (f *Failure) TotalBudgetUSD() float64 { return f.TotalBudget }

func (f *Failure) Error() string        { return f.Err.Error() }
func (f *Failure) Unwrap() error        { return f.Err }
func (f *Failure) StageName() string    { return f.Stage }
func (f *Failure) CategoryName() string { return f.Category }

func fail(stage, category string, err error) *Failure {
	return &Failure{Stage: stage, Category: category, Err: err}
}

func failf(stage, category, format string, args ...any) *Failure {
	return &Failure{Stage: stage, Category: category, Err: fmt.Errorf(format, args...)}
}

// scoped prefixes a failure's message with the scope it happened in, keeping
// its stage and category so a caller still reads which step and whether a
// re-run could help.
func scoped(label string, err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return &Failure{Stage: f.Stage, Category: f.Category, Err: fmt.Errorf("%s: %w", label, f.Err), TotalBudget: f.TotalBudget}
	}
	return fmt.Errorf("%s: %w", label, err)
}

// Categories this package adds to the shared vocabulary.
const (
	// CategoryInvalid means the package was generated and does not satisfy
	// the format's rules, its own or those across specs. The files are on
	// disk and the errors name the rules, because a spec you can read and fix
	// is worth more than no spec at all.
	CategoryInvalid = envelope.CategoryInvalidSpec
	// CategoryDisk is a filesystem failure.
	CategoryDisk = envelope.CategoryDisk
)

// Preflight validates the run options before spending turns or tokens.
func Preflight(ctx context.Context, o Options) (*Result, *Failure) {
	if o.Comment && !o.DryRun {
		if o.Input.Issue == nil {
			return nil, failf("preflight", "usage",
				"--comment posts the PRD back to the issue it came from, and the input is %s",
				o.Input.Kind)
		}
		if o.Forge == nil || !o.Forge.Authenticated() {
			return nil, failf("preflight", "auth",
				"--comment needs a forge credential: set GITHUB_TOKEN, GH_TOKEN, or GITLAB_TOKEN, or run without --comment")
		}
	}
	if o.Name != "" && !validName(o.Name) {
		return nil, failf("preflight", "usage", "--name %q must match [a-z][a-z0-9]*(_[a-z0-9]+)*", o.Name)
	}
	if o.Workspace == nil {
		return nil, failf("preflight", agentrun.CategoryInternal, "no workspace configured")
	}
	if o.Runner == nil && (o.author == nil || (o.Adversarial && o.reviewer == nil)) {
		return nil, failf("preflight", agentrun.CategoryInternal, "no runner configured")
	}
	for _, step := range afspec.GenerationSteps {
		if _, err := ArtifactSchema(step); err != nil {
			return nil, fail("preflight", agentrun.CategoryInternal, err)
		}
	}
	return nil, nil
}

// RunPreflight is spec --preflight: every check Run performs before its first
// model call, reported and then stopped at. It calls the one Preflight
// function Run calls, and the one findSplitPlan Run calls, never a copy of
// either, so a run that would refuse refuses here with the identical stage,
// category and message. A refusal returns no result: a partial checklist of
// what passed before it would be a second answer to "would this run start".
//
// Nothing is written: no package, no split plan, no comment, and no phase is
// run. What the package would be called is the model's to choose, so nothing
// about it is checked here.
func RunPreflight(ctx context.Context, o Options) (*Result, error) {
	if _, f := Preflight(ctx, o); f != nil {
		return nil, f
	}
	// The preflight reports the index the Runner's phases would read.
	o.Index = runIndex(o)

	result := &Result{Stage: "preflight", DryRun: o.DryRun, inputRef: cli.ResumePlaceholder(o.Input)}

	var list []cli.PreflightCheck
	add := func(check string, ok bool, detail string) {
		list = append(list, cli.PreflightCheck{Check: check, OK: ok, Detail: detail})
	}
	// The conditions below are the ones Preflight refuses on, so reaching
	// this line means each of them held.
	if o.Name != "" {
		add("name_flag", true, o.Name)
	}
	if o.Comment && !o.DryRun {
		add("comment_target", true, o.Input.Issue.String())
		add("comment_credential", true, "")
	}
	add("schemas_valid", true, "")

	// The plans are looked at exactly as Run looks at them, --dry-run
	// included; a dry run resumes none, so its estimate is the fresh one.
	packages := 1
	plan, err := findSplitPlan(resolveSpecsDir(o, o.Workspace.Root()), o.Input, o.Run)
	if err != nil {
		return nil, fail("preflight", "usage", err)
	}
	// Under --dry-run the lookup is for its warnings only (another input's
	// plan, a stale one): no split_plan entry.
	switch {
	case o.DryRun:
	case plan != nil:
		packages = plan.Pending()
		add("split_plan", true, fmt.Sprintf("resuming a split: %d of %d scopes to write", plan.Pending(), len(plan.Scopes)))
	default:
		add("split_plan", true, "no unfinished split for this input")
	}
	if backend, err := agentrun.DetectSymbolBackend(o.Workspace); err == nil {
		add("symbol_backend", true, backend)
	}
	add("code_search_index", true, indexDetail(o.Index, o.IndexUnavailable))
	if o.Runner != nil {
		add("phase_models", true, phaseModels(o.Runner, o.Architecture, o.Adversarial))
	}
	result.Preflight = list

	// The phases the plan on disk already decides: one PRD phase and one per
	// generation step for each package still to write. Whether a PRD not yet
	// written calls for a split is the model's decision, so a fresh input is
	// the one-package figure, a lower bound.
	phases := 1 + len(afspec.GenerationSteps)
	if o.Architecture {
		phases++
	}
	// A review stops early once nothing serious is found, so every allowed
	// round is counted: the figure is an upper bound for the review phases.
	if o.Adversarial {
		phases += 2 * o.AdversarialRounds
	}
	phases *= packages
	est := &cli.Estimate{Phases: phases}
	if o.Runner != nil {
		est.MaxTurnsPerPhase, est.MaxBudgetPerPhaseUSD = o.Runner.ResolvedBounds()
	}
	est.MaxTotalUSD = float64(phases) * est.MaxBudgetPerPhaseUSD
	result.Estimate = est
	return result, nil
}

// GeneratePRD runs spec generation and returns the primary package and its directory.
func GeneratePRD(ctx context.Context, o Options) (*Package, string, error) {
	res, err := Run(ctx, o)
	if res == nil {
		return nil, "", err
	}
	return &res.Package, res.SpecDir, err
}

// Run produces every specification package the input calls for.
//
// An input that is one spec's worth of work is one PRD phase and one
// package. An input the PRD phase reports as more than that is written as a
// split: the first package from the PRD just written, then one PRD phase and
// one package per remaining scope, each seeing the packages before it. The
// split is planned once, recorded in the spec root, and followed until every
// scope has a package — by this run, or by the next run on the same input if
// this one stops early.
//
// Within a package the order is the format's, not a preference:
// requirements, then tests, then tasks, each phase seeing the complete
// artifacts before it. A generator that cannot see a test id cannot own it,
// which is how the previous format produced specs whose edge-case and
// property tests belonged to no task.
func Run(ctx context.Context, o Options) (*Result, error) {
	if _, f := Preflight(ctx, o); f != nil {
		return nil, f
	}
	// Whether the run has an index is decided once, here, for every phase.
	// It is the index the Runner's phases read.
	o.Index = runIndex(o)
	root := o.Workspace.Root()
	specsDir := resolveSpecsDir(o, root)

	env := &runEnv{o: o, root: root, specsDir: specsDir, author: o.author, reviewer: o.reviewer}
	if env.author == nil || (env.reviewer == nil && o.Adversarial) {
		phases := &agentAuthor{runner: o.Runner, ws: o.Workspace, codeSearch: o.Index != nil}
		if env.author == nil {
			env.author = phases
		}
		if env.reviewer == nil {
			env.reviewer = phases
		}
	}
	landscape, err := discoverLandscape(specsDir)
	if err != nil {
		o.Run.Warn(cli.WarnSpecsDirUnreadable, "low", "could not read the existing specs in %s: %v", specsDir, err)
	}
	env.landscape = landscape
	env.profile = project.DetectProfile(root)
	if env.profile.Known() {
		o.Progress.Detail("project: %s (from %s), tests %q, linter %q",
			env.profile.Language, env.profile.Manifest, env.profile.AllTests, env.profile.Linter)
	} else {
		o.Run.Warn(cli.WarnProjectLanguageUnknown, "low", "the project's language could not be determined from %s: the plan's test "+
			"commands cannot be checked against it", root)
	}

	// The spec tool never changes the tree, so the map is built once, before
	// the first phase, and every phase of every scope of a split reuses it
	//. A failure never fails the run: the phases go without it.
	env.repoMap = buildRepoMap(ctx, o)

	result := &Result{DryRun: o.DryRun, inputRef: cli.ResumePlaceholder(o.Input)}

	// ---------------------------------------------------- resume or PRD --
	//
	// A plan left by an earlier run on this input is the split, already
	// decided; the PRD phase is not run again to re-decide it. A dry run
	// writes nothing and so also resumes nothing: it must not spend the
	// remaining scopes of a real plan without recording them.
	//
	// The plans are still looked at under --dry-run, so another input's
	// plan is reported the same way; a plan for this input is not resumed.
	plan, err := findSplitPlan(specsDir, o.Input, o.Run)
	if err != nil {
		return nil, fail("preflight", "usage", err)
	}
	if plan != nil && o.DryRun {
		o.Progress.Step("plan", "an unfinished split for this input is at %s; --dry-run does not resume it",
			relativeTo(root, plan.Path(specsDir)))
		plan = nil
	}

	var first *PRD
	if plan != nil {
		if o.Name != "" {
			o.Run.Warn(cli.WarnNameFlagIgnored, "low", "--name %q is ignored while resuming a split: the planned names are used", o.Name)
		}
		o.Run.Warn(cli.WarnRelevantFilesUnavailable, "low",
			"the PRD phase was not run (resuming a split); the later phases run without the relevant-files block")
		o.Progress.Step("plan", "resuming the split planned for %s: %d of %d scopes to write",
			plan.Input.Origin, plan.Pending(), len(plan.Scopes))
	} else {
		prd, err := env.writePRD(ctx, nil, "")
		if err != nil {
			return nil, err
		}
		if o.Name != "" {
			prd.SpecName = o.Name
		}
		if len(prd.RecommendedSplit) > 0 {
			plan = newSplitPlan(o.Input, prd.RecommendedSplit)
			plan.Scopes[0].Name = prd.SpecName
			o.Progress.Step("plan", "the input is %d specs' worth of work; writing every scope: %s",
				len(plan.Scopes), scopeNames(plan))
			if !o.DryRun {
				if err := checkPlanSlot(plan, specsDir); err != nil {
					return nil, fail("plan", "usage", err)
				}
				if err := plan.save(specsDir); err != nil {
					return nil, fail("plan", CategoryDisk, fmt.Errorf("recording the split: %w", err))
				}
			}
		}
		first = &prd
	}

	// The plan is on disk from here until it is removed, whether this run
	// wrote it or found it, so the result names it from here: Resumable() and
	// next[] describe the disk, whichever way the run stops. A dry run reads
	// and writes no plan.
	if plan != nil && !o.DryRun {
		result.SplitPlan = relativeTo(root, plan.Path(specsDir))
	}

	// ---------------------------------------------------- one package --
	if plan == nil {
		// The review comes before the number is reserved: a run interrupted
		// while the PRD is reviewed leaves no reservation behind.
		var review *Review
		if o.Adversarial {
			prd, rv, err := env.runRounds(ctx, *first, "", nil)
			if err != nil {
				return result, err
			}
			first, review = &prd, rv
		}
		slot, err := env.reservePackage(*first, "")
		if err != nil {
			return result, err
		}
		pkg, err := env.buildPackage(ctx, *first, "", slot, nil, review)
		if pkg != nil {
			result.Package = *pkg
		}
		return result, err
	}

	// ------------------------------------------------------ the split --
	return env.writeSplit(ctx, plan, first, result)
}

// buildRepoMap builds the run's repository map. It never fails the run:
// navigation is an optimisation, so on an error the phases run without a map
// and a low warning records why. A zero RepoMapTokens disables
// the map. The idea's own words name the paths that are reduced last.
func buildRepoMap(ctx context.Context, o Options) string {
	build := o.buildMap
	if build == nil {
		build = repomap.Build
	}
	m, err := build(ctx, o.Workspace, o.RepoMapTokens, repomap.PathsIn(o.Input.Body))
	if err != nil {
		o.Run.Warn(cli.WarnRepoMapBuildFailed, "low", "the repository map could not be built, so the phases run without it: %v", err)
		return ""
	}
	return m
}

// runEnv is what every package of a run shares.
type runEnv struct {
	o        Options
	root     string
	specsDir string
	author   author
	reviewer reviewer
	profile  project.Profile
	// repoMap is the run's repository map, or "" when it is disabled or could
	// not be built.
	repoMap string

	// mu guards what the packages of a split, generated concurrently, share:
	// the landscape, the numbering and the split plan.
	mu        sync.Mutex
	landscape []afspec.SpecMeta
	// lastID is the highest numeric prefix this run has assigned, so a dry
	// run — which leaves nothing on disk to count — still numbers its
	// packages consecutively.
	lastID int
	// siblings are the packages of this run by number, for the packages after
	// them to wait for and read (see siblings.go).
	siblings map[int]*sibling
}

// setLandscapeStatus records a package's new status in the landscape.
func (e *runEnv) setLandscapeStatus(specID, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.landscape {
		if e.landscape[i].SpecID == specID {
			e.landscape[i].Status = status
		}
	}
}

// landscapeNow is a copy of the landscape, for a phase to read while other
// packages are being written.
func (e *runEnv) landscapeNow() []afspec.SpecMeta {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.landscape)
}

// writePRD runs the PRD phase, for the whole input or for one scope of it.
func (e *runEnv) writePRD(ctx context.Context, split *splitContext, label string) (PRD, error) {
	o := e.o
	done := o.Progress.Begin("%swriting the PRD from %s", prefixed(label), o.Input.Origin)
	prd, stats, err := e.author.WritePRD(ctx, prdRequest{
		Root:         e.root,
		SourceKind:   o.Input.Kind.String(),
		SourceOrigin: o.Input.Origin,
		Input:        o.Input.Body,
		Context:      o.Input.Context,
		Profile:      e.profile,
		Landscape:    e.landscapeNow(),
		SpecRoot:     relativeTo(e.root, e.specsDir),
		Steering:     project.Steering(e.specsDir),
		Split:        split,
		RepoMap:      e.repoMap,
	})
	scope := prd.SpecName
	if split != nil {
		scope = split.Scope().Name
	}
	recordPhase(o.Run, stats, scope)
	done(cli.PhaseSummary(stats))
	if err != nil {
		return PRD{}, fail("prd", agentrun.CategoryOf(err), err)
	}
	return prd, nil
}

// allocateID is the next free numeric prefix: one past the highest on disk
// or the highest this run has used, whichever is larger.
func (e *runEnv) allocateID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := maxSpecNumber(e.specsDir) + 1
	if n <= e.lastID {
		n = e.lastID + 1
	}
	e.lastID = n
	return formatSpecID(n)
}

// slot is the number and directory reserved for one package.
type slot struct {
	specID  string
	dirName string
	path    string
}

// reservePackage takes the package's number before any of its phases run.
//
// The number is reserved on disk now, by creating the package's directory,
// not when the package is written after four paid phases: a second run, or a
// person's mkdir, in the meantime would otherwise take the same number, and
// the ids inside the artifacts carry it, so the package cannot be renumbered
// afterwards. buildPackage gives the number back when it fails before the
// write. A dry run writes nothing and reserves nothing. A split reserves its
// packages one at a time, in scope order, so the numbers follow the plan
// whichever package finishes first.
func (e *runEnv) reservePackage(prd PRD, label string) (slot, error) {
	if !afspec.IsSpecName(prd.SpecName) {
		return slot{}, scopedIf(label, failf("scaffold", "internal",
			"the spec name %q does not make an NN_snake_case directory name", prd.SpecName))
	}
	var specID string
	if e.o.DryRun {
		specID = e.allocateID()
	} else {
		e.mu.Lock()
		floor := e.lastID
		e.mu.Unlock()
		id, err := reserveSpecDir(e.specsDir, prd.SpecName, floor)
		if err != nil {
			return slot{}, scopedIf(label, fail("scaffold", CategoryDisk, err))
		}
		specID = id
		if n, err := strconv.Atoi(id); err == nil {
			e.mu.Lock()
			e.lastID = max(e.lastID, n)
			e.mu.Unlock()
		}
	}
	dirName := specID + "_" + prd.SpecName
	e.track(specID, dirName)
	return slot{specID: specID, dirName: dirName, path: filepath.Join(e.specsDir, dirName)}, nil
}

// scopedIf is scoped for a split's scope, and err unchanged for an undivided
// input, whose label is empty.
func scopedIf(label string, err error) error {
	if err == nil || label == "" {
		return err
	}
	return scoped(label, err)
}

// prefixed is a progress line's prefix for a split's scope, "" otherwise.
func prefixed(label string) string {
	if label == "" {
		return ""
	}
	return label + ": "
}

// unwritten is pkg as a run that failed before writing it reports it: what
// the PRD phase decided, and nothing that says a package exists. Nothing is
// written until every phase has finished, and the reservation is removed, so
// the directory pkg was going to have is gone.
func unwritten(pkg *Package) *Package {
	cp := *pkg
	cp.SpecDir, cp.Status, cp.Artifacts = "", "", []string{}
	return &cp
}

// buildPackage turns one PRD into one package in the slot reserved for it:
// the three generation phases, the optional architecture document, the
// write, the validation and the activation. label names the scope in progress
// messages and errors; it is empty for an undivided input. onWritten, when
// set, is told the package's id and directory the moment it is on disk.
// review is how prd was reviewed, nil without --adversarial; it is reported
// on the package, written or not, and never written into it.
//
// The package is returned alongside any error, so a caller can report what
// exists — an invalid package in particular is on disk and worth naming. One
// that failed before it was written has no SpecDir (see unwritten). Packages of a split are built concurrently, so
// everything this shares with them goes through e.mu.
func (e *runEnv) buildPackage(ctx context.Context, prd PRD, label string, sl slot,
	onWritten func(specID, dirName string), review *Review) (*Package, error) {

	o := e.o
	wrap := func(err error) error { return scopedIf(label, err) }
	specID, dirName, specPath := sl.specID, sl.dirName, sl.path
	// The packages after this one wait for it, however it ends.
	sib := e.sibling(specID)
	defer sib.settle()
	written := false
	if !o.DryRun {
		defer func() {
			if !written {
				_ = os.Remove(specPath) // only an empty reservation; Remove refuses anything else
			}
		}()
	}

	pkg := &Package{
		SpecDir:       relativeTo(e.root, specPath),
		SpecID:        specID,
		SpecName:      prd.SpecName,
		Title:         prd.Title,
		Status:        "draft",
		Source:        sourceField(o.Input),
		OpenQuestions: prd.OpenQuestions,
		RelevantFiles: prd.RelevantFiles,
		Review:        review,
	}

	// ------------------------------------------------------- generation --
	spec := afspec.CreateSpec(specID, prd.SpecName)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	spec.CreatedAt, spec.UpdatedAt = now, now
	spec.Title = prd.Title
	spec.Source = pkg.Source
	spec.PRDBody = normalizeBody(prd.Body)

	partial := afspec.PartialSpec{SpecID: specID, SpecName: prd.SpecName}
	for _, step := range afspec.GenerationSteps {
		req := artifactRequest{
			Step:          step,
			SpecID:        specID,
			SpecName:      prd.SpecName,
			Root:          e.root,
			PRD:           spec.PRDBody,
			Profile:       e.profile,
			Landscape:     e.landscapeNow(),
			SpecRoot:      relativeTo(e.root, e.specsDir),
			Steering:      project.Steering(e.specsDir),
			Partial:       &partial,
			RelevantFiles: prd.RelevantFiles,
			RepoMap:       e.repoMap,
			CodebaseNotes: prd.CodebaseNotes,
		}
		if step == afspec.StepRequirements {
			// The paths are checked against the specs this one depends on, by
			// the names their paths use. The earlier packages of this run are
			// not on disk yet, so this step waits for theirs.
			e.awaitRequirements(ctx, specID)
			req.Actors = e.actorsNow(specID)
		}
		done := o.Progress.Begin("%sgenerating %s", prefixed(label), afspec.ArtifactFileName(step))
		_, stats, err := e.author.GenerateArtifact(ctx, req)
		recordPhase(o.Run, stats, prd.SpecName)
		done(cli.PhaseSummary(stats))
		if err != nil {
			return unwritten(pkg), wrap(fail(string(step), agentrun.CategoryOf(err), err))
		}
		if step == afspec.StepRequirements {
			sib.endRequirements(actorsOf(partial.Requirements))
		}
		pkg.Artifacts = append(pkg.Artifacts, afspec.ArtifactFileName(step))
	}
	spec.Requirements = partial.Requirements
	spec.TestSpec = partial.TestSpec
	spec.Tasks = partial.Tasks

	if o.Architecture {
		done := o.Progress.Begin("%swriting architecture.md", prefixed(label))
		doc, stats, err := e.author.WriteArchitecture(ctx, architectureRequest{
			SpecID: specID, SpecName: prd.SpecName, Root: e.root, PRD: spec.PRDBody, Partial: &partial,
			RelevantFiles: prd.RelevantFiles, RepoMap: e.repoMap, CodebaseNotes: prd.CodebaseNotes,
			Profile: e.profile, Steering: project.Steering(e.specsDir),
		})
		recordPhase(o.Run, stats, prd.SpecName)
		done(cli.PhaseSummary(stats))
		if err != nil {
			// The architecture document is optional by the format's own
			// definition, so failing to write it degrades the package rather
			// than failing the run that produced the four required artifacts.
			o.Run.Warn(cli.WarnArchitectureNotWritten, "low", "architecture.md could not be written: %v", err)
		} else {
			spec.Architecture = doc
			pkg.Artifacts = append(pkg.Artifacts, "architecture.md")
		}
	}

	summarize(pkg, spec)

	// ------------------------------------------------------------ write --
	if !o.DryRun {
		if err := writeSpec(spec, specPath); err != nil {
			return pkg, wrap(err)
		}
		written = true
		o.Progress.Step("write", "%swrote %s", prefixed(label), specPath)
		if onWritten != nil {
			onWritten(specID, dirName)
		}
	}
	pkg.Artifacts = append([]string{"prd.md"}, pkg.Artifacts...)

	// --------------------------------------------------------- validate --
	//
	// What is validated is what was WRITTEN, re-read from disk, not the value
	// in memory. Save renders prd.md from the frontmatter fields and the body
	// and encodes the three artifacts canonically; validating the in-memory
	// spec would check the pipeline's intention rather than the package a
	// coder will open. A dry run has nothing on disk, so it validates the
	// value it would have written, with the directory name it would have used
	// — which is what makes the C1 folder check apply to both paths.
	validated := spec
	validated.Dir = specPath
	if !o.DryRun {
		loaded, err := afspec.LoadSpec(specPath)
		if err != nil {
			return pkg, wrap(fail("validate", CategoryDisk,
				fmt.Errorf("the package was written to %s and cannot be read back: %w", specPath, err)))
		}
		validated = loaded
	}

	// The verdict is the format's whole one, speca's: the package's own rules
	// and the rules across specs, which need the specs it depends on. Those
	// written before it in this run are settled first, and a dry run, which
	// wrote none of them, hands over the ones it held in memory.
	if o.DryRun {
		sib.keep(validated)
	}
	e.awaitSettled(ctx, specID)
	var inMemory []*afspec.Spec
	if o.DryRun {
		inMemory = append(e.earlierSpecs(specID), validated)
	}
	verdict, err := specpkg.Verdict(e.specsDir, validated, inMemory...)
	if err != nil {
		// The package was just written there, so a spec root that cannot be
		// read now is the disk's failure, not a verdict.
		return pkg, wrap(fail("validate", CategoryDisk,
			fmt.Errorf("the package was written to %s and the spec root cannot be read to check it against the other specs: %w", specPath, err)))
	}
	report := specpkg.ReportOf(verdict)
	pkg.Validation = report
	pkg.Traceability = specpkg.TraceOf(validated)

	// The package now exists as far as later scopes are concerned, valid or
	// not: the landscape lists it, so the next PRD is written against it. A
	// dry run wrote nothing, so there is nothing for a later scope to read.
	if !o.DryRun {
		e.mu.Lock()
		e.landscape = append(e.landscape, afspec.SpecMeta{
			SpecID: specID, SpecName: prd.SpecName, Status: pkg.Status, Dir: specPath,
		})
		e.mu.Unlock()
	}

	if !report.Valid {
		o.Progress.Step("validate", "%sthe package does not validate: %d error(s)", prefixed(label), report.ErrorCount)
		if o.Comment && !o.DryRun && o.Input.Issue != nil {
			o.Run.Warn(cli.WarnCommentNotPosted, "low", "the PRD was not posted on %s: the package does not "+
				"validate, and a PRD whose package is invalid is not one to circulate", o.Input.Issue)
		}
		return pkg, wrap(failf("validate", CategoryInvalid,
			"the generated package has %d validation error(s); it is on disk at %s and each "+
				"error names the rule it broke", report.ErrorCount, pkg.SpecDir))
	}
	o.Progress.Step("validate", "%svalid: %d requirements, %d tests, %d tasks", prefixed(label),
		pkg.Requirements, pkg.Tests, pkg.Tasks)

	// --------------------------------------------------------- activate --
	if o.Activate && !o.DryRun {
		activated, err := validated.Transition("active", specPath)
		if err != nil {
			o.Run.Warn(cli.WarnActivationFailed, "high", "the spec validates but could not be activated: %v", err)
		} else {
			pkg.Status = activated.Status
			e.setLandscapeStatus(specID, activated.Status)
			o.Progress.Step("activate", "%sactivated %s", prefixed(label), dirName)
		}
	}

	// ---------------------------------------------------------- comment --
	if o.Comment && !o.DryRun && o.Input.Issue != nil {
		body := prdComment(validated, pkg)
		url, err := o.Forge.AddComment(ctx, *o.Input.Issue, body)
		if err != nil {
			o.Run.Warn(cli.WarnCommentNotPosted, "low", "the PRD could not be posted on %s: %v", o.Input.Issue, err)
			o.Run.RecordSideEffect("comment", "prd", o.Input.Issue.String(), "", false, cli.WarnCommentNotPosted)
		} else {
			o.Run.RecordSideEffect("comment", "prd", o.Input.Issue.String(), url, true, "")
			pkg.CommentURL = url
			o.Progress.Step("prd", "posted the PRD to %s", url)
		}
	}
	return pkg, nil
}

// splitReport renders the plan for the result. failed holds the scopes this
// run stopped on.
func splitReport(root, specsDir string, plan *SplitPlan, failed map[int]bool) []ScopeReport {
	out := make([]ScopeReport, 0, len(plan.Scopes))
	for i, s := range plan.Scopes {
		r := ScopeReport{Name: s.Name, Scope: s.Scope, Status: ScopePending}
		switch {
		case s.Written():
			r.Status = ScopeDone
			if s.Invalid {
				r.Status = ScopeInvalid
			}
			r.SpecID = s.SpecID
			r.SpecDir = relativeTo(root, filepath.Join(specsDir, s.Dir))
		case failed[i]:
			r.Status = ScopeFailed
		}
		out = append(out, r)
	}
	return out
}

func scopeNames(plan *SplitPlan) string {
	names := make([]string, 0, len(plan.Scopes))
	for _, s := range plan.Scopes {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// writeSpec saves the artifacts into the package directory reserveSpecDir
// created.
//
// The directory is removed again if the save fails, because a half-written
// spec directory is picked up by DiscoverSpecs and then reported as broken by
// every later run.
func writeSpec(spec *afspec.Spec, specPath string) *Failure {
	if err := spec.Save(specPath); err != nil {
		_ = os.RemoveAll(specPath)
		return fail("write", CategoryDisk, fmt.Errorf("saving %s: %w", specPath, err))
	}
	return nil
}

// resolveSpecsDir picks the spec root: the flag, then the environment, then
// the repository's own .specs.
func resolveSpecsDir(o Options, root string) string {
	if o.SpecsDir != "" {
		if filepath.IsAbs(o.SpecsDir) {
			return filepath.Clean(o.SpecsDir)
		}
		return filepath.Join(root, o.SpecsDir)
	}
	if v := strings.TrimSpace(os.Getenv(SpecDirEnv)); v != "" {
		if filepath.IsAbs(v) {
			return filepath.Clean(v)
		}
		return filepath.Join(root, v)
	}
	return filepath.Join(root, DefaultSpecDirName)
}

// discoverLandscape lists the specs that already exist, so the new one is not
// written as though the repository had none. It returns what it could read
// even when it also returns an error.
func discoverLandscape(specsDir string) ([]afspec.SpecMeta, error) {
	if _, err := os.Stat(specsDir); err != nil {
		return nil, nil // a repository with no specs yet is not an error
	}
	// DiscoverSpecs returns the specs it could read alongside the errors of
	// the ones it could not: one unreadable directory is reported, and the
	// rest are still the landscape.
	metas, err := afspec.DiscoverSpecs(specsDir)
	sort.Slice(metas, func(i, j int) bool { return metas[i].SpecID < metas[j].SpecID })
	return metas, err
}

// maxSpecNumber is the highest numeric prefix among the packages on disk,
// archived ones included, or 0 when there are none. An archived spec keeps
// its number: reusing it would make two specs answer to one ID.
func maxSpecNumber(specsDir string) int {
	return max(maxPrefixIn(specsDir), maxPrefixIn(filepath.Join(specsDir, "archive")))
}

// maxPrefixIn is the highest numeric prefix before the first `_` among the
// direct subdirectories of dir. A directory that cannot be read counts as
// empty.
func maxPrefixIn(dir string) int {
	highest := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		i := strings.IndexByte(name, '_')
		if i <= 0 {
			continue
		}
		if n, err := strconv.Atoi(name[:i]); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// formatSpecID zero-pads a prefix to two digits (and to three once past 99,
// so the directories keep sorting lexically).
func formatSpecID(n int) string {
	if n > 99 {
		return fmt.Sprintf("%03d", n)
	}
	return fmt.Sprintf("%02d", n)
}

// sourceField is the PRD frontmatter's provenance.
//
// It is set here rather than left blank, because "where did this spec come
// from" is the first question anyone asks of a generated spec and the answer
// is knowable at the moment it is written.
func sourceField(in cli.Input) string {
	switch in.Kind {
	case cli.KindIssue, cli.KindFile:
		return in.Origin
	default:
		return "interactive"
	}
}

// normalizeBody strips any frontmatter fence a model added anyway, and
// guarantees the body ends with exactly one newline.
func normalizeBody(body string) string {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "---\n") {
		if i := strings.Index(body[4:], "\n---\n"); i >= 0 {
			body = strings.TrimSpace(body[4+i+5:])
		}
	}
	return body + "\n"
}

func summarize(r *Package, s *afspec.Spec) {
	if s.Requirements != nil {
		r.Requirements = len(s.Requirements.Requirements)
		for _, req := range s.Requirements.Requirements {
			r.Criteria += len(req.Criteria)
		}
		r.ExecutionPaths = len(s.Requirements.ExecutionPaths)
	}
	if s.TestSpec != nil {
		r.Tests = len(s.TestSpec.Tests)
	}
	if s.Tasks != nil {
		r.Tasks = len(s.Tasks.Tasks)
	}
}

// prdComment is what is posted back to the issue a spec was generated from.
// A split posts one comment per package, each carrying its own PRD.
func prdComment(s *afspec.Spec, r *Package) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Specification: `%s`\n\n", filepath.Base(r.SpecDir))
	fmt.Fprintf(&b, "Generated from this issue: %d requirements, %d criteria, %d tests, "+
		"%d tasks, %d execution paths. The package validates.\n\n",
		r.Requirements, r.Criteria, r.Tests, r.Tasks, r.ExecutionPaths)
	if len(r.OpenQuestions) > 0 {
		b.WriteString("### Decisions worth checking\n\n")
		for _, q := range r.OpenQuestions {
			fmt.Fprintf(&b, "- **%s** — %s (%s)\n",
				strings.TrimSpace(q.Question), strings.TrimSpace(q.Decision), strings.TrimSpace(q.Why))
		}
		b.WriteString("\n")
	}
	b.WriteString("### PRD\n\n")
	b.WriteString(strings.TrimSpace(s.PRDBody))
	b.WriteString("\n\n---\n*Written by `spec`. It is not a substitute for review.*\n")
	return b.String()
}

func relativeTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// recordPhase puts a finished phase in the envelope, labelled with the spec
// it worked on: a split runs the same phases once per scope, and without the
// label the report cannot say what any one spec cost.
func recordPhase(run *cli.Run, res agentrun.Result, scope string) {
	if run == nil || res.Name == "" {
		return
	}
	run.AddPhase(cli.PhaseFromResult(res, scope))
}

// phaseModels is the detail of the informational phase_models preflight
// check: the model and effort each phase the run will start runs on, in the
// order a package is built.
func phaseModels(r *agentrun.Runner, architecture, adversarial bool) string {
	var parts []string
	for _, phase := range agentrun.Phases {
		if phase == agentrun.PhaseArchitecture && !architecture {
			continue
		}
		if (phase == agentrun.PhasePRDReview || phase == agentrun.PhasePRDRevision) && !adversarial {
			continue
		}
		parts = append(parts, phase+" "+r.ModelFor(phase).String())
	}
	return strings.Join(parts, "; ")
}

// runIndex is the index the run's phases read: the one Options carries, or,
// when it carries none, the one the Runner was built with. The shell puts the
// run's one index on both.
func runIndex(o Options) tools.Index {
	if o.Index == nil && o.Runner != nil {
		return o.Runner.Index()
	}
	return o.Index
}

// indexDetail is the detail of the informational code_search_index preflight
// check: "built" when the run has an index, otherwise why it has
// none. The fallback is search_files, so the check never refuses the run.
func indexDetail(idx tools.Index, reason string) string {
	if idx != nil {
		return "built"
	}
	if reason == "" {
		reason = "index not built"
	}
	return "unavailable: " + reason
}

// reserveSpecDir takes the next free spec number for a package called name,
// by creating its directory: one past the highest number on disk, archived
// specs included, and past floor (the highest this run has used). It returns
// the number.
//
// Choosing the number and creating the directory happen under a lock file in
// the spec root, taken with O_EXCL, so two runs cannot read the same highest
// number and both create a directory under it with different names. A lock
// older than reserveLockStale is from a run that died holding it, and is
// broken.
func reserveSpecDir(specsDir, name string, floor int) (string, error) {
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", specsDir, err)
	}
	lock := filepath.Join(specsDir, reserveLockName)
	deadline := time.Now().Add(reserveLockStale)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			break
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("locking %s: %w", specsDir, err)
		}
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > reserveLockStale {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%s is held by another run; remove it if no spec run is in progress", lock)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() { _ = os.Remove(lock) }()

	n := max(maxSpecNumber(specsDir), floor) + 1
	for range 100 {
		id := formatSpecID(n)
		dir := filepath.Join(specsDir, id+"_"+name)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return id, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
		n++
	}
	return "", fmt.Errorf("no free spec number in %s after 100 attempts", specsDir)
}

// reserveLockName is the lock file reserveSpecDir holds while it picks a
// number. It is not a directory, so nothing that lists specs sees it.
const reserveLockName = ".spec-number.lock"

// reserveLockStale is how long a lock may be held before it is taken to be a
// dead run's: picking a number is a directory listing and a mkdir.
const reserveLockStale = 30 * time.Second
