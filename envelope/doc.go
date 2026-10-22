// Package envelope defines the JSON that the spec, specr and speca commands
// write to stdout, as Go types a program can decode it into.
//
// Every command writes exactly one JSON object on every program-driven path,
// including the failing ones: an Envelope. Its result property holds the
// command's own result, and which type that is depends on the command:
//
//	spec   a SpecResult    the package(s) a run wrote
//	specr  a RenderResult  a package rendered as Markdown
//	speca  an AdminResult  a package's state, its verdict, the list of packages, or a change to its tasks
//
// Decode a command's output with the function for its result:
//
//	env, err := envelope.DecodeSpecr(stdout)
//	if err != nil {
//		return err // not an envelope at all
//	}
//	if !env.OK {
//		return env.Error // an *ErrorInfo: stage, category, message, fix hint
//	}
//	md := env.Result.Markdown
//
// Decode is the same for any other result type, and Envelope[any] reads a
// result of a shape the caller does not know.
//
// The package depends on the standard library only, so importing it pulls in
// neither the commands' machinery nor a model SDK. The commands themselves
// build their output from these types, so the two cannot drift apart: a
// field added to the output is a field added here.
//
// # Compatibility
//
// The types follow the commands' output. Fields are added over time and a
// decoder ignores the ones it does not know; a field's name and meaning do
// not change. Code, category and stage names are open sets: a value this
// package has no constant for is not an error.
package envelope
