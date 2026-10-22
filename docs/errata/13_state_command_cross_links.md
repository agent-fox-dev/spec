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

## D-2: the scan allows `ComputeIntentHash` in `status.go`

**Spec:** TS-13-14 has "no non-test file in cmd/speca" call `afspec.ComputeIntentHash`
or declare `intentDrift`. 12-REQ-4.5 (spec 12) has the dry run of an activation
report "the intent_hash that `ComputeIntentHash` gives".

**Code:** `speca` decides no drift by itself any more, but the dry run of an
activation (`preview`, `cmd/speca/status.go`) computes the hash the activation
would record, which is not a drift decision and cannot come from `IntentDrift`.
The scan (`TestSpecaDecidesDriftAndWritabilityThroughSpecpkg_TS13_14`,
`cmd/speca/writable_test.go`) forbids the declaration of `intentDrift`, and any
call of `ComputeIntentHash` outside `status.go`.

## D-4: the result keeps the answer to "can this package take a write"

**Spec:** 13-REQ-4.4 has `speca` decide writability "only by calling
`specpkg.Writable`".

**Code:** the report's claim suggestion was decided by `Result.writable()` from
the result's own fields (`status`, `intent_drift`), a second copy of the rule that
the test of a hand-made result relied on. `reportWith` (`cmd/speca/report.go`) now
stores `specpkg.Writable(spec)` in the result, which the claim reads; the list
stores one for each package (`Result.writableDirs`, `cmd/speca/list.go`) in place of
its map of drifted ones. Spec 10's `TestReportNext_TS10_48` and `TestListNext_TS10_49`,
which build results by hand, set these fields as the code does.

**Test of the delivered behaviour:**
`TestSpecaDecidesDriftAndWritabilityThroughSpecpkg_TS13_14` (`cmd/speca/writable_test.go`).

## D-5: more files of `speca` decided drift than the task lists

**Spec:** task 3 lists `report.go`, `forms.go`, `list.go` and `verdict.go`.

**Code:** spec 12 added three more users of the private drift check, after the spec
was written: the sealing check (`status_checks.go`), the record in the replacing
package (`status_supersede.go`) and the refusal of the task form (`task.go`,
`unwritable`). 13-REQ-4.4 leaves "no private drift helper" in `cmd/speca`, so these
call `specpkg.IntentDrift` and `Writable` too, and `result.go` loses its method.
`verdict.go` decided nothing and is unchanged.

## D-8: an empty `intent_hash` is a hash that is set

**Spec:** 13-REQ-4.1 has `IntentDrift` be true when the active package's
`IntentHash` is non-nil and the hash of its intent is another or cannot be computed.

**Code:** the private check in `cmd/speca` took an empty `intent_hash` for an unset
one. `Spec.Save` does not (`afspec/afspec.go`, the guard on `s.IntentHash != nil`):
it refuses an active package whose hash is the empty string, as it refuses any
other that is not the hash of its intent. `specpkg.IntentDrift` follows the spec
and the library, so for that one corner `speca`'s report says `intent_drift: true`
and its task form refuses the write at stage `status`, as the library would, where
it used to report no drift and fail at the write.

**Test of the delivered behaviour:**
`TestIntentDriftIsTrueOnlyForAnActivePackageWhoseHashFailsOrDiffers_TS13_11`
(`internal/specpkg/writable_test.go`) and
`TestAnActivePackageWithAnEmptyHashHasDrifted` (`cmd/speca/writable_test.go`).

## D-3: the two commands claim the same task, except where the first unfinished task is stuck

**Spec:** 13-REQ-5.7 has `specr` and `speca` hold a claim "exactly when" the other
does, for any package state, both naming the task of `specr`'s render suggestion;
13-REQ-5.4 has `specr` offer none when the first unfinished task is not ready.

