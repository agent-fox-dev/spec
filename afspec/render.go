package afspec

import (
	"fmt"
	"slices"
	"strings"
)

// Rendering is deterministic: same JSON in, same Markdown out (§11).
//
// An unbudgeted render drops nothing from the contract: the PRD frontmatter,
// every field of every artifact, every check and smoke check, the test
// commands and the cross-spec dependencies all reach the Markdown. A coder
// reads the render instead of the files, so a field it leaves out is a field
// the coder never sees. The exceptions are the artifacts' $schema URIs and
// their copies of the spec's identity, which say nothing the metadata does
// not, and the progress, which is not the contract and is never rendered: a
// task move leaves the render as it was (§3). A scoped render shows what the
// task does not own by id and title; a budget (WithMaxTokens) is the caller's
// request to drop detail.

// The levels of detail a budget steps through: the full render, the render
// without the architecture, and that one with the checks slimmed to their
// kind and the smoke checks to their components and end state.
const (
	levelFull = iota
	levelNoArchitecture
	levelSlim
)

// RenderCombined renders the whole spec as one Markdown document in the order
// of §11.1: PRD body, architecture if present, requirements, tasks.
//
// With WithMaxTokens(N) it applies progressive truncation:
//   - level 0: full render
//   - level 1: drop the architecture section
//   - level 2: additionally render the checks and smoke checks in slim form
func (s *Spec) RenderCombined(opts ...RenderOption) string {
	cfg := resolveOpts(opts)

	result := s.renderCombinedAt(levelFull)
	if !budgetActive(cfg) || EstimateTokens(result) <= cfg.maxTokens {
		return result
	}

	if s.Architecture != "" {
		result = s.renderCombinedAt(levelNoArchitecture)
		if EstimateTokens(result) <= cfg.maxTokens {
			return result
		}
	}

	return s.renderCombinedAt(levelSlim)
}

func (s *Spec) renderCombinedAt(level int) string {
	var sb strings.Builder

	sb.WriteString("# PRD\n\n")
	sb.WriteString(s.prdSection())
	sb.WriteString("\n")

	if s.Architecture != "" && level < levelNoArchitecture {
		sb.WriteString("# Architecture\n\n")
		sb.WriteString(s.Architecture)
		sb.WriteString("\n")
	}

	sb.WriteString("# Requirements\n\n")
	if s.Requirements != nil {
		sb.WriteString(renderRequirements(s.Requirements, level == levelSlim))
	}
	sb.WriteString("\n")

	sb.WriteString("# Tasks\n\n")
	if s.Tasks != nil {
		sb.WriteString(s.RenderTasks())
	}
	sb.WriteString("\n")

	return sb.String()
}

// RenderIndividual renders each artifact separately, keyed by "prd",
// "requirements", "tasks" and — only when present — "architecture". The
// same truncation levels as RenderCombined apply.
func (s *Spec) RenderIndividual(opts ...RenderOption) map[string]string {
	cfg := resolveOpts(opts)

	result := map[string]string{
		"prd":          s.prdSection(),
		"requirements": "",
		"tasks":        "",
	}
	if s.Requirements != nil {
		result["requirements"] = s.Requirements.Render()
	}
	if s.Tasks != nil {
		result["tasks"] = s.RenderTasks()
	}
	if s.Architecture != "" {
		result["architecture"] = s.Architecture
	}

	if !budgetActive(cfg) || sumMapTokens(result) <= cfg.maxTokens {
		return result
	}

	if _, hasArch := result["architecture"]; hasArch {
		delete(result, "architecture")
		if sumMapTokens(result) <= cfg.maxTokens {
			return result
		}
	}

	if s.Requirements != nil {
		result["requirements"] = renderRequirements(s.Requirements, true)
	}
	return result
}

