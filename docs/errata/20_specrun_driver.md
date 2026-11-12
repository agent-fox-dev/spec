# Erratum: 20_specrun_driver

## A run with nothing open starts a reconcile

**Criterion:** `20_specrun_driver` REQ-1.4: "WHEN no package under the spec
root is open, THE specrun SHALL end without starting a session".

**What the code does:** `23_specrun_reconcile` REQ-1.2 adds, when every
package is done, one reconcile step "unless a reconcile was the last step and
no package landed since". On a repository where every package is done and the
ledger records no reconcile, the run therefore starts one session, the
reconcile, before it ends with `all_done`; a second run starts none. A dry
run there, with a forge client, names the reconcile as its next step. A
repository where a reconcile was the last step behaves as REQ-1.4 says.

**Why:** the two packages contradict each other for this one case, and the
later, more specific rule is the one the loop needs: the end of a run is where
the open items opened before the label scheme are adopted. The summary and the
result are unchanged (`specrun: every package is done, landed 0`,
`all_done: true`).

**Where:** `internal/specrun/reconcile.go` (`reconcileDue`, `finalReconcile`);
the test is `internal/specrun/loop_test.go:TestLoop_EndsWhenEveryPackageIsDone`.
