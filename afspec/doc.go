// Package afspec is the reference Go library for the agent-fox specification
// format, version 2. It loads, validates, mutates, saves and renders spec
// packages with byte-for-byte round-trip fidelity. The format is specified in
// specification/spec-format-v2.md in this module's repository.
//
// Spec structs are treated as value types — mutation methods such as Transition
// and Supersede return new Spec copies rather than modifying the receiver in
// place.
//
// # Concurrency
//
// This package provides no goroutine-safety guarantees. Spec instances are not
// safe for concurrent use by multiple goroutines. Callers are responsible for
// synchronizing concurrent access to Spec instances externally (e.g. using a
// sync.Mutex or by confining each Spec to a single goroutine).
package afspec