// RenderIndividualScoped renders the spec scoped to one task (§11.1): the PRD
// body without its Background and Requirements sections, which the criteria
// below restate, and the architecture unfiltered; for a task of the plan, the
// requirements that own the task's criteria in full and all others as one
// line each, the paths a shared check of those criteria names in full; for
// the integration task, every path in full with its smoke check and every
// requirement as one line, with the criteria that carry a contract; the task
// in full and all others as one line.
//
// Because rule C5 guarantees that every task lists its criteria, there is no
// inference: what a task owns is what it says. An unknown task ID falls back
// to the unscoped render.
func (s *Spec) RenderIndividualScoped(taskID int, opts ...RenderOption) map[string]string {
	cfg := resolveOpts(opts)

	task, ok := s.GetTask(taskID)
	if !ok {
		return s.RenderIndividual(opts...)
	}

	// Criteria in scope: the task's criterion IDs, plus every criterion of a
	// requirement the task claims wholesale.
	criteriaRefs := map[string]bool{}
	requirementRefs := map[string]bool{}
	for _, ref := range task.Criteria {
		criteriaRefs[ref] = true
		requirementRefs[ref] = true
	}

	// Paths in scope: every path for the integration task; for a task of the
	// plan, those a shared check of its criteria names.
	pathRefs := map[string]bool{}
	if s.Requirements != nil {
		for _, req := range s.Requirements.Requirements {
			for _, c := range req.Criteria {
				inScope := requirementRefs[req.Id] || criteriaRefs[c.Id]
				if inScope && c.Check.SameAs != nil {
					pathRefs[*c.Check.SameAs] = true
				}
			}
		}
		if task.Kind == TaskKindIntegration {
			for _, p := range s.Requirements.ExecutionPaths {
				pathRefs[p.Id] = true
			}
		}
	}

	result := map[string]string{"prd": s.scopedPRDSection()}
	if s.Architecture != "" {
		result["architecture"] = s.Architecture
	}

	render := func(slim bool) {
		if s.Requirements != nil {
			result["requirements"] = s.renderScopedRequirements(*task, criteriaRefs, requirementRefs, pathRefs, slim)
		} else {
			result["requirements"] = ""
		}
	}
	render(false)

	if s.Tasks != nil {
		result["tasks"] = s.renderScopedTasks(taskID)
	} else {
		result["tasks"] = ""
	}

	if !budgetActive(cfg) || sumMapTokens(result) <= cfg.maxTokens {
		return result
	}

	if _, hasArch := result["architecture"]; hasArch {
		delete(result, "architecture")
		if sumMapTokens(result) <= cfg.maxTokens {
			return result
		}
	}

	render(true)
	return result
}

// prdSection is the PRD as a render shows it: the frontmatter, then the body.
func (s *Spec) prdSection() string {
	meta := s.renderMetadata()
	if meta == "" {
		return s.PRDBody
	}
	return meta + "\n" + s.PRDBody
}

// scopedPRDSection is the PRD as a task's render shows it: prdSection without
// the body's Background and Requirements, which a coder on one task reads as
// the criteria it owns.
func (s *Spec) scopedPRDSection() string {
	meta := s.renderMetadata()
	body := withoutProseSections(s.PRDBody)
	if meta == "" {
		return body
	}
	return meta + "\n" + body
}

// scopedOutSections are the PRD sections a task's render leaves out, by their
// exact headings, in the order the note names them.
var scopedOutSections = []string{"Background", "Requirements"}

// withoutProseSections removes body's level-two sections named in
// scopedOutSections: each from its heading line to the next level-two heading
// or the end. A "## " line inside a fenced code block is not a heading. The
// first section removed is replaced by one line that names what was left out
// and where it is; a body with none of them is returned unchanged.
func withoutProseSections(body string) string {
	lines := strings.SplitAfter(body, "\n")
	var out []string
	var removed []string
	notePos := -1
	skipping, fence := false, ""
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if fence == "" {
			if heading, ok := strings.CutPrefix(trimmed, "## "); ok {
				name := strings.TrimSpace(heading)
				skipping = slices.Contains(scopedOutSections, name)
				if skipping {
					removed = append(removed, name)
					if notePos < 0 {
						notePos = len(out)
					}
				}
			}
		}
		if marker := fenceMarker(trimmed); marker != "" {
			switch {
			case fence == "":
				fence = marker
			case strings.HasPrefix(strings.TrimSpace(trimmed), fence):
				fence = ""
			}
		}
		if !skipping {
			out = append(out, line)
		}
	}
	if notePos < 0 {
		return body
	}
	var named []string
	for _, name := range scopedOutSections {
		if slices.Contains(removed, name) {
			named = append(named, name)
		}
	}
	verb, pronoun := "are", "them"
	if len(named) == 1 {
		verb, pronoun = "is", "it"
	}
	note := fmt.Sprintf("_The PRD's %s %s not in a task's render: the criteria this task owns are below. "+
		"Read %s in prd.md or spec.md._\n\n", strings.Join(named, " and "), verb, pronoun)
	return strings.Join(out[:notePos], "") + note + strings.Join(out[notePos:], "")
}

