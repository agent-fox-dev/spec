# Erratum: spec 13, spec, specr and speca point at one another

Where the implementation of `.specs/13_state_command_cross_links` differs from
the spec, and why. The decisions are numbered as in the implementation's survey;
each is added by the task that made it.

## D-6: the tests that run the three commands are one test for each

**Spec:** TS-13-3 and TS-13-4 have the test run "the spec, specr and speca entry
points in process"; TS-13-2 and TS-13-7 read all three commands' sources.

**Code:** `spec`, `specr` and `speca` are each a `package main`, which no test can
import, so no one test can call the `Main` of all three. TS-13-3 and TS-13-4 are
one test for each command, in its own package (`cmd/spec/main_test.go`,
`cmd/specr/main_test.go`, `cmd/speca/main_test.go`), with the same assertions. TS-13-2
and TS-13-7, which read files, are in `internal/cli/siblings_test.go`, which every
command imports. TS-13-10 is a test in each of the two packages that have a golden
file.

**Test of the delivered behaviour:**
`TestHelpCarriesTheSiblingLine_TS13_3` and
`TestBareInvocationCarriesTheSiblingLine_TS13_4` (the three `main_test.go`),
`TestTheSiblingSentenceIsWrittenInOneFile_TS13_2` and
`TestEachCommandsDocNamesTheOtherTwo_TS13_7` (`internal/cli/siblings_test.go`).
