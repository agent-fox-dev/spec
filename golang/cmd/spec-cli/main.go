// Package main is the entry point for the spec CLI binary.
// The version variable is set at build time via ldflags:
//
//	go build -ldflags "-X main.version=1.0.0" ./cmd/spec-cli
package main

import (
	spec "github.com/agent-fox-dev/spec/cmd/spec"
)

// version is set at build time via ldflags (-X main.version=...).
var version = "dev"

func main() {
	spec.SetVersion(version)
	spec.Execute()
}