// fenceMarker is the fence a Markdown line opens or closes, "```" or "~~~",
// or "" when it is not a fence line.
func fenceMarker(line string) string {
	t := strings.TrimSpace(line)
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, m) {
			return m
		}
	}
	return ""
}

// renderMetadata renders the PRD frontmatter as a list, one field per line.
// A field the frontmatter does not set is left out; a spec built in memory
// without frontmatter renders none.
func (s *Spec) renderMetadata() string {
	var sb strings.Builder
	field := func(name, value string) {
		if value != "" {
			sb.WriteString(fmt.Sprintf("- **%s:** %s\n", name, value))
		}
	}
	if s.SpecID != "" || s.SpecName != "" {
		field("Spec", fmt.Sprintf("%s (id %s, name %s)", code(s.SpecID+"_"+s.SpecName), code(s.SpecID), code(s.SpecName)))
	}
	field("Title", s.Title)
	field("Status", s.Status)
	if s.SchemaVersion != 0 {
		field("Schema version", fmt.Sprint(s.SchemaVersion))
	}
	field("Profile", s.Profile)
	field("Steering version", s.SteeringVersion)
	field("Created", s.CreatedAt)
	field("Source", s.Source)
	field("Supersedes", codeList(s.Supersedes))
	if sb.Len() > 0 {
		hash := "none (the spec has not been activated)"
		if s.IntentHash != nil && *s.IntentHash != "" {
			hash = code(*s.IntentHash)
		}
		field("Intent hash", hash)
	}
	return sb.String()
}

// isRequirementInScope reports whether a requirement is claimed by the task,
// either wholesale by its own ID or through one of its criteria.
func isRequirementInScope(req Requirement, criteriaRefs, requirementRefs map[string]bool) bool {
	if requirementRefs[req.Id] {
		return true
	}
	for _, c := range req.Criteria {
		if criteriaRefs[c.Id] {
			return true
		}
	}
	return false
}

// renderScopedRequirements renders the in-scope requirements in full, every
// other requirement as one line, the in-scope execution paths in full and
// every other as one line. For the integration task it lists, instead of
// requirements in full, the criteria that carry a contract.
func (s *Spec) renderScopedRequirements(task PlannedTask, criteriaRefs, requirementRefs, pathRefs map[string]bool, slim bool) string {
	var sb strings.Builder
	integration := task.Kind == TaskKindIntegration

	sb.WriteString("## Spec Overview\n\n")
	if integration {
		sb.WriteString("All requirements in this specification (the integration task owns no criterion; the contracts to confirm are below):\n\n")
	} else {
		sb.WriteString("All requirements in this specification (full detail shown only for the active task):\n\n")
	}
	for _, req := range s.Requirements.Requirements {
		suffix := " (other task)"
		if !integration && isRequirementInScope(req, criteriaRefs, requirementRefs) {
			suffix = " (included below)"
		}
		sb.WriteString(fmt.Sprintf("- **%s:** %s%s\n", req.Id, req.Title, suffix))
	}
	sb.WriteString("\n")
	if len(s.Requirements.ExecutionPaths) > 0 {
		if integration {
			sb.WriteString("Execution paths (every path in full: the integration task verifies them all):\n\n")
		} else {
			sb.WriteString("Execution paths (full detail shown only for the paths a check of the active task's criteria is the same as):\n\n")
		}
		for _, p := range s.Requirements.ExecutionPaths {
			suffix := " (verified by the integration task)"
			if pathRefs[p.Id] {
				suffix = " (included below)"
			}
			sb.WriteString(fmt.Sprintf("- **%s:** %s%s\n", p.Id, p.Title, suffix))
		}
		sb.WriteString("\n")
	}

	if integration {
		renderContracts(&sb, s.Requirements)
	} else {
		sb.WriteString("## Requirements\n\n")
		for _, req := range s.Requirements.Requirements {
			if isRequirementInScope(req, criteriaRefs, requirementRefs) {
				renderRequirement(&sb, req, slim)
			}
		}
	}

	var scopedPaths []ExecutionPath
	for _, p := range s.Requirements.ExecutionPaths {
		if pathRefs[p.Id] {
			scopedPaths = append(scopedPaths, p)
		}
	}
	renderExecutionPaths(&sb, scopedPaths, slim)
	// The external APIs belong to the whole spec, and which of them a task
	// calls is not recorded, so every scoped render carries them all.
	renderExternalAPIs(&sb, s.Requirements.ExternalApis)

	return sb.String()
}

