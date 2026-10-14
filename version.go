package spec

import (
	"runtime/debug"
	"strings"
)

// Build-time configurable variables, overridable via -ldflags.
var (
	// Version holds the semantic version string.
	Version = "dev"
	// Commit holds the short git commit SHA.
	Commit = "dev"
	// BuildTime holds the UTC build timestamp (e.g. "2025-06-01T00:00:00Z").
	BuildTime = ""
)

// Identity is the build identity the commands report: Version, followed by
// the commit and build time when the build set them, as in
// "v1.4.1 (commit 94f3890, built 2026-10-14T06:33:13Z)". A build that left
// Version at "dev" reports the module version go install recorded instead,
// when there is one.
func Identity() string {
	v := Version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	var details []string
	if Commit != "" && Commit != "dev" {
		details = append(details, "commit "+Commit)
	}
	if BuildTime != "" {
		details = append(details, "built "+BuildTime)
	}
	if len(details) == 0 {
		return v
	}
	return v + " (" + strings.Join(details, ", ") + ")"
}
