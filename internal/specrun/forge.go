package specrun

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/agent-fox-dev/aflib/envelope"
	"github.com/agent-fox-dev/aflib/issuex"
	"github.com/agent-fox-dev/spec/afspec"
)

// newForge makes the forge client of the repository at root: the forge of
// its origin remote, with the token the issue command reads. A test replaces
// it with an in-memory forge.
var newForge = defaultForge

// errNoForge is why a forge client could not be made.
var errNoForge = errors.New("no forge")

func defaultForge(root string) (issuex.Client, issuex.Repo, error) {
	repo, ok := issuex.DetectRepo(root)
	if !ok {
		return nil, issuex.Repo{}, fmt.Errorf("%w: the repository has no origin remote that names a GitHub or GitLab repository", errNoForge)
	}
	c, err := issuex.NewWithOptions(issuex.Options{Dir: root, Repo: repo, UserAgent: tool})
	if err != nil {
		return nil, repo, fmt.Errorf("%w: the forge of %s is not supported: %v", errNoForge, repo, err)
	}
	if !c.Authenticated() {
		return nil, repo, fmt.Errorf("%w: no token is configured for %s: set GITHUB_TOKEN or GH_TOKEN for GitHub, GITLAB_TOKEN for GitLab", errNoForge, repo)
	}
	return c, repo, nil
}

// warnForgeUnavailable is raised by a dry run that has no forge client.
const warnForgeUnavailable envelope.WarnCode = "forge_unavailable"

// connect makes the forge client. Without one a run refuses to start, since
// the label rules decide what the loop does next; a dry run warns and goes
// on, and writes to no forge in any case.
func (r *run) connect() *envelope.ErrorInfo {
	c, repo, err := newForge(r.root)
	if err != nil {
		if r.opts.dryRun {
			r.warnings = append(r.warnings, envelope.Warning{Code: warnForgeUnavailable, Severity: envelope.SeverityLow,
				Stage: "preflight", Message: err.Error()})
			return nil
		}
		return failure("preflight", "environment", "%v", err)
	}
	r.forge, r.repo = c, repo
	return nil
}

// The fixed labels of the scheme: the four kinds of an open item, the four
// severities of a defect, and the outcomes a person or a repair records.
var schemeLabels = []issuex.Label{
	{Name: "af:awaits", Color: "1d76db", Description: "an open item a named later package must use or wire"},
	{Name: "af:defect", Color: "d73a4a", Description: "delivered code that behaves otherwise than its spec asks"},
	{Name: "af:untested", Color: "fbca04", Description: "believed to work, but no test asserts it"},
	{Name: "af:deviation", Color: "c5def5", Description: "a recorded departure from the spec, for a person to decide"},
	{Name: "af:severity-security", Color: "b60205", Description: "a defect that exposes something"},
	{Name: "af:severity-data-loss", Color: "b60205", Description: "a defect that loses data"},
	{Name: "af:severity-correctness", Color: "e99695", Description: "a defect that gives a wrong result"},
	{Name: "af:severity-minor", Color: "f9d0c4", Description: "a defect of little consequence"},
	{Name: "af:accepted", Color: "0e8a16", Description: "a deviation a person accepted"},
	{Name: "af:rejected", Color: "5319e7", Description: "a deviation a person rejected"},
	{Name: "af:repair-failed", Color: "000000", Description: "a repair that did not land"},
}

// createScheme creates every label of the scheme, the fixed ones and
// af:from-<NN> and af:awaits-<NN> for every package under the spec root and
// its archive. issuex lists no labels, so one that exists is found by
// creating it: a conflict is no error.
func (r *run) createScheme(ctx context.Context) *envelope.ErrorInfo {
	labels := append([]issuex.Label{}, schemeLabels...)
	for _, id := range r.specIDs() {
		labels = append(labels,
			issuex.Label{Name: "af:from-" + id, Color: "ededed", Description: "an open item package " + id + " opened"},
			issuex.Label{Name: "af:awaits-" + id, Color: "ededed", Description: "an open item package " + id + " must take up"})
	}
	for _, l := range labels {
		if err := r.forge.CreateLabel(ctx, r.repo, l); err != nil && !issuex.IsConflict(err) {
			return failure("preflight", "forge", "the label %s could not be created on %s: %v", l.Name, r.repo, err)
		}
	}
	return nil
}

// specIDs is the spec id of every package under the spec root and archive/
// that loads, in number order.
func (r *run) specIDs() []string {
	var ids []string
	for _, dir := range []string{r.specsDir, filepath.Join(r.specsDir, "archive")} {
		names, _ := afspec.ListSpecDirs(dir)
		for _, n := range names {
			if spec, err := afspec.LoadSpec(filepath.Join(dir, n)); err == nil {
				ids = append(ids, spec.SpecID)
			}
		}
	}
	return ids
}

// The kinds of an open item, one label each.
var kindLabels = []string{"af:awaits", "af:defect", "af:untested", "af:deviation"}

