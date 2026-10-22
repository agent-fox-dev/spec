package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// claimsIn lists the claim calls in a next, by the task they claim.
func claimsIn(next []envelope.Next) []string {
	var out []string
	for _, n := range next {
		if n.Tool == "speca" && len(n.Flags) == 4 && n.Flags[2] == "--state" && n.Flags[3] == "in_progress" {
			out = append(out, n.Flags[1])
		}
	}
	return out
}

// TS-13-14: speca decides drift and writability only through specpkg. Its
// envelopes say what they said: drift only for the package that drifted, the
// task form refused at stage status for it and for a sealed package, and the
// claim suggested only for a package that can take it. No source of the command
// declares a drift check of its own.
func TestSpecaDecidesDriftAndWritabilityThroughSpecpkg_TS13_14(t *testing.T) {
	root := newRepo(t)
	activePackage(t, root, "03_drift", "03", "drift", planTask(1, "pending", nil))
	editFile(t, filepath.Join(pkgDir(root, "03_drift"), "prd.md"), "Coding agents drive", "Coding agents might drive")
	activePackage(t, root, "04_ok", "04", "ok", planTask(1, "pending", nil))
	sealed := packageWith(t, root, "05_sealed", "05", "sealed", "sealed")
	_ = sealed

	for _, tc := range []struct {
		dir      string
		drift    bool
		writable bool
	}{{"09_agent_mode", false, true}, {"04_ok", false, true}, {"03_drift", true, false}, {"05_sealed", false, false}} {
		env, _, _ := run(t, "--dir", root, tc.dir)
		r := result(t, env)
		if r.IntentDrift != tc.drift {
			t.Errorf("%s: intent_drift = %v; want %v", tc.dir, r.IntentDrift, tc.drift)
		}
		if got := len(claimsIn(env.Next)) > 0; got != (tc.writable && tc.dir != "05_sealed") {
			t.Errorf("%s: the report suggests a claim: %v; want %v", tc.dir, got, tc.writable)
		}
		refused := tc.dir != "09_agent_mode" && tc.dir != "04_ok"
		if tc.dir == "09_agent_mode" || tc.dir == "04_ok" {
			continue
		}
		e, code, _ := run(t, "--dir", root, tc.dir, "--task", "1", "--state", "in_progress")
		if refused && (code != cli.ExitFailed || e.Error == nil || e.Error.Stage != "status" || e.Error.Category != "lifecycle") {
			t.Errorf("%s: the task form: code %d, error %+v; want it refused at stage status", tc.dir, code, e.Error)
		}
	}

	// The list claims the first active package that can take it: not the one that drifted.
	env, _, _ := run(t, "--dir", root, "--list")
	if got := claimsIn(env.Next); len(got) != 1 || len(env.Next) < 1 || env.Next[0].Input != ".specs/04_ok" {
		t.Errorf("the list suggests %+v; want the claim of the active package without drift, .specs/04_ok", env.Next)
	}

	// No source declares a drift check of its own, or computes an intent hash to
	// decide drift: the one use is the hash a dry-run activation would record.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == "intentDrift" || n.Name.Name == "writable" && n.Recv != nil {
					t.Errorf("%s declares %s", file, n.Name.Name)
				}
			case *ast.SelectorExpr:
				if n.Sel.Name == "ComputeIntentHash" && file != "status.go" {
					t.Errorf("%s calls ComputeIntentHash", file)
				}
			}
			return true
		})
	}
}

// An active package with an empty intent_hash has a hash that is set and is not
// the hash of its intent, which is what the library refuses to save. The old
// private check took an empty hash for an unset one.
func TestAnActivePackageWithAnEmptyHashHasDrifted(t *testing.T) {
	root := newRepo(t)
	dir := activePackage(t, root, "03_a", "03", "a", planTask(1, "pending", nil))
	prd, err := os.ReadFile(filepath.Join(dir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(prd), "\n")
	i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "intent_hash:") })
	if i < 0 {
		t.Fatalf("no intent_hash in prd.md:\n%s", prd)
	}
	lines[i] = `intent_hash: ""`
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	env, _, _ := run(t, "--dir", root, "03_a")
	if r := result(t, env); !r.IntentDrift {
		t.Errorf("intent_drift = false for a package whose hash is set and wrong")
	}
	e, code, _ := run(t, "--dir", root, "03_a", "--task", "1", "--state", "in_progress")
	if code != cli.ExitFailed || e.Error == nil || e.Error.Stage != "status" {
		t.Errorf("the task form: code %d, error %+v; want it refused at stage status", code, e.Error)
	}
}
