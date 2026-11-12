// Command skillgen renders the af-* skills under skills/ for each coding agent
// that loads skills from a directory, and installs them into that agent's skill
// directory.
//
//	go run ./internal/skillgen [--source DIR] [--out DIR] [--check]
//	go run ./internal/skillgen --install   [--agent A] [--project DIR] [--skills-dir DIR]
//	go run ./internal/skillgen --uninstall [--agent A] [--project DIR] [--skills-dir DIR]
//
// It is not installed: make runs it, so the repository's public surface does
// not grow. It makes no model call, no network call and reads no credential,
// and it reports every problem a source tree holds rather than the first.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	check := flags.Bool("check", false, "run the checks and write nothing")
	doInstall := flags.Bool("install", false, "install the skills for one agent")
	doUninstall := flags.Bool("uninstall", false, "remove the installed skills of one agent")
	hashOf := flags.String("sha256", "", "print the SHA-256 of this skill's SKILL.md as rendered for --agent, what an install writes, and write nothing")
	opts := options{}
	flags.StringVar(&opts.agent, "agent", defaultAgent, "the agent to install for")
	flags.StringVar(&opts.project, "project", "", "install into this repository instead of the user's directory")
	flags.StringVar(&opts.skillsDir, "skills-dir", "", "install into this directory, whatever the agent")
	if err := flags.Parse(argv); err != nil {
		return exitUsage
	}
	if *source == "" || *out == "" {
		fmt.Fprintln(stderr, "skillgen: --source and --out must name a directory")
		return exitUsage
	}
	// The render replaces its output directory whole, so a mistaken --out would
	// take the tree it names with it.
	if clean := filepath.Clean(*out); clean == "." || clean == string(filepath.Separator) ||
		clean == filepath.Clean(*source) {
		fmt.Fprintf(stderr, "skillgen: --out %s would be replaced whole; name a directory of its own\n", *out)
		return exitUsage
	}

	if *doInstall && *doUninstall {
		fmt.Fprintln(stderr, "skillgen: --install and --uninstall are two runs, not one")
		return exitUsage
	}
	var target string
	var chosen agent
	if *doInstall || *doUninstall {
		var err error
		if chosen, target, err = resolveInstall(opts); err != nil {
			fmt.Fprintln(stderr, err)
			return exitUsage
		}
	}
	// An uninstall removes directories and renders nothing, so it does not read
	// a SKILL.md and works even when one of them no longer parses.
	if *doUninstall {
		names, err := skillDirs(os.DirFS(*source))
		if err != nil {
			fmt.Fprintln(stderr, "skillgen: "+err.Error())
			return exitProblem
		}
		if err := uninstall(names, target, stdout); err != nil {
			fmt.Fprintln(stderr, "skillgen: "+err.Error())
			return exitProblem
		}
		return exitOK
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
	if *check {
		return exitOK
	}
	if *hashOf != "" {
		return src.printHash(*hashOf, opts.agent, stdout, stderr)
	}
	if *doInstall {
		if err := src.install(chosen, target, opts, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "skillgen: "+err.Error())
			return exitProblem
		}
		return exitOK
	}
	if err := src.write(*out); err != nil {
		fmt.Fprintln(stderr, "skillgen: "+err.Error())
		return exitProblem
	}
	return exitOK
}

func main() { os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr)) }

// printHash prints the SHA-256 of the skill's SKILL.md as rendered for the
// agent, which is what an install of this tree writes: specrun is built with
// it, to tell the installed af-impl from another.
func (s *source) printHash(name, agentName string, stdout, stderr io.Writer) int {
	a, ok := lookupAgent(agentName)
	if !ok {
		fmt.Fprintf(stderr, "skillgen: no agent %q\n", agentName)
		return exitUsage
	}
	for _, sk := range s.skills {
		if sk.name == name {
			sum := sha256.Sum256([]byte(renderSkill(sk, a).doc))
			fmt.Fprintln(stdout, hex.EncodeToString(sum[:]))
			return exitOK
		}
	}
	fmt.Fprintf(stderr, "skillgen: no skill %q under the source\n", name)
	return exitUsage
}
