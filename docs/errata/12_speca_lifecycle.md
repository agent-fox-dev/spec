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

## D-8: a version 1 package is refused at stage load here, as a usage error in the task form

**Spec:** 12-REQ-3.2 refuses a package whose `schema_version` is neither absent
nor 2 at stage `load`, category `input` (exit 1). Spec 11 (11-REQ-2.4) refuses
the same package in the task form as a usage error (exit 2, stage `usage`).

**Code:** each form follows its own spec. The status form refuses it in
`checkLoaded` (`cmd/speca/status.go`), for the package and for the replacing
package, whose message begins `the --by package`, and the task form in
`executeTask` (`cmd/speca/task.go`). Both say that `speca` changes the state of
(writes) format version 2 packages only and that a write would discard the
version 1 plan; `--force` reaches neither.

**Test of the delivered behaviour:**
`TestLoadFailuresAndOtherFormatVersionsAreRefusedAtLoad_TS12_16`
(`cmd/speca/status_test.go`).

## D-3: the keys of the `prd.md` frontmatter are looked for by `speca`

**Spec:** the Background says `Validate` reports each key the schema does not
allow as an `unknown_field` error, and 12-REQ-3.6 and 3.7 have sealing, archiving
and superseding refuse, or with `--force` warn about, a package for which it does.
TS-12-20 and TS-12-21 put the unknown key in the frontmatter of `prd.md`.

**Code:** the library records the unknown keys of `requirements.json`,
`test_spec.json` and `tasks.json` only (`afspec/validate.go`, `ValidateSchema`).
The frontmatter is rendered again from the fields the library knows, by the same
write, so a key of one's own in it is dropped and `Validate` never said so.
`frontmatterUnknownKeys` (`cmd/speca/task.go`, from spec 11's D-20) parses the
frontmatter as the loader does and asks which keys the schema's type lacks; they
join the keys of the other artifacts as `prd.md /key`, in the refusal and in the
warning.

The same keys decide whether the replacing package can be saved to record the id
it supersedes (12-REQ-7.3): one that holds any is not saved, and the warning names
them.

Activation, which the spec covers with its validation check, treats such a key as
a failing rule `unknown_field` of the same check: refused at stage `validate`,
category `invalid_spec`, with `--force` the way past and the key named in the
high `spec_invalid` warning. Without that an activation would drop it silently.

**Test of the delivered behaviour:**
`TestUnknownKeysRefuseSealingAndArchiving_TS12_20`,
`TestForceSealsPastUnknownKeys_TS12_21` and
`TestActivationRefusesAFrontmatterKeyTheLibraryDoesNotReport`
(`cmd/speca/status_test.go`).

## D-5: the Intent message names the heading; it does not contain it on a line

**Spec:** 12-REQ-4.2 has the message tell the caller to add a `## Intent`
section to `prd.md`, "spelled exactly so on a line of its own". The pseudocode of
TS-12-24 asserts `contains(msg,'\n## Intent\n')`.

**Code:** "spelled exactly so, on a line of its own" is the rule the library holds a
heading to: `HasIntent` (`afspec/intent.go`) matches `## Intent` alone on its
line, and `afspec/README.md` says so in those words. The message says the same
of the heading to add ("the heading `## Intent`, spelled exactly so and on a line
of its own"), and does not carry a newline-delimited heading itself.

**Test of the delivered behaviour:**
`TestActivationWithoutAnIntentIsRefused_TS12_24` (`cmd/speca/status_test.go`).

## D-13: the no-write scan allows `Transition`, `Supersede` and `Save` in the status form

**Spec:** 10-REQ-3.7 (spec 10) has `speca` write no file in any form; spec 11
allowed `Save` in `task.go`.

**Code:** `TestSpecaHasNoCodeThatWritesOrReachesOut_TS10_22` forbids `Save`,
`Transition`, `Supersede`, `MoveToArchive` and every write by `os` in
`cmd/speca`, and allows `Save` in `task.go`. The status form writes through
`Transition` and `Supersede` (`cmd/speca/status.go`) and, for the replacing
package, `Save`; the scan now allows each in the file that makes the call and
nowhere else. `MoveToArchive`, the writes of `os`, the environment, `net` and
`os/exec` stay forbidden everywhere: the directory move is the library's
`MoveUnderArchive`. The behavioural half of the test, which runs the read-only
forms and compares the file tree, is unchanged.

**Test of the delivered behaviour:**
`TestSpecaHasNoCodeThatWritesOrReachesOut_TS10_22` and
`TestSpecaWritesNothingAndReadsNoCredential_TS10_22` (`cmd/speca/main_test.go`).

## D-4: TS-12-15's first case is run with `--status active`

**Spec:** TS-12-15 gives "a draft package without an Intent section, with unknown
keys, targeted at archived under a taken destination" and says it "is refused by
the move/target check, not by the unknown-key or destination checks".

**Code:** a draft may be archived (`draft → archived`), and archiving has no check
of its own (12-REQ-6), so no check before the keys can refuse that call; the
keys, then the destination, would. The case exercises the order it describes in
three calls instead, all refused with no result and nothing written, also under
`--dry-run`:

- a draft with no intent and a key of its own, `--status active`: refused for the
  intent, which is the target's check, and the message does not name the key;
- an active package with a key of its own and a taken destination,
  `--status archived`: refused by the move (seal it first), before either;
- a sealed package with the same, `--status archived`: refused for the key
  (stage `status`), and with `--force`, for the destination (stage `archive`).

**Test of the delivered behaviour:**
`TestTheChecksRunInOrderAndTheFirstRefusalEndsTheCall_TS12_15`
(`cmd/speca/status_test.go`).
