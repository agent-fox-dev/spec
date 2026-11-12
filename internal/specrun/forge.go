package specrun

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

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
