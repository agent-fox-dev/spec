package main

import (
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// Result is speca's result. Its fields are the envelope package's
// AdminResult, which a program reading the output decodes into; the methods
// are what the shell asks of a result.
type Result struct {
	envelope.AdminResult
}

// Summary is the envelope's one sentence.
func (r *Result) Summary() string { return "" }

// Next suggests the call to make after this one.
func (r *Result) Next() []cli.Next { return nil }