**Code:** `specr` claims the first task that is not done or dropped (M), when it is
ready; `speca`'s `next_task` is the first *ready* task. With no task in progress the
two are the same task whenever M is ready, and when no task is dropped (every earlier
task is then done, so M is stuck only behind a task that cannot be done). They differ
when M is pending and not ready while a later task is ready, which needs M to depend
on something that is never done: a task that is **dropped** (a package that
validates), a later task or an id the plan does not have (packages that do not). With
task 1 dropped, task 2 depending on it and task 3 depending on none, `speca` claims 3
and `specr`, following 5.4, offers the render of 2 alone (reproduced with both built
commands). The two readings of the spec cannot both hold there; I followed 5.1 to 5.4,
whose cases are each tested, and kept the safe direction of 5.7: `specr` never offers a
claim that `speca` would not, and its claim always names its render target. An earlier
version of this entry named only the dependency on a later task; the reviewer found
the dropped one.

**Test of the delivered behaviour:** `TestSpecrAndSpecaClaimTheSameTask_TS13_22`
(equal wherever no task is dropped; where only `speca` claims, the first unfinished task
is pending and not ready) and
`TestAWholeRenderWhoseFirstTaskIsStuckClaimsNothingSpecaWouldNot` (both causes)
(`cmd/specr/next_test.go`), which run the built `speca` beside `specr`.

## D-9: a reference that names no package is a usage error, not exit 1

**Spec:** TS-13-30 has `specr` run on "a spec reference that resolves to no
package or to a package with malformed JSON" and asserts exit 1 for both, with an
error and no next. 13-REQ-7.5 has exit 1 for a package `specr` "cannot load".

**Code:** a reference that resolves to no package is refused by the resolver, before
anything is loaded, as a usage error (exit 2, stage `usage`); `specr`'s help and
`--schema` say so ("2 usage error — the reference names no package"). A package that
resolves but does not load is the failure at stage `load` (exit 1). Both have an
error and no next. The test asserts each its own code.

**Test of the delivered behaviour:** `TestAPackageThatCannotBeLoadedHasNoNext_TS13_30`
(`cmd/specr/next_test.go`).

## D-10: a package with a status the format does not define is refused by name

**Spec:** 13-REQ-4.4 has `speca`'s envelopes for the report, list and task form
unchanged by the move to `specpkg.Writable`.

**Code:** the old check refused the task form for a sealed, superseded or archived
package and for drift, and let any other status through to `Save`, which accepts it.
`Writable` is true only for a draft and an active package without drift, so a package
whose status is none of the five (`weird`, `Draft`) is refused at stage `status`, as the
glossary of the spec has it ("the only packages speca's task and status writes
accept"). It is a change of the task form's envelope for that one case, and the message
says what it found: `<dir> has the status "weird", which the format does not define:
only a draft, or an active package whose intent has not drifted, takes a change`. The
report and the list are unchanged.

**Test of the delivered behaviour:**
`TestAStatusTheFormatDoesNotDefineIsRefusedByName` (`cmd/speca/writable_test.go`).

## D-11: a package that holds keys, or is in another format version, is offered the call

**Spec:** the PRD says `specr` suggests the `speca` call "wherever that call would
succeed", and 13-REQ-5.7 has the claim agree with `speca <spec>`'s.

**Code:** "can take a write" is the status and the intent (`Writable`). A package that
holds a key the schema does not allow, or declares a format version other than 2, is
writable by that rule, so `specr` offers its claim, as `speca <spec>` does; `speca`
then refuses the call with the reason (stage `validate` for the keys, a usage error for
the version). The two commands agree, as 5.7 requires; the phrase "wherever it would
succeed" is true of the writability rule and not of those two checks, which
`speca`'s own `next` rules (not to be changed here) do not apply to a claim. The
documents say so.

**Test of the delivered behaviour:** `TestSpecrAndSpecaClaimTheSameTask_TS13_22`
(`cmd/specr/next_test.go`) for the agreement.

