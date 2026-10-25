package specgen

import (
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

func nextFor(r *Result, tool string) []cli.Next {
	var out []cli.Next
	for _, n := range r.Next() {
		if n.Tool == tool {
			out = append(out, n)
		}
	}
	return out
}

// impl is suggested on every written package that validates, in split order,
// with the ready count when there are several.
func TestNext_SuggestsImplOnEveryValidPackage(t *testing.T) {
	valid := ValidationReport{Valid: true}
	r := &Result{}
	r.Package = Package{SpecDir: ".specs/01_a", SpecID: "01"}
	r.FollowOnSpecs = []Package{
		{SpecDir: ".specs/02_b", SpecID: "02", Validation: valid},
		{SpecDir: ".specs/03_c", SpecID: "03", Validation: valid},
		{SpecDir: ".specs/04_d", SpecID: "04"},
	}
	impl := nextFor(r, "impl")
	if len(impl) != 2 || impl[0].Input != ".specs/02_b" || impl[1].Input != ".specs/03_c" {
		t.Fatalf("impl entries = %+v, want 02_b then 03_c", impl)
	}
	if want := "the package validates and is ready to implement (2 of this run's packages are ready, in split order)"; impl[0].Why != want {
		t.Errorf("why = %q, want %q", impl[0].Why, want)
	}
	if impl[0].Flags == nil || len(impl[0].Flags) != 0 {
		t.Errorf("flags = %#v, want a non-nil empty slice", impl[0].Flags)
	}

	// The first package, when it validates, comes first; one ready package
	// keeps the plain reason.
	r.Package.Validation.Valid = true
	r.FollowOnSpecs = nil
	if one := nextFor(r, "impl"); len(one) != 1 || one[0].Input != ".specs/01_a" ||
		one[0].Why != "the package validates and is ready to implement" {
		t.Errorf("a single ready package = %+v", one)
	}

	// A dry run wrote nothing to implement.
	r.DryRun = true
	if got := nextFor(r, "impl"); len(got) != 0 {
		t.Errorf("a dry run suggests %+v", got)
	}
}

// An unfinished split suggests spec again on the same input: the literal
// origin when it has one, the placeholder for text and stdin.
func TestNext_SuggestsSpecAgainOnAnUnfinishedSplit(t *testing.T) {
	for _, tc := range []struct {
		name, ref, want string
	}{
		{"a file", "docs/brief.md", "docs/brief.md"},
		{"text", cli.ResumePlaceholder(cli.Input{Kind: cli.KindText, Origin: "argument", Body: "a long brief"}), "<same input>"},
		{"stdin", cli.ResumePlaceholder(cli.Input{Kind: cli.KindStdin, Origin: "stdin", Body: "a long brief"}), "<same input>"},
		{"an input never recorded", "", "<same input>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Result{inputRef: tc.ref}
			r.SplitPlan = ".specs/01_a.split.json"
			if n := nextFor(r, "spec"); len(n) != 1 || n[0].Input != tc.want {
				t.Errorf("spec entries = %+v, want input %q", n, tc.want)
			}
		})
	}
}

// With nothing that validates and no split unfinished there is nothing to
// suggest.
func TestNext_IsEmptyWithNothingToSuggest(t *testing.T) {
	r := &Result{}
	r.SpecDir = ".specs/01_a"
	if n := r.Next(); n != nil {
		t.Errorf("Next() = %+v, want nil", n)
	}
	if n := (*Result)(nil).Next(); n != nil {
		t.Errorf("a nil result's Next() = %+v, want nil", n)
	}
}
