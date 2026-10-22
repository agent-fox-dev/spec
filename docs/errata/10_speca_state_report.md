# Erratum: spec 10, speca: report a spec package's state from the shell

Where the implementation of `.specs/10_speca_state_report` differs from the
spec, and why. The decisions are numbered as in the implementation's survey;
the later ones are added by the task that made them.

## D-1: `NextTask` copies the task deeply, which `GetTask` does not

**Spec:** task 1, step 1.3, has `NextTask` return a deep copy "as `GetTask`
does".

**Code:** `GetTask` copies the `Task` struct and nothing under it, so the
slices and the `optional` pointer of the copy are the plan's own. 10-REQ-1.4
and TS-10-4 require that mutating the returned task, its `depends_on` among
the rest, leaves the plan unchanged. `NextTask` therefore uses `Task.clone`
(`afspec/task.go`, `clone`), which `deepCopy` now uses as well, so the list of
fields to copy exists once. `GetTask` is not changed.

**Test of the delivered behaviour:**
`TestNextTaskReturnsACopyOfTheFirstInProgressTask_TS10_4`
(`afspec/task_test.go`) mutates every slice and the pointer of the copy.

## D-2: the resolver's refusals all begin with the tool's name

**Spec:** 10-REQ-2.1 says the shared resolver gives "the same results specr's
former locate gave". 10-REQ-2.2 and TS-10-10 say each refusal's message begins
with the tool name the caller passed in.

**Code:** the former messages for a reference that matches no package or two
began with the reference, and the one for several lines with "the input".
`Locate` (`internal/specpkg/locate.go`, `Locate`) begins every refusal with the
tool: the three that already named it keep their words, and the others
become `<tool> cannot resolve the reference: ` followed by the former text, so
the list of what the spec root holds, the names of two matches and the archived
package that matches are all still in the message. Which reference resolves to
which directory, and which is refused, is unchanged.

**Test of the delivered behaviour:**
`TestLocateRefusesWithAUsageErrorNamingTheTool_TS10_10`
(`internal/specpkg/specpkg_test.go`), and `specr`'s own `TestUnknownReference`
and `TestAmbiguousReference`, which read the lists out of the message.

## D-10: task 2 regenerates `specr`'s schema golden

**Spec:** task 2 touches the files of `envelope` only, and task 4 regenerates
`cmd/specr/testdata/schema.golden.json`.

**Code:** `TaskInfo` is shared by `specr` and `speca`, so adding `ready` to it
(`envelope/render_result.go`, `TaskInfo`) changes `specr --schema`, and
`TestSchemaGolden` is red from that commit on. A task does not land on a red
gate, so the golden is regenerated in task 2 (`UPDATE_GOLDEN=1`; the diff is
the `ready` property in the two places `TaskInfo` occurs). Task 4 finds it
already current.

**Test of the delivered behaviour:** `TestSchemaGolden`
(`cmd/specr/schema_test.go`).

## D-11: `specpkg.List` orders the directories by number

**Spec:** 10-REQ-2.3 has the shared listing return the `NN_name` directories;
only 10-REQ-7.1 and task 9 order them, for `speca --list`.

**Code:** `List` (`internal/specpkg/locate.go`, `List` and `compareDirs`) returns
them in the order 10-REQ-7.1 asks for: the prefix compared as a number, a tie
broken by the whole name. The resolver and `--list` then share one order. For
the packages a repository has, whose prefixes have two digits, this is the
order the former listing gave; it differs only from 100 packages on, in the
order of the names a refusal lists.

**Test of the delivered behaviour:** `TestListAndDisplay_TS10_11`
(`internal/specpkg/specpkg_test.go`).

## D-12: TS-10-13 pins the converters, not a pipeline run

**Spec:** TS-10-13 has "specgen run the pipeline over the same package" and the
JSON of `spec`'s result equal the golden output byte for byte.

**Code:** the pipeline writes the package from a model's submissions, so it
cannot be run over a package damaged in memory in each way the report names.
The golden file (`internal/specpkg/testdata/report.golden.json`) was captured
from `specgen`'s own converters before they moved, over such a package, and
the test compares `specpkg.ReportOf` and `TraceOf` with it. `specgen` calls
the same two functions (`specgen/pipeline.go`, the `Validate` step), and its
pipeline tests, which pin the validation and traceability of a package with an
unowned test, pass unchanged.

**Test of the delivered behaviour:**
`TestConvertersGiveTheReportSpecAlwaysWrote_TS10_13`
(`internal/specpkg/specpkg_test.go`) and `TestAnInvalidPackageIsWrittenAndReported`
(`specgen/pipeline_test.go`).

## D-13: the nested report types carry no description tags

**Spec:** 10-REQ-9.1 has every field of `AdminResult` documented with a
description tag.

