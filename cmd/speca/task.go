package main

import "github.com/agent-fox-dev/spec/internal/cli"

// executeTask is the task form: speca <spec> --task T --state S.
func executeTask(c *call, dir string) (*Result, *cli.ErrorInfo) {
	return nil, &cli.ErrorInfo{Stage: "load", Category: "input", Message: "the task form is not yet implemented"}
}
