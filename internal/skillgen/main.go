// Command skillgen renders the af-* skills under skills/ for each coding agent
// that loads skills from a directory, and installs them into that agent's skill
// directory.
//
//	go run ./internal/skillgen [--source DIR] [--out DIR]
//
// It is not installed: make runs it, so the repository's public surface does
// not grow. It makes no model call, no network call and reads no credential.
package main

import (
	"fmt"
	"io"
	"os"
)

// defaultSource is the skills tree the render reads when none is given.
const defaultSource = "skills"

// Main runs the render and returns the process exit code: 0 when the source
// tree holds no problem, 1 when it holds one.
func Main(argv []string, stdout, stderr io.Writer) int {
	root := defaultSource
	if len(argv) == 2 && argv[0] == "--source" {
		root = argv[1]
	}
	_, problems := readSource(os.DirFS(root), root)
	for _, p := range problems {
		fmt.Fprintln(stderr, p)
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

func main() { os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr)) }
