# Erratum: spec 12, speca: change a package's lifecycle status

Where the implementation of `.specs/12_speca_lifecycle` differs from the spec,
and why. The decisions are numbered as in the implementation's survey; each is
added by the task that made it.

## D-1: `Change` and its tests are in `admin_result.go`

**Spec:** task 2 lists `envelope/envelope.go` and `envelope/envelope_test.go`
among its files, and says `Change` is "where spec 11 declares it".

**Code:** spec 11 declared `Change` in `envelope/admin_result.go`, beside
`AdminResult`, and tested it in `envelope/admin_result_test.go`
(`TestChangesAndDryRunRoundTrip_TS11_51`). `envelope.go` holds the envelope and
its decoders, which this spec does not touch. The new fields and the tests of
them are in the files that already hold `Change`.

**Test of the delivered behaviour:**
`TestChangeWritesSpecAndAddedOnlyForSupersedes_TS12_55`
(`envelope/admin_result_test.go`).

## D-2: task 2 regenerates the schema golden

**Spec:** task 2 touches `envelope` and `internal/cli` only; task 3 regenerates
`cmd/speca/testdata/schema.golden.json`.

**Code:** `speca --schema` is generated from the flag set and from
`envelope.AdminResult`, so the two new fields of `Change` and the widened
descriptions of `Change`, `changes` and `dry_run` change the document the moment
they land, and `TestSchemaGolden_TS10_20` is red from that commit on. A task does
not land on a red gate, so task 2 regenerates the golden (`UPDATE_GOLDEN=1`, 22
lines: the `spec` and `added` properties and the descriptions). Task 3 adds only
what the flags change.

**Test of the delivered behaviour:** `TestSchemaGolden_TS10_20`
(`cmd/speca/schema_test.go`).

## D-12: `--status ""` and `--by ""` are refused, not ignored

**Spec:** 12-REQ-2.3 refuses "a value other than draft that is not one of the
four targets" and 12-REQ-2.4 a `--by` without `--status superseded`. It does not
say what an empty value is.

**Code:** `--state` of the task form is taken as absent when it is empty
(`options.taskForm`), so `speca <spec> --state ""` is the report. For a form
that moves a package, a caller that templates `--status "$S"` with an empty `S`
would be told the package's state, with exit 0, where it asked for a move.
`--status` and `--by` are therefore flags that remember they were given
(`givenFlag`, `cmd/speca/status.go`): an empty `--status` is a status that is
none of the four, and an empty `--by` is empty, both usage errors. `--state ""`
of spec 11 is left as it is.

**Test of the delivered behaviour:** `TestAnUnknownStatusIsAUsageError_TS12_11`
and `TestMalformedStatusCallsAreUsageErrors_TS12_12` (`cmd/speca/main_test.go`).

## D-15: a status form with no input is the bare invocation

**Spec:** 12-REQ-2.4 has `--status` with no positional input refused as a usage
error with no result.

**Code:** a call with no input takes the path every form without an input takes
(spec 11's D-12): the help goes to stderr, and a usage envelope (exit 2, stage
`usage`, category `usage`, message `no input: …`, no result) to stdout when
stdout is not a terminal. The message is on stdout, not repeated on stderr after
`speca:`.

**Test of the delivered behaviour:** `TestMalformedStatusCallsAreUsageErrors_TS12_12`
(`cmd/speca/main_test.go`, the case "with no input").
