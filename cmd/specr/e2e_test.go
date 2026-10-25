package main

// The tests here run the speca calls that specr's next suggests against the
// real, built speca: only a built binary proves that a suggested call really
// works, and that the two commands agree on which task to claim.

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// An agent renders the package, runs the speca call next suggests first, and
// tasks.json records the move: a whole render's claim moves task 1 to in
// progress, and the render of a task in progress offers the call that records
// it done.
func TestTheSpecaCallNextSuggestsWorksAgainstTheRealSpeca(t *testing.T) {
	bin := buildSpeca(t)
	for _, tc := range []struct {
		name   string
		states []string
		argv   []string
		flags  []string
		want   string
	}{
		{"the claim after a whole render", []string{"pending", "pending", "pending"}, []string{"09"},
			[]string{"--task", "1", "--state", "in_progress"}, "in_progress"},
		{"the done call after a task render", []string{"in_progress", "pending", "pending"}, []string{"09", "--task", "1"},
			[]string{"--task", "1", "--state", "done"}, "done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := exampleWith(t, tc.states, nil)
			env, code, _ := run(t, append([]string{"--dir", root}, tc.argv...)...)
			if code != cli.ExitOK || len(env.Next) == 0 || env.Next[0].Tool != "speca" || !slices.Equal(env.Next[0].Flags, tc.flags) {
				t.Fatalf("code %d, next = %+v; want speca %v first", code, env.Next, tc.flags)
			}
			call := env.Next[0]
			out, status := speca(t, bin, append([]string{"--dir", root, call.Input}, call.Flags...)...)
			if status != cli.ExitOK || !out.OK || out.Result == nil || len(out.Result.Changes) != 1 {
				t.Fatalf("speca %s: exit %d, error %+v", strings.Join(call.Flags, " "), status, out.Error)
			}
			if got := taskState(t, root, 1); got != tc.want {
				t.Errorf("tasks.json says task 1 is %s; want %s", got, tc.want)
			}
		})
	}
}