// labelChange is what a label rule does to one issue: the labels it adds and
// removes, and the comment that says why.
type labelChange struct {
	Issue   int      `json:"issue"`
	Rule    string   `json:"rule"`
	Add     []string `json:"add"`
	Remove  []string `json:"remove"`
	Comment string   `json:"-"`
}

// The label rules, by name.
const (
	ruleIncompleteLabels = "incomplete-labels"
	ruleAwaitsFinished   = "awaits-finished"
)

// incompleteLabels is the rule incomplete-labels over the open issues of the
// package id: an issue that lacks a kind, carries two, lacks af:from-<id>, is
// a defect without exactly one severity, or is awaits without an
// af:awaits-<MM>, becomes a deviation (a person decides it), keeps or gains
// af:from-<id>, and gets a comment naming the rule, what was missing and the
// base commit. A complete issue gets no change, so the rule changes nothing
// the second time.
func incompleteLabels(issues []issuex.Issue, id, base string) []labelChange {
	var out []labelChange
	for _, is := range issues {
		var kinds, severities []string
		awaits := false
		for _, l := range is.Labels {
			switch {
			case slices.Contains(kindLabels, l):
				kinds = append(kinds, l)
			case strings.HasPrefix(l, "af:severity-"):
				severities = append(severities, l)
			case strings.HasPrefix(l, "af:awaits-"):
				awaits = true
			}
		}
		from := "af:from-" + id
		var missing []string
		switch {
		case len(kinds) == 0:
			missing = append(missing, "it has no kind")
		case len(kinds) > 1:
			missing = append(missing, "it has "+strings.Join(kinds, " and ")+", two kinds")
		case kinds[0] == "af:defect" && len(severities) != 1:
			missing = append(missing, "it is a defect without exactly one af:severity- label")
		case kinds[0] == "af:awaits" && !awaits:
			missing = append(missing, "it is awaits without an af:awaits-<NN> label naming the package that takes it up")
		}
		if !slices.Contains(is.Labels, from) {
			missing = append(missing, "it lacks "+from)
		}
		if len(missing) == 0 {
			continue
		}
		c := labelChange{Issue: is.Number, Rule: ruleIncompleteLabels}
		for _, k := range kinds {
			if k != "af:deviation" {
				c.Remove = append(c.Remove, k)
			}
		}
		for _, l := range []string{"af:deviation", from} {
			if !slices.Contains(is.Labels, l) {
				c.Add = append(c.Add, l)
			}
		}
		c.Comment = fmt.Sprintf("specrun, rule incomplete-labels, on %s: %s. It is now af:deviation, for a person to decide.",
			base, strings.Join(missing, "; "))
		out = append(out, c)
	}
	return out
}

// awaitsFinished is the rule awaits-finished over the open issues labelled
// af:awaits: one whose af:awaits-<NN> names a package that is finished, or no
// package at all, will never be taken up, and becomes a correctness defect:
// its awaits labels removed, af:defect and af:severity-correctness added, and
// a comment naming the rule, the package and the base commit. known reports
// whether a package with the id exists, and whether it is finished.
func awaitsFinished(issues []issuex.Issue, known func(id string) (exists, finished bool), base string) []labelChange {
	var out []labelChange
	for _, is := range issues {
		if is.State != "open" || !slices.Contains(is.Labels, "af:awaits") {
			continue
		}
		for _, l := range is.Labels {
			id, ok := strings.CutPrefix(l, "af:awaits-")
			if !ok {
				continue
			}
			exists, finished := known(id)
			if exists && !finished {
				continue
			}
			why := "the package " + id + " it awaited has landed"
			if !exists {
				why = "no package " + id + " exists to take it up"
			}
			c := labelChange{Issue: is.Number, Rule: ruleAwaitsFinished, Remove: []string{"af:awaits", l},
				Comment: fmt.Sprintf("specrun, rule awaits-finished, on %s: %s, so nothing will close it. "+
					"It is now a correctness defect.", base, why)}
			for _, add := range []string{"af:defect", "af:severity-correctness"} {
				if !slices.Contains(is.Labels, add) {
					c.Add = append(c.Add, add)
				}
			}
			out = append(out, c)
			break
		}
	}
	return out
}

// knownPackages tells, by spec id, whether a package exists under the spec
// root or its archive, and whether it is finished: one under archive/ is.
func (r *run) knownPackages() func(id string) (bool, bool) {
	finished := map[string]bool{}
	for _, sub := range []string{"", "archive"} {
		dir := filepath.Join(r.specsDir, sub)
		names, _ := afspec.ListSpecDirs(dir)
		for _, n := range names {
			if spec, err := afspec.LoadSpec(filepath.Join(dir, n)); err == nil {
				f, seen := finished[spec.SpecID]
				finished[spec.SpecID] = (!seen || f) && (sub == "archive" || spec.IsFinished())
			}
		}
	}
	return func(id string) (bool, bool) {
		f, ok := finished[id]
		return ok, f
	}
}

