// Package legacy holds the generated Go types for spec format version 1.3.
//
// The library reads, validates and writes format version 2 only (see
// specification/spec-format-v2.md in this module's repository). These types
// exist so that an embedder can load a v1 spec package with LoadSpec and
// convert it with afspec.Migrate; nothing else in this module depends on them.
// The generated UnmarshalJSON methods enforce required fields, enums and ID
// patterns, which is the only structural checking a migration input receives.
package legacy