// renderContracts lists every criterion that carries a contract, with it: the
// integration task confirms each one is consumed in production code.
func renderContracts(sb *strings.Builder, r *RequirementsV3Json) {
	sb.WriteString("## Contracts\n\n")
	n := 0
	for _, req := range r.Requirements {
		for _, c := range req.Criteria {
			if contract := c.ContractText(); contract != "" {
				sb.WriteString(fmt.Sprintf("- **%s:** %s\n", c.Id, contract))
				n++
			}
		}
	}
	if n == 0 {
		sb.WriteString("No criterion carries a contract.\n")
	}
	sb.WriteString("\n")
}

// renderScopedTasks renders the target task in full and every other task as a
// one-line summary.
func (s *Spec) renderScopedTasks(taskID int) string {
	var sb strings.Builder
	renderPlanContext(&sb, s.Tasks)
	sb.WriteString("## Tasks\n\n")
	for _, t := range s.Plan() {
		if t.Id == taskID {
			renderTaskFull(&sb, t, &s.Tasks.TestCommands)
		} else {
			renderTaskSummary(&sb, t)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// requirements.json
// ---------------------------------------------------------------------------

// Render renders the requirements artifact as Markdown: every requirement
// with its criteria, each with its check; every path with its smoke check;
// the external APIs.
func (r *RequirementsV3Json) Render() string { return renderRequirements(r, false) }

// renderRequirements is Render, in slim form when asked: the checks reduced
// to their kind and the smoke checks to their components and end state.
func renderRequirements(r *RequirementsV3Json, slim bool) string {
	var sb strings.Builder

	sb.WriteString("## Requirements\n\n")
	for _, req := range r.Requirements {
		renderRequirement(&sb, req, slim)
	}

	renderExecutionPaths(&sb, r.ExecutionPaths, slim)

	renderExternalAPIs(&sb, r.ExternalApis)

	return sb.String()
}

// renderExternalAPIs renders every external package the spec depends on,
// with each symbol's import path, signature and notes, and flags the
// packages whose signatures were not read from their source.
func renderExternalAPIs(sb *strings.Builder, apis []ExternalApi) {
	if len(apis) == 0 {
		return
	}
	sb.WriteString("## External APIs\n\n")
	for _, api := range apis {
		status := "verified"
		if !api.Verified {
			status = "UNVERIFIED — confirm these signatures before use"
		}
		sb.WriteString(fmt.Sprintf("### %s (%s) — %s\n\n", code(api.Package), api.Version, status))
		sb.WriteString("| Symbol | Import Path | Signature | Notes |\n")
		sb.WriteString("|--------|-------------|-----------|-------|\n")
		for _, sym := range api.Symbols {
			notes := ""
			if sym.Notes != nil {
				notes = *sym.Notes
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				cell(code(sym.Name)), cell(code(sym.ImportPath)), cell(code(sym.Signature)), cell(notes)))
		}
		sb.WriteString("\n")
	}
}

// renderRequirement renders one requirement with every criterion as its EARS
// sentence, its contract when it has one, and its check (§11.2).
func renderRequirement(sb *strings.Builder, req Requirement, slim bool) {
	sb.WriteString(fmt.Sprintf("### %s: %s\n\n", req.Id, req.Title))

	if req.Rationale != nil && *req.Rationale != "" {
		sb.WriteString(fmt.Sprintf("**Rationale:** %s\n\n", *req.Rationale))
	}

	sb.WriteString("#### Criteria\n\n")
	for _, c := range req.Criteria {
		resolved := req.Resolve(c)
		sb.WriteString(fmt.Sprintf("1. [%s] %s\n", c.Id, resolved.RenderEARSSentence()))
		if contract := c.ContractText(); contract != "" {
			sb.WriteString(fmt.Sprintf("   → %s\n", contract))
		}
		renderCheck(sb, c.Check, slim)
	}
	sb.WriteString("\n")
}

// renderCheck renders a criterion's check under its sentence, indented as the
// contract is: the id it is the same as, or its kind, then, unless slim, its
// setup and its pseudocode.
func renderCheck(sb *strings.Builder, ch Check, slim bool) {
	if ch.SameAs != nil {
		sb.WriteString(fmt.Sprintf("   Check: same as %s\n", *ch.SameAs))
		return
	}
	kind := ch.KindText()
	if kind == "" {
		kind = "(none)"
	}
	sb.WriteString(fmt.Sprintf("   Check: %s\n", kind))
	if slim {
		return
	}
	for _, line := range ch.Setup {
		sb.WriteString(fmt.Sprintf("   - setup: %s\n", line))
	}
	if pc := ch.PseudocodeText(); pc != "" {
		sb.WriteString("   ```\n")
		sb.WriteString(indentLines(pc, "   "))
		sb.WriteString("   ```\n")
	}
}

// indentLines prefixes every line of text with prefix and ends it with a
// newline.
func indentLines(text, prefix string) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		sb.WriteString(prefix)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func renderExecutionPaths(sb *strings.Builder, paths []ExecutionPath, slim bool) {
	if len(paths) == 0 {
		return
	}
	sb.WriteString("## Execution Paths\n\n")
	for _, path := range paths {
		sb.WriteString(fmt.Sprintf("### %s: %s\n\n", path.Id, path.Title))
		for i, step := range path.Steps {
			sb.WriteString(fmt.Sprintf("%d. **%s** %s\n", i+1, step.Actor, step.Action))
		}
		sb.WriteString("\n")
		renderSmoke(sb, path.Smoke, slim)
	}
}

// renderSmoke renders a path's smoke check: the real components, the end
// state, and, unless slim, the setup and the pseudocode.
func renderSmoke(sb *strings.Builder, sm Smoke, slim bool) {
	sb.WriteString(fmt.Sprintf("**Smoke check — real components (must not be mocked):** %s\n\n",
		strings.Join(sm.RealComponents, ", ")))
	if !slim && len(sm.Setup) > 0 {
		sb.WriteString("**Setup:**\n\n")
		for _, line := range sm.Setup {
			sb.WriteString(fmt.Sprintf("- %s\n", line))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("**End state:**\n\n")
	for _, line := range sm.EndState {
		sb.WriteString(fmt.Sprintf("- %s\n", line))
	}
	sb.WriteString("\n")
	if pc := sm.PseudocodeText(); !slim && pc != "" {
		sb.WriteString("**Pseudocode:**\n\n")
		sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", pc))
	}
}

// ---------------------------------------------------------------------------
// tasks.json
// ---------------------------------------------------------------------------

// Render renders the tasks artifact as Markdown: the test commands, the
// dependencies and the tasks it stores. The integration task is derived from
// the whole package, so it is in Spec.RenderTasks and not here.
func (t *TasksV3Json) Render() string {
	var sb strings.Builder
	renderPlanContext(&sb, t)
	sb.WriteString("## Tasks\n\n")
	for _, task := range t.Tasks {
		renderTaskFull(&sb, PlannedTask{Task: task, Kind: TaskKindImplement}, &t.TestCommands)
	}
	return sb.String()
}

// RenderTasks renders the package's plan as Markdown: the test commands, the
// dependencies, every task of tasks.json and then the integration task
// (§8.4). No task state is rendered.
func (s *Spec) RenderTasks() string {
	var sb strings.Builder
	renderPlanContext(&sb, s.Tasks)
	sb.WriteString("## Tasks\n\n")
	for _, task := range s.Plan() {
		renderTaskFull(&sb, task, &s.Tasks.TestCommands)
	}
	return sb.String()
}

// renderPlanContext renders what the tasks artifact says about the whole
// plan rather than one task: the project's test commands and the specs this
// one depends on.
func renderPlanContext(sb *strings.Builder, t *TasksV3Json) {
	sb.WriteString("## Test Commands\n\n")
	sb.WriteString(fmt.Sprintf("- **all_tests:** %s\n", code(t.TestCommands.AllTests)))
	sb.WriteString(fmt.Sprintf("- **linter:** %s\n", code(t.TestCommands.Linter)))
	if t.TestCommands.SpecTests != nil && *t.TestCommands.SpecTests != "" {
		sb.WriteString(fmt.Sprintf("- **spec_tests:** %s\n", code(*t.TestCommands.SpecTests)))
	}
	if t.TestCommands.AffectedTests != nil && *t.TestCommands.AffectedTests != "" {
		sb.WriteString(fmt.Sprintf("- **affected_tests:** %s\n", code(*t.TestCommands.AffectedTests)))
	}
	sb.WriteString("\n## Dependencies\n\n")
	if len(t.Dependencies) == 0 {
		sb.WriteString("None: this spec depends on no other spec.\n\n")
		return
	}
	for _, d := range t.Dependencies {
		sb.WriteString(fmt.Sprintf("- %s — %s\n", code(d.Spec), d.Reason))
	}
	sb.WriteString("\n")
}

// code renders s as a Markdown code span, with a fence long enough for the
// backticks s contains.
func code(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// codeList renders each item as a code span, comma-separated.
func codeList(items []string) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = code(it)
	}
	return strings.Join(out, ", ")
}

// cell makes s safe inside a Markdown table cell: a pipe would end the cell
// and a newline the row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "<br>")
}

// renderTaskFull renders a task with its criteria, dependencies, steps,
// touches, done_when and the implicit definition of done (§7.2, §11.2). No
// state is rendered: the progress is not the contract.
func renderTaskFull(sb *strings.Builder, task PlannedTask, commands *TestCommands) {
	sb.WriteString(fmt.Sprintf("### %d. %s (%s)\n\n", task.Id, task.Title, task.Kind))

	if len(task.Criteria) > 0 {
		sb.WriteString(fmt.Sprintf("**Criteria:** %s\n\n", strings.Join(task.Criteria, ", ")))
	}

	if len(task.DependsOn) > 0 {
		deps := make([]string, len(task.DependsOn))
		for i, d := range task.DependsOn {
			deps[i] = fmt.Sprint(d)
		}
		sb.WriteString(fmt.Sprintf("**Depends on:** %s\n\n", strings.Join(deps, ", ")))
	}

	// Steps are numbered task.step, so one can be named outside its task.
	// The number is not an ordered-list marker, which Markdown would
	// renumber, so each step is a bullet.
	sb.WriteString("**Steps:**\n\n")
	for i, step := range task.Steps {
		sb.WriteString(fmt.Sprintf("- %d.%d %s\n", task.Id, i+1, step))
	}
	sb.WriteString("\n")

	if len(task.Touches) > 0 {
		sb.WriteString("**Touches:**\n\n")
		for _, f := range task.Touches {
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**Done when:**\n\n")
	if task.Kind == TaskKindIntegration {
		sb.WriteString("- every smoke check above runs with its real components and reaches its end state\n")
	} else {
		sb.WriteString("- the checks of the criteria listed above exist as tests, are executable and pass\n")
	}
	if commands != nil && commands.AllTests != "" {
		sb.WriteString(fmt.Sprintf("- `%s` passes\n", commands.AllTests))
	}
	if commands != nil && commands.Linter != "" {
		sb.WriteString(fmt.Sprintf("- `%s` passes\n", commands.Linter))
	}
	for _, d := range task.DoneWhen {
		if task.Kind == TaskKindIntegration {
			continue // the derived done_when restates the commands above
		}
		sb.WriteString(fmt.Sprintf("- %s\n", d))
	}
	sb.WriteString("\n")
}

// renderTaskSummary renders a task as a one-line summary.
func renderTaskSummary(sb *strings.Builder, task PlannedTask) {
	sb.WriteString(fmt.Sprintf("- %d. %s (%s)\n", task.Id, task.Title, task.Kind))
}

// RenderedFileName is the name of the file every save writes beside a
// package's artifacts: its whole render, for a reviewer to read in a diff. It
// is derived from the artifacts and is never read as data.
const RenderedFileName = "spec.md"

// RenderedHeader is the first line of RenderedFileName: it says where the
// file comes from and how to bring it up to date.
const RenderedHeader = "<!-- Generated by afspec from prd.md, requirements.json, tasks.json and architecture.md. Do not edit: change those files, or run speca <package> --render. -->"

// RenderedBytes is what RenderedFileName must hold for the package: the
// RenderedHeader line, a blank line, and the whole render, with no scope and
// no budget. It is what every save writes, and what a program that writes a
// package's files itself writes to RenderedFileName. The same package always
// gives the same bytes, whatever its progress.
func (s *Spec) RenderedBytes() []byte {
	render := s.RenderCombined()
	b := make([]byte, 0, len(RenderedHeader)+2+len(render))
	b = append(b, RenderedHeader...)
	b = append(b, "\n\n"...)
	return append(b, render...)
}