// labelChanges is what the label rules would change after the package in
// specDir landed, as the labels now stand: incomplete-labels over its issues,
// then awaits-finished over every awaits issue. A package found nowhere has
// no issues to check, and nil spec.
func (r *run) labelChanges(specDir string) ([]labelChange, error) {
	spec := r.findPackage(specDir)
	base, _ := gitOutput(r.root, "rev-parse", "HEAD")
	var out []labelChange
	if spec != nil {
		issues, err := r.packageIssues(spec.SpecID, filepath.Base(specDir))
		if err != nil {
			return nil, fmt.Errorf("%s: listing the issues of %s: %w", ruleIncompleteLabels, specDir, err)
		}
		out = incompleteLabels(issues, spec.SpecID, base)
	} else {
		fmt.Fprintf(r.stderr, "%s: the package %s is gone, and its issues are not checked\n", tool, specDir)
	}
	awaiting, err := r.listOpen([]string{"af:awaits"})
	if err != nil {
		return nil, fmt.Errorf("%s: listing the awaits issues: %w", ruleAwaitsFinished, err)
	}
	// An issue incomplete-labels changes is not judged again in this pass.
	awaiting = slices.DeleteFunc(awaiting, func(is issuex.Issue) bool {
		return slices.ContainsFunc(out, func(c labelChange) bool { return c.Issue == is.Number })
	})
	return append(out, awaitsFinished(awaiting, r.knownPackages(), base)...), nil
}

// findPackage loads the package in specDir, or under archive/ when it has
// moved there; nil when it is found nowhere.
func (r *run) findPackage(specDir string) *afspec.Spec {
	if spec, err := afspec.LoadSpec(filepath.Join(r.root, filepath.FromSlash(specDir))); err == nil {
		return spec
	}
	if r.specsDir != "" {
		if spec, err := afspec.LoadSpec(filepath.Join(r.specsDir, "archive", filepath.Base(specDir))); err == nil {
			return spec
		}
	}
	return nil
}

// listOpen lists the open issues carrying every one of labels, a listing
// that is incomplete asked for again with a higher limit.
func (r *run) listOpen(labels []string) ([]issuex.Issue, error) {
	for limit := 100; ; limit *= 4 {
		l, err := r.forge.ListIssues(r.ctx, r.repo, issuex.IssueFilter{State: "open", Labels: labels, Limit: limit})
		if err != nil {
			return nil, err
		}
		if !l.Incomplete || limit >= 25600 {
			return l.Issues, nil
		}
	}
}

// packageIssues lists the open issues of the package: those labelled
// af:from-<id>, and those whose title starts with its directory name and a
// colon, as af-impl titles an open item; a listing that is incomplete is
// asked for again with a higher limit.
func (r *run) packageIssues(id, dirName string) ([]issuex.Issue, error) {
	labelled, err := r.listOpen([]string{"af:from-" + id})
	if err != nil {
		return nil, err
	}
	all, err := r.listOpen(nil)
	if err != nil {
		return nil, err
	}
	out := labelled
	for _, is := range all {
		if strings.HasPrefix(is.Title, dirName+":") && !slices.ContainsFunc(out, func(o issuex.Issue) bool { return o.Number == is.Number }) {
			out = append(out, is)
		}
	}
	slices.SortFunc(out, func(a, b issuex.Issue) int { return a.Number - b.Number })
	return out, nil
}

// checkLabels applies the label rules after the package in specDir landed.
// A change's comment is recorded as pending in the state before its labels
// change, so that a comment cut short is posted later. A forge call that
// fails returns its error: the rules after it would decide on labels that are
// not known.
func (r *run) checkLabels(specDir string) error {
	changes, err := r.labelChanges(specDir)
	if err != nil {
		return err
	}
	for _, c := range changes {
		ref := issuex.IssueRef{Repo: r.repo, Number: c.Issue}
		r.state.PendingComments = append(r.state.PendingComments, pendingComment{Issue: c.Issue, Body: c.Comment})
		r.saveState()
		if len(c.Add) > 0 {
			if err := r.forge.AddLabels(r.ctx, ref, c.Add); err != nil {
				return fmt.Errorf("%s: issue #%d: %w", c.Rule, c.Issue, err)
			}
		}
		for _, l := range c.Remove {
			if err := r.forge.RemoveLabel(r.ctx, ref, l); err != nil {
				return fmt.Errorf("%s: issue #%d: %w", c.Rule, c.Issue, err)
			}
		}
		if err := r.postPending(); err != nil {
			return err
		}
		fmt.Fprintf(r.stderr, "%s: issue #%d: %s added %v, removed %v\n", tool, c.Issue, c.Rule, c.Add, c.Remove)
	}
	return nil
}

// postPending posts the comments the state owes, each removed once posted.
func (r *run) postPending() error {
	for len(r.state.PendingComments) > 0 {
		p := r.state.PendingComments[0]
		if _, err := r.forge.AddComment(r.ctx, issuex.IssueRef{Repo: r.repo, Number: p.Issue}, p.Body); err != nil {
			return fmt.Errorf("incomplete-labels: the comment on issue #%d: %w", p.Issue, err)
		}
		r.state.PendingComments = r.state.PendingComments[1:]
		r.saveState()
	}
	return nil
}
