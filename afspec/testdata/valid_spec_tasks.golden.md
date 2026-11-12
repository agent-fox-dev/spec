## Test Commands

- **all_tests:** `go test ./... -count=1`
- **linter:** `go vet ./...`
- **spec_tests:** `go test ./afspec/... -count=1`

## Dependencies

None: this spec depends on no other spec.

## Tasks

### 1. Load and save spec artifacts (implement)

**Criteria:** REQ-1

**Steps:**

- 1.1 Write TS-01-1, TS-01-2 and TS-01-3 and confirm they fail
- 1.2 Decode prd.md and the three JSON artifacts into the Spec struct
- 1.3 Return a LoadError naming the file whenever one cannot be read or decoded
- 1.4 Write the artifacts back through the deterministic marshaller

**Touches:**

- `afspec/afspec.go`
- `afspec/prd.go`
- `afspec/marshal.go`

**Done when:**

- the checks of the criteria listed above exist as tests, are executable and pass
- `go test ./... -count=1` passes
- `go vet ./...` passes

### 2. Cross-file validation (implement)

**Criteria:** REQ-2.1

**Depends on:** 1

**Steps:**

- 2.1 Write TS-01-4 and confirm it fails
- 2.2 Index criteria, paths and tests once per validation run
- 2.3 Report an error for every criterion no test verifies and every test no task owns

**Touches:**

- `afspec/validate.go`

**Done when:**

- the checks of the criteria listed above exist as tests, are executable and pass
- `go test ./... -count=1` passes
- `go vet ./...` passes

### 3. Verify every execution path end to end (integration)

**Depends on:** 1, 2

**Steps:**

- 3.1 Run the smoke check of PATH-1 (A consumer loads and validates a spec) with afspec library, spec directory on disk unmocked and confirm its end state: LoadSpec returns no error; Validate reports the spec as valid
- 3.2 Trace PATH-1 through the real code; confirm each step calls the next and no stub remains
- 3.3 For every criterion with a contract (REQ-1.1, REQ-1.2, REQ-1.3, REQ-2.1), confirm the producer's result is consumed by a caller in production code, not only by tests
- 3.4 Search every path in touches for stub markers (the project's own, then TODO, NotImplementedError, todo!(), panic("not implemented"), a bare return of nothing on a non-trivial path) and remove any that remain

**Touches:**

- `afspec/afspec.go`
- `afspec/prd.go`
- `afspec/marshal.go`
- `afspec/validate.go`

**Done when:**

- every smoke check above runs with its real components and reaches its end state
- `go test ./... -count=1` passes
- `go vet ./...` passes

