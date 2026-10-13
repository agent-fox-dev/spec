// Command spec turns a product idea into a complete, validated version 2
// specification package.
//
//	spec [flags] <text | file | issue-url | ->
//
// It takes exactly one input and writes exactly one JSON object to stdout.
// Progress goes to stderr, so the two never interleave.
package main

import (
	"context"
	"flag"
	"os"
	"runtime/debug"

	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/specgen"
)

// version is the build identity. A release build sets it with
// -ldflags "-X main.version=..."; otherwise it is the module version go
// install recorded, or "dev".
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

const usage = `spec — turn a product idea into a validated specification package

Usage:
  spec [flags] <input>

The input is exactly one of:
  a GitHub or GitLab issue or pull/merge-request URL
                                       the issue and its comments are the idea
  a path to a readable file            the file's contents are the idea
  any other text                       the text is the idea
  -                                    the idea is read from stdin

Output is one JSON object on stdout; progress goes to stderr.

--dry-run changes nothing: it writes no files under the spec root, posts no
comment on the issue, and reports the package that would have been written. --total-budget is
checked before each scope of a split begins its PRD phase (and a split that
stops there keeps its plan to resume from); an input that is one spec's worth
of work has no interior boundary, so there it lowers the one phase's ceiling
like --budget.

The run is unattended: nobody is waiting to answer questions, so the PRD phase
resolves every open question itself, records each in a '## Design Decisions'
section, and reports the ones it is least sure of as open_questions in the
result. It then generates requirements.json, test_spec.json and tasks.json in
that order — each validated against the format's schema and its cross-file
rules before it is written — writes the package under .specs/NN_name/, and
activates it if it validates.

An input that is more than one spec's worth of work is split: the PRD phase
reports every scope, and spec writes all of them, one package per scope, each
built on the ones before it. The split is recorded in the spec root as
<first_scope>.split.json until the last package is written; a run that stops
early is resumed by running spec on the same input again.

Everything the model does here is read-only. The only thing written is the
spec package itself, by this program, after the run.

Exit codes:
  0  a valid package was written
  1  failed; the stage is named in the JSON. An 'invalid_spec' failure still
     leaves the package on disk with the broken rules named. A split that
     stopped early names the scope, and the plan remains to resume from.
  2  usage error — nothing was fetched, nothing was written

Flags:
`

// specFlags holds the values of spec's own flags. Both Exec and
// PreflightExec read them through specOptions, so a check run under
// --preflight cannot pass against a configuration the real run would not use.
type specFlags struct {
	specsDir     string
	name         string
	architecture bool
	noActivate   bool
	comment      bool
}

// specOptions builds the specgen.Options for one run from the parsed flags
// and what the shell resolved.
func (f *specFlags) specOptions(d cli.Deps) specgen.Options {
	return specgen.Options{
		Index:            d.Index,
		IndexUnavailable: d.IndexUnavailable,
		Input:            d.Input,
		Workspace:        d.Workspace,
		SpecsDir:         f.specsDir,
		Name:             f.name,
		Architecture:     f.architecture,
		Activate:         !f.noActivate,
		Comment:          f.comment,
		DryRun:           d.Common.DryRun,
		TotalBudgetUSD:   d.Common.TotalBudgetUSD,
		RepoMapTokens:    d.Common.RepoMapTokens,
		Runner:           d.Runner,
		Forge:            d.Forge,
		Run:              d.Run,
		Progress:         d.Progress,
	}
}

// response maps a pipeline outcome onto the exit code, the result and the
// error object, the same way for the ordinary run and for --preflight. A
// failure before there is a result passes no result: a nil *Result in the
// interface is not a nil result.
func response(result *specgen.Result, err error) (int, any, *cli.ErrorInfo) {
	if err == nil {
		return cli.ExitOK, result, nil
	}
	info := cli.ErrorFrom("run", err)
	if result == nil {
		return cli.ExitCodeFor(info.Category), nil, info
	}
	return cli.ExitCodeFor(info.Category), result, info
}

func newApp() cli.App {
	f := &specFlags{}
	var common *cli.Common

	return cli.App{
		Name:    "spec",
		Version: buildVersion(),
		Usage:   usage,
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&f.specsDir, "specs-dir", "", "where NN_name packages live; default <dir>/"+specgen.DefaultSpecDirName+" or $"+specgen.SpecDirEnv)
			fs.StringVar(&f.name, "name", "", "override the spec name the model chooses; must match [a-z][a-z0-9]*(_[a-z0-9]+)*")
			fs.BoolVar(&f.architecture, "architecture", false, "also write the optional architecture.md")
			fs.BoolVar(&f.noActivate, "no-activate", false, "leave a valid package in draft instead of activating it")
			fs.BoolVar(&f.comment, "comment", false, "post the finished PRD back to the issue the input came from")
		},
		// Generating an artifact is one long structured answer plus however
		// many repairs the rules demand. The turn ceiling is the repair
		// budget, and 60 is enough for a model that is converging and a stop
		// for one that is not.
		DefaultBounds: agentrun.Bounds{MaxTurns: 60, MaxBudgetUSD: 5.00},
		// An input that does not split has one phase, so --total-budget lowers
		// its ceiling; a split also checks it between scopes.
		SinglePhase: true,

		PreCheck: func(c *cli.Common) error {
			common = c
			if f.name != "" && !specgen.ValidSpecName(f.name) {
				return cli.Usagef("--name %q must match [a-z][a-z0-9]*(_[a-z0-9]+)*", f.name)
			}
			return nil
		},
		CheckInput: func(in cli.Input) error {
			if f.comment && !common.DryRun && in.Issue == nil {
				return cli.Usagef("--comment posts the PRD back to the forge issue it came from (GitHub or GitLab), "+
					"and %s is %s", in.Origin, in.Kind)
			}
			return nil
		},
		Exec: func(ctx context.Context, d cli.Deps) (int, any, *cli.ErrorInfo) {
			return response(specgen.Run(ctx, f.specOptions(d)))
		},
		// --preflight: every check the ordinary run makes before its first
		// model call, against the identical options, then stop.
		PreflightExec: func(ctx context.Context, d cli.Deps) (int, any, *cli.ErrorInfo) {
			return response(specgen.RunPreflight(ctx, f.specOptions(d)))
		},
	}
}

func main() {
	os.Exit(newApp().Main(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
