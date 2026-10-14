package spec

import "testing"

// Identity adds the commit and build time only when a build set them.
func TestIdentity(t *testing.T) {
	defer func(v, c, b string) { Version, Commit, BuildTime = v, c, b }(Version, Commit, BuildTime)
	for _, tc := range []struct{ version, commit, built, want string }{
		{"v1.4.1", "94f3890", "2026-10-14T06:33:13Z", "v1.4.1 (commit 94f3890, built 2026-10-14T06:33:13Z)"},
		{"v1.4.1", "94f3890", "", "v1.4.1 (commit 94f3890)"},
		{"v1.4.1", "dev", "2026-10-14T06:33:13Z", "v1.4.1 (built 2026-10-14T06:33:13Z)"},
		{"v1.4.1", "dev", "", "v1.4.1"},
		{"dev", "dev", "", "dev"}, // go test records no module version
	} {
		Version, Commit, BuildTime = tc.version, tc.commit, tc.built
		if got := Identity(); got != tc.want {
			t.Errorf("Identity() with %q %q %q = %q, want %q", tc.version, tc.commit, tc.built, got, tc.want)
		}
	}
}
