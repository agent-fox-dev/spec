package spec

import "testing"

func TestIdentity_AddsTheCommitAndBuildTimeOnlyWhenABuildSetThem(t *testing.T) {
	defer func(v, c, b string) { Version, Commit, BuildTime = v, c, b }(Version, Commit, BuildTime)
	for _, tc := range []struct{ name, version, commit, built, want string }{
		{"commit and build time", "v1.4.1", "94f3890", "2026-10-14T06:33:13Z", "v1.4.1 (commit 94f3890, built 2026-10-14T06:33:13Z)"},
		{"commit only", "v1.4.1", "94f3890", "", "v1.4.1 (commit 94f3890)"},
		{"build time only", "v1.4.1", "dev", "2026-10-14T06:33:13Z", "v1.4.1 (built 2026-10-14T06:33:13Z)"},
		{"neither", "v1.4.1", "dev", "", "v1.4.1"},
		{"an unstamped build", "dev", "dev", "", "dev"}, // go test records no module version
	} {
		t.Run(tc.name, func(t *testing.T) {
			Version, Commit, BuildTime = tc.version, tc.commit, tc.built
			if got := Identity(); got != tc.want {
				t.Errorf("Identity() with %q %q %q = %q, want %q", tc.version, tc.commit, tc.built, got, tc.want)
			}
		})
	}
}
