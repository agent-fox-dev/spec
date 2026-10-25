package afspec

import (
	"path/filepath"
	"testing"
)

func TestResolveSpecsDir_PrefersTheFlagThenTheEnvironment(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	abs := filepath.Join(string(filepath.Separator), "elsewhere", "specs")
	for _, tc := range []struct {
		name, flag, env, want string
	}{
		{"neither", "", "", filepath.Join(root, ".specs")},
		{"a relative flag", "specs", "", filepath.Join(root, "specs")},
		{"an absolute flag", abs + "/", "", abs},
		{"the flag wins over the environment", "specs", "env", filepath.Join(root, "specs")},
		{"a blank flag falls through", "  ", "env", filepath.Join(root, "env")},
		{"an absolute environment", "", abs, abs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(SpecDirEnv, tc.env)
			if got := ResolveSpecsDir(tc.flag, root); got != tc.want {
				t.Errorf("ResolveSpecsDir(%q, %q) with %s=%q = %q; want %q", tc.flag, root, SpecDirEnv, tc.env, got, tc.want)
			}
		})
	}
}
