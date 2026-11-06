package specpkg

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/project"
)

// steeringVersionRule is the rule steering_version: a draft or active package
// that records the steering version it was written under is told the
// amendments the spec root's steering has had since (ADR 15). It reads the
// spec root, so it belongs to the verdict and not to the package's own
// validation. Whether a package still holds under a new directive is a
// judgment, so every finding is a warning: a person who has checked the
// package clears it by changing the field. It says nothing of a package that
// is history, of one that records no version, or under a steering that has
// none.
func steeringVersionRule(specsDir string, spec *afspec.Spec) []afspec.ValidationEntry {
	if spec.SteeringVersion == "" || (spec.Status != "draft" && spec.Status != "active") {
		return nil
	}
	steering := project.ReadSteering(specsDir)
	if !steering.Versioned() {
		return nil
	}
	pkg, now := spec.SteeringVersion, steering.Version
	warn := func(msg string) []afspec.ValidationEntry {
		return []afspec.ValidationEntry{{Category: "warning", Check: "steering_version", Artifact: "prd.md", EntityID: spec.SpecID, Message: msg}}
	}

	switch c := project.CompareVersions(pkg, now); {
	case c == 0:
		return nil
	case c > 0:
		return warn(fmt.Sprintf("the package was written under steering version %s, which is newer than the steering's version %s: "+
			"the steering was moved back, or steering_version is wrong", pkg, now))
	}
	if !slices.ContainsFunc(steering.Amendments, func(a project.Amendment) bool { return a.Version == pkg }) {
		return warn(fmt.Sprintf("the package was written under steering version %s, which is in no amendment of the steering's record; "+
			"the steering is at %s: check the package against the directives, then set steering_version to %s", pkg, now, now))
	}

	var since []string
	for _, a := range steering.Amendments {
		if project.CompareVersions(a.Version, pkg) <= 0 {
			break
		}
		since = append(since, fmt.Sprintf("%s (%s): %s", a.Version, a.Date, a.Change))
	}
	msg := fmt.Sprintf("the package was written under steering version %s, and the steering is at %s; the amendments since, newest first: %s. "+
		"Check the package against them, then set steering_version to %s", pkg, now, strings.Join(since, "; "), now)
	if project.MajorOf(now) > project.MajorOf(pkg) {
		msg += ". One of them raised the MAJOR number: a directive the package was written under has been removed or changed"
	}
	return warn(msg)
}