**Code:** every field of `AdminResult`, `TaskCounts` and `PackageInfo` is. The
fields of `ValidationReport`, `ValidationItem` and `TraceReport` are not: they
are `spec`'s types, in `envelope/spec_result.go`, which is no task's `touches`.
`speca --schema` therefore describes `validation` and `traceability` as wholes
(`envelope/admin_result.go`, `Validation` and `Traceability`), naming the
members of each, and not their members one by one.

**Test of the delivered behaviour:**
`TestAdminResultDocumentsEveryFieldAndOmitsWhatAFormDoesNotProduce_TS10_51`
(`envelope/admin_result_test.go`).

## D-14: a task in no known state is in the total and in no count

**Spec:** 10-REQ-4.7 and TS-10-30 have `task_counts.total` equal the length of
`tasks` and the sum of `pending`, `in_progress`, `done` and `dropped`, for any
loaded package.

**Code:** the task states are a string in the generated types, so a package
whose `tasks.json` names another state loads, and does not validate.
`countTasks` (`cmd/speca/report.go`, `countTasks`) sets `total` to the number of
tasks and counts the four states, so for such a package the two equalities
cannot both hold. The one that ties `total` to `tasks` is kept: a caller
iterating `tasks` finds `total` of them. The package's `valid` is false and
`failing_rules` names the rule the state breaks.

**Test of the delivered behaviour:** `TestReportCountsAddUp_TS10_30`
(`cmd/speca/report_test.go`), over plans in the four states.

## D-6: "the package has at least one edge" means an edge from or to it

**Spec:** 10-REQ-6.3 has the verdict keep the errors that name no entity (the
dependency cycle) "when the package has at least one edge in the graph"; the
reason, in the design decisions, is that a package with no edge cannot be on a
cycle.

**Code:** `concern` (`cmd/speca/verdict.go`, `concern`) reads it as an edge the
package is at either end of: `graph.Dependencies(id)` or `graph.Dependents(id)`
is not empty. A package that is only depended on, and depends on nothing, keeps
the cycle error though it may not be on the cycle; the rule cannot tell, since
the error names no package. A package with no edge at all never has it.

**Test of the delivered behaviour:**
`TestVerdictKeepsTheCrossSpecFindingsOfThePackage_TS10_36`
(`cmd/speca/verdict_test.go`): the package that is only depended on keeps the
cycle error, and the one with no edge does not.

## D-5: speca reads `$AF_SPEC_DIR`, and no other variable

**Spec:** task 9, step 9.5, has the code "read no environment variable";
10-REQ-3.7 and TS-10-22 say it reads no credential or model environment
variable.

**Code:** `afspec.ResolveSpecsDir` (`afspec/specsdir.go`, `ResolveSpecsDir`)
reads `AF_SPEC_DIR` when `--specs-dir` is not given, as it does for `specr`,
and the PRD names the variable as the spec root's second source. That is the
only variable read on the call path, and it is neither a credential nor a
model. The command's own code calls no `os.Getenv`, `os.LookupEnv` or
`os.Environ`. The requirement is followed; the step's wording was wider than it.

**Test of the delivered behaviour:**
`TestSpecaWritesNothingAndReadsNoCredential_TS10_22` (the output does not change
with credentials and models set) and
`TestSpecaHasNoCodeThatWritesOrReachesOut_TS10_22` (`cmd/speca/main_test.go`).

## D-15: an archived package that does not load has two fields, not three

**Spec:** 10-REQ-7.3 has an entry for a package that fails to load hold only
`spec_dir` and `load_error`; 10-REQ-7.4 has each package listed from `archive/`
marked `archived: true`.

**Code:** the two meet for a package under `archive/` that does not load. `entry`
(`cmd/speca/list.go`, `entry`) follows the more specific rule: such an entry
holds `spec_dir` and `load_error` and no `archived`. Its `spec_dir` still says
`archive/`, and `PackageInfo` (`envelope/admin_result.go`) documents a failed
entry as holding those two fields only.

**Test of the delivered behaviour:** `TestListAnArchivedPackageThatDoesNotLoad`
(`cmd/speca/list_test.go`).

## D-16: an `archive/` that cannot be read raises a warning

**Spec:** 10-REQ-7.5 has a missing `archive/` add nothing and raise no warning;
it says nothing of one that exists and cannot be read.

**Code:** `list` (`cmd/speca/list.go`, `list`) tells the two apart with
`errors.Is(err, fs.ErrNotExist)`. A missing `archive/` is silent. Any other
failure to read it (it is a file, or has no permission) adds nothing to the
list and raises the low `specs_dir_unreadable` warning, naming it, so that the
absence of the archived packages is not mistaken for there being none.

**Test of the delivered behaviour:** `TestListAll_TS10_42` (the `archive`
that is a file) in `cmd/speca/list_test.go`.
