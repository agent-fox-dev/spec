// Command skillgen renders the af-* skills under skills/ for each coding agent
// that loads skills from a directory, and installs them into that agent's skill
// directory.
//
//	go run ./internal/skillgen [--source DIR] [--out DIR]
//
// It is not installed: make runs it, so the repository's public surface does
// not grow. It makes no model call, no network call and reads no credential,
// and it reports every problem a source tree holds rather than the first.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// defaultSource is the skills tree the render reads, and defaultOut where it
// writes, when neither is given.
const (
	defaultSource = "skills"
	defaultOut    = "build/skills"
)

// exit codes: a problem in the source tree is 1, a usage error 2.
const (
	exitOK      = 0
	exitProblem = 1
	exitUsage   = 2
)

// Main runs the render and returns the process exit code.
func Main(argv []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("skillgen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("source", defaultSource, "the skills tree to read")
	out := flags.String("out", defaultOut, "where to write the rendered skills")
	if err := flags.Parse(argv); err != nil {
		return exitUsage
	}
	if *source == "" || *out == "" {
		fmt.Fprintln(stderr, "skillgen: --source and --out must name a directory")
		return exitUsage
	}

	src, problems := readSource(os.DirFS(*source), *source)
	for _, p := range problems {
		fmt.Fprintln(stderr, p)
	}
	for _, w := range src.unlinked() {
		fmt.Fprintln(stderr, w)
	}
	// Nothing is written while the source tree holds a problem, so a failed run
	// leaves the previous output exactly as it was.
	if len(problems) > 0 {
		return exitProblem
	}
	return exitOK
}

func main() { os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr)) }
