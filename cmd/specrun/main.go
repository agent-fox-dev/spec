// Command specrun drives the implementation loop: /af-impl on one spec
// package after another, each in a fresh claude -p session, until every
// package is done. See docs/specrun.md.
package main

import (
	"os"

	"github.com/agent-fox-dev/spec/internal/specrun"
)

func main() { os.Exit(specrun.Main(os.Args[1:], os.Stdout, os.Stderr)) }
