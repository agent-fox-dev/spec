package specrun

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installSkill writes an af-impl SKILL.md under dir/.claude/skills and returns
// its path and SHA-256.
func installSkill(t *testing.T, dir, content string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, ".claude", "skills", "af-impl", "SKILL.md")
	writeFile(t, path, content)
	sum := sha256.Sum256([]byte(content))
	return path, hex.EncodeToString(sum[:])
}

// A run says what it runs: specrun's version, the skill a session here loads
// with its hash, and the versions of claude and issue.
func TestVersions_SayWhatARunRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		setup     func(t *testing.T, f *fixture) (path, sum string)
		withIssue bool
	}{
		"the skill in the user's directory": {func(t *testing.T, f *fixture) (string, string) {
			return installSkill(t, f.home, "the user's af-impl\n")
		}, true},
		"the project's skill wins": {func(t *testing.T, f *fixture) (string, string) {
			installSkill(t, f.home, "the user's af-impl\n")
			path, sum := installSkill(t, f.root, "the project's af-impl\n")
			f.commit(t, "the project's skill")
			git(t, f.root, "push", "-q")
			return path, sum
		}, true},
		"no issue on PATH": {func(t *testing.T, f *fixture) (string, string) {
			f.setenv("PATH", strings.Join(slices.DeleteFunc(filepath.SplitList(os.Getenv("PATH")), func(d string) bool {
				return exists(filepath.Join(d, "issue"))
			}), string(os.PathListSeparator)))
			return installSkill(t, f.home, "the user's af-impl\n")
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.addPackage(t, "01_a", "01", "pending")
			f.plan(t, land(0))
			path, sum := tc.setup(t, f)
			env, code, stderr := runMain(t)
			v := env.Result.Versions
			if code != 0 || v == nil {
				t.Fatalf("code %d, versions %+v, error %+v", code, v, env.Error)
			}
			if realPath(t, v.SkillPath) != realPath(t, path) || v.SkillSHA256 != sum {
				t.Errorf("skill %s %s; want %s %s", v.SkillPath, v.SkillSHA256, path, sum)
			}
			if v.Claude != "2.1.322 (Claude Code)" || v.Specrun == "" {
				t.Errorf("claude %q, specrun %q", v.Claude, v.Specrun)
			}
			if !tc.withIssue && v.Issue != "not installed" {
				t.Errorf("issue %q; want not installed", v.Issue)
			}
			if !strings.Contains(stderr, v.SkillSHA256) || !strings.Contains(stderr, v.Claude) {
				t.Errorf("stderr does not log the versions:\n%s", stderr)
			}
		})
	}
}

// A skill that is not the one specrun was built with, or none, is a warning,
// and with --strict-skills a refusal; with no embedded hash nothing can be
// compared, which only --strict-skills refuses.
func TestVersions_WarnOrRefuseOnASkillThatIsNotTheBuiltOne(t *testing.T) {
	const content = "the af-impl it was built with\n"
	sum := sha256.Sum256([]byte(content))
	same := hex.EncodeToString(sum[:])
	for _, embedded := range []string{"equal", "different", "empty"} {
		for _, installed := range []bool{true, false} {
			for _, strict := range []bool{false, true} {
				name := embedded + map[bool]string{true: ", installed", false: ", absent"}[installed] +
					map[bool]string{true: ", strict", false: ""}[strict]
				t.Run(name, func(t *testing.T) {
					f := newFixture(t)
					f.addPackage(t, "01_a", "01", "pending")
					f.plan(t, land(0))
					if installed {
						installSkill(t, f.home, content)
					}
					f.builtSkill = map[string]string{"equal": same, "different": strings.Repeat("0", 64), "empty": ""}[embedded]
					args := []string{}
					if strict {
						args = append(args, "--strict-skills")
					}
					env, code, _ := runMain(t, args...)
					var codes []string
					for _, w := range env.Warnings {
						codes = append(codes, string(w.Code))
					}
					wantCode := ""
					switch {
					case embedded == "empty":
					case !installed:
						wantCode = "skill_not_installed"
					case embedded == "different":
						wantCode = "skill_mismatch"
					}
					path := filepath.Join(f.home, ".claude", "skills", "af-impl", "SKILL.md")
					names := func(msg string) bool {
						installedHash := map[bool]string{true: same, false: "not installed"}[installed]
						builtHash := map[string]string{"equal": same, "different": strings.Repeat("0", 64), "empty": "unknown"}[embedded]
						return strings.Contains(msg, path) && strings.Contains(msg, installedHash) && strings.Contains(msg, builtHash)
					}
					if strict && !(embedded == "equal" && installed) {
						if code != 1 || env.Error == nil || env.Error.Stage != "preflight" || len(f.starts(t)) != 0 {
							t.Errorf("code %d, error %+v, %d sessions; want a refusal at preflight", code, env.Error, len(f.starts(t)))
						} else if !names(env.Error.Message) {
							t.Errorf("message %q does not name the path and both hashes", env.Error.Message)
						}
						return
					}
					for _, w := range env.Warnings {
						if !names(w.Message) {
							t.Errorf("warning %q does not name the path and both hashes", w.Message)
						}
					}
					if code != 0 || len(specDirs(f.starts(t))) != 1 {
						t.Errorf("code %d, error %+v, %d sessions; want the run to go on", code, env.Error, len(f.starts(t)))
					}
					if strings.Join(codes, ",") != wantCode {
						t.Errorf("warnings %v; want %q", codes, wantCode)
					}
					if embedded == "empty" && env.Result.Versions != nil && env.Result.Versions.BuiltSkillSHA256 != "unknown" {
						t.Errorf("built_skill_sha256 %q; want unknown", env.Result.Versions.BuiltSkillSHA256)
					}
				})
			}
		}
	}
}