// For a package in any state, specr and speca claim the same task, or neither
// does, and the task claimed is the one specr's render suggestion names. The
// states are the tasks' states and their dependencies on earlier tasks; a
// dependency on a later one is a package that does not validate, and has its
// own test below.
func TestSpecrAndSpecaClaimTheSameTask(t *testing.T) {
	bin := buildSpeca(t)
	taskStates := []string{"pending", "in_progress", "done", "dropped"}
	depVariants := [][][]int{
		{nil, nil, nil},    // each depends on the one before it
		{{}, {}, {}},       // none depends on any
		{nil, {1}, {1, 2}}, // as the example is written
		{{}, {1}, nil},     // mixed
		{nil, nil, {}},     // the last depends on none, behind two that depend on the one before
		{{}, nil, {}},      // the first and the last depend on none, the second on the first
	}
	type state struct {
		status string
		states []string
		deps   [][]int
	}
	// Every combination of the three tasks' states, each with a shape of the
	// dependencies drawn from a fixed seed, and a few of the same for each of the
	// other statuses.
	rng := rand.New(rand.NewPCG(13, 22))
	var all []state
	for _, a := range taskStates {
		for _, b := range taskStates {
			for _, c := range taskStates {
				all = append(all, state{"draft", []string{a, b, c}, depVariants[rng.IntN(len(depVariants))]})
			}
		}
	}
	// A package whose first task is dropped, so that the next depends on a task that
	// is never done, while the last is ready (see the invariant below).
	all = append(all,
		state{"draft", []string{"dropped", "pending", "pending"}, depVariants[4]},
		state{"draft", []string{"dropped", "pending", "pending"}, depVariants[5]},
		state{"active", []string{"dropped", "pending", "pending"}, depVariants[4]})
	for _, status := range []string{"active", "unhashed", "drifted", "sealed", "superseded", "archived"} {
		for i := 0; i < 6; i++ {
			s := all[rng.IntN(64)]
			all = append(all, state{status, s.states, depVariants[rng.IntN(len(depVariants))]})
		}
	}

	// Each state is its own repository, so the states run side by side: the
	// time is in starting speca.
	var claimed, stuck atomic.Int32
	t.Run("states", func(t *testing.T) {
		for i, s := range all {
			where := fmt.Sprintf("%s %v deps %v", s.status, s.states, s.deps)
			t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
				t.Parallel()
				root := newRepo(t)
				prepareStatus(t, root, s.status)
				plan(t, filepath.Join(root, ".specs", example), s.states, s.deps)
				env, _, _ := run(t, "--dir", root, "09")
				a := claims(env.Next)
				theirs, _ := speca(t, bin, "--dir", root, "09")
				b := claims(theirs.Next)
				renders := byTool(env.Next, "specr")
				switch {
				case len(a) > 0:
					// specr never claims what speca would not, and names the task it renders.
					claimed.Add(1)
					if !slices.EqualFunc(a, b, slices.Equal) {
						t.Errorf("%s: specr claims %v, speca %v", where, a, b)
					} else if len(renders) != 1 || renders[0].Flags[1] != a[0][1] {
						t.Errorf("%s: the claim names task %s, the render suggestion %+v", where, a[0][1], renders)
					}
				case len(b) > 0:
					// speca claims the first ready task, specr the first unfinished one: they
					// differ only when that task is stuck, behind a dependency that is never done,
					// and a task dropped is such a dependency. Without a dropped task they agree.
					stuck.Add(1)
					if !slices.Contains(s.states, "dropped") {
						t.Errorf("%s: speca claims %v and specr nothing, though no task is dropped", where, b)
						return
					}
					if len(renders) != 1 {
						t.Errorf("%s: specr suggests %+v", where, env.Next)
						return
					}
					m, err := strconv.Atoi(renders[0].Flags[1])
					if err != nil {
						t.Fatalf("%s: the render suggestion names no task: %+v", where, renders[0])
					}
					if task := env.Result.Tasks[m-1]; task.State != "pending" || task.Ready {
						t.Errorf("%s: speca claims %v and specr nothing, yet its task %d is %+v", where, b, m, task)
					}
				}
			})
		}
	})
	if n := int32(len(all)); claimed.Load() == 0 || stuck.Load() == 0 || claimed.Load() == n {
		t.Errorf("%d of %d states were claimed and %d were stuck; the property was checked of one kind only", claimed.Load(), n, stuck.Load())
	}
}

// When the first unfinished task is stuck, behind a dependency that is never
// done, while a later task is ready, specr offers no claim, and speca claims the
// later ready task: specr never claims what speca would not. A task that depends
// on a later one is such a stuck task, and the package fails validation; so is
// one that depends on a task dropped, and that package validates.
func TestWhereTheFirstTaskIsStuckSpecrClaimsNothingSpecaWould(t *testing.T) {
	bin := buildSpeca(t)
	for _, tc := range []struct {
		name   string
		states []string
		deps   [][]int
		valid  bool
	}{
		{"a dependency on a later task", []string{"done", "pending", "pending"}, [][]int{nil, {3}, {1}}, false},
		{"a dependency on a dropped task", []string{"dropped", "pending", "pending"}, [][]int{nil, nil, {}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := exampleWith(t, tc.states, tc.deps)
			env, _, _ := run(t, "--dir", root, "09")
			if got := calls(env.Next); !slices.Equal(got, []string{"specr --task 2"}) {
				t.Errorf("specr: next = %v; want the render of task 2 alone", got)
			}
			theirs, _ := speca(t, bin, "--dir", root, "09")
			if got := claims(theirs.Next); len(got) != 1 || got[0][1] != "3" {
				t.Errorf("speca: claims %v; want the claim of the ready task 3", got)
			}
			if theirs.Result == nil || theirs.Result.Valid == nil || *theirs.Result.Valid != tc.valid {
				t.Errorf("the package validates: %v; want %v", theirs.Result != nil && theirs.Result.Valid != nil && *theirs.Result.Valid, tc.valid)
			}
		})
	}
}
