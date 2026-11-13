# Erratum: spec 28, close a task on evidence

Where the implementation of `.specs/28_close_task_on_evidence` differs from
the spec, and why. The decisions are numbered as in the implementation's
survey.

## D-8: an empty evidence command is a schema error

**Spec:** REQ-3.1's check lists `evidence_tests = ''` among the commands that
give exactly one `evidence_tests_placeholder` error.

**Code:** `evidence_tests` has `minLength: 1` in `tasks.v3.json`, as
`affected_tests` has, so an empty command is a `json_schema` error. The
library runs the cross-file rules, `evidence_tests_placeholder` among them,
only on a package that passes its schemas (`afspec/validate.go`,
`ValidateCrossFile`), so the two cannot both be reported. The empty command
is refused, by the schema, as the empty `affected_tests` is.

**Test of the delivered behaviour:**
`TestValidate_ChecksThePlaceholderOfTheEvidenceCommand`
(`afspec/validate_test.go`), its case `empty`, asserts one `json_schema`
error.

## A key the schema does not allow counts as contract drift

**Spec:** REQ-1.2 says a file's contract hash changes "when any other value of
the file changes".

**Code:** the hash is taken over what the library decodes, which drops a key
the schema does not allow, so such a key cannot change the hash. Rather than
let it pass, `(*Spec).ContractDrift` (`afspec/contract.go`) names a contract
file that holds one as changed: an activated contract holds none, since
activation refuses a package that does not validate.

**Test of the delivered behaviour:** `TestReport_ContractDrift`
(`cmd/speca/report_test.go`), package `17`, reports `contract_drift: true`
for a `tasks.json` that gained an `owner` key.
