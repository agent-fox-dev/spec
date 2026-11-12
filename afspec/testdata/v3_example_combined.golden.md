# PRD

- **Spec:** `09_agent_mode` (id `09`, name `agent_mode`)
- **Title:** Agent mode for the spec CLI
- **Status:** draft
- **Schema version:** 3
- **Created:** 2026-09-08T09:00:00Z
- **Source:** docs/prds/gospec.md
- **Intent hash:** none (the spec has not been activated)

# Agent mode for the spec CLI

## Intent

Coding agents drive the `spec` CLI programmatically. When `AF_AGENT=1` is
set, every command must produce machine-readable JSON on stdout and nothing
else, so that an agent can parse results without filtering banner or
spinner text.

## Goals

- All structured output goes to stdout as one JSON object.
- Errors are reported as `{"ok": false, "error": "..."}` with exit code 1.
- Progress output never reaches stdout.

## Non-goals

- Streaming progress events.
- A machine-readable schema for every command's payload.

## Glossary

- **agent mode**: The CLI behaviour selected by the environment variable AF_AGENT=1: JSON-only stdout, no banner, no spinner.

# Requirements

## Requirements

### REQ-1: JSON-only stdout in agent mode

**Rationale:** Agents parse stdout; a single stray byte breaks them.

#### Criteria

1. [REQ-1.1] WHILE AF_AGENT=1 is set in the environment, THE spec binary SHALL suppress the banner and spinner and write only the command's JSON result to stdout
   → stdout parses as exactly one JSON object
   Check: unit
   - setup: AF_AGENT=1 is set
   - setup: a spec root with two specs exists
   ```
   r = run(env={AF_AGENT:'1'}, 'spec list'); assert parse_json(r.stdout).ok == true; assert not contains(r.stdout, BANNER_TEXT)
   ```
1. [REQ-1.2] WHEN a command finishes and AF_AGENT=1 is set and --quiet was also passed, THE spec binary SHALL behave exactly as with AF_AGENT=1 alone
   → identical stdout, stderr and exit code
   Check: unit
   - setup: AF_AGENT=1 is set
   - setup: a spec root with two specs exists
   ```
   a = run(env, 'spec list'); b = run(env, 'spec --quiet list'); assert a == b
   ```
1. [REQ-1.3] THE spec binary SHALL for any command and any arguments, in agent mode, write progress and diagnostics to stderr only
   → stdout parses as JSON; every diagnostic line appears on stderr only
   Check: property
   - setup: AF_AGENT=1 is set
   - setup: a temporary spec root
   ```
   for cmd in COMMANDS: r = run(env, cmd); assert is_json(r.stdout)
   ```

### REQ-2: Error envelope in agent mode

#### Criteria

1. [REQ-2.1] IF a command returns an error while AF_AGENT=1 is set, THEN THE spec binary SHALL write {"ok": false, "error": <message>} to stdout and exit
   → exit code 1; stdout is {"ok": false, "error": string}
   Check: unit
   - setup: AF_AGENT=1 is set
   - setup: the spec 99_missing does not exist
   ```
   r = run(env, 'spec status 99_missing'); assert r.code == 1; j = parse_json(r.stdout); assert j.ok == false and len(j.error) > 0
   ```
1. [REQ-2.2] IF stdout is a closed pipe while the JSON result is written, THEN THE spec binary SHALL discard the write error and keep the command's own exit code
   → no panic; exit code is the command's, not a write error
   Check: unit
   - setup: AF_AGENT=1 is set
   - setup: stdout is connected to a reader that closes before the payload is written
   ```
   r = run_with_closed_stdout(env, 'spec list'); assert r.code == 0; assert not contains(r.stderr, 'panic')
   ```

## Execution Paths

### PATH-1: Agent lists specs and parses the result

1. **agent** runs `spec list` with AF_AGENT=1
2. **spec binary** detects agent mode before the banner would print
3. **spec binary** scans the spec root and builds the list payload
4. **spec binary** writes the payload as one JSON object to stdout and exits 0

**Smoke check — real components (must not be mocked):** spec binary, spec root on disk

**Setup:**

- the spec binary is built
- a spec root with one spec exists

**End state:**

- exit code is 0
- stdout is one JSON object whose specs array has one entry


# Tasks

## Test Commands

- **all_tests:** `go test ./... -count=1`
- **linter:** `go vet ./...`
- **spec_tests:** `go test ./cmd/spec/... -run 'TestAgentMode' -count=1`

## Dependencies

- `08` — extends the cobra root command and emit helpers of the Go CLI

## Tasks

### 1. Detect agent mode and keep stdout JSON-only (implement)

**Criteria:** REQ-1

**Steps:**

- 1.1 Write TS-09-1, TS-09-2 and TS-09-3 in cmd/spec/agent_test.go and confirm they fail
- 1.2 Add isAgentMode() reading AF_AGENT and call it in the root PersistentPreRun before the banner
- 1.3 Force quiet when in agent mode so the spinner and banner write nothing
- 1.4 Make emit() the only writer to stdout; route every other message to stderr

**Touches:**

- `cmd/spec/root.go`
- `cmd/spec/banner.go`
- `cmd/spec/spinner.go`

**Done when:**

- the checks of the criteria listed above exist as tests, are executable and pass
- `go test ./... -count=1` passes
- `go vet ./...` passes

### 2. Error envelope and closed-pipe handling (implement)

**Criteria:** REQ-2.1, REQ-2.2

**Depends on:** 1

**Steps:**

- 2.1 Write TS-09-4 and TS-09-5 and confirm they fail
- 2.2 In Execute(), when agent mode is on and RunE returns an error, print {ok:false,error} via emit() and exit 1
- 2.3 In emit(), treat EPIPE and io.ErrClosedPipe as success so the command's exit code is preserved

**Touches:**

- `cmd/spec/root.go`
- `cmd/spec/emit.go`

**Done when:**

- the checks of the criteria listed above exist as tests, are executable and pass
- `go test ./... -count=1` passes
- `go vet ./...` passes

### 3. Verify every execution path end to end (integration)

**Depends on:** 1, 2

**Steps:**

- 3.1 Run the smoke check of PATH-1 (Agent lists specs and parses the result) with spec binary, spec root on disk unmocked and confirm its end state: exit code is 0; stdout is one JSON object whose specs array has one entry
- 3.2 Trace PATH-1 through the real code; confirm each step calls the next and no stub remains
- 3.3 For every criterion with a contract (REQ-1.1, REQ-1.2, REQ-1.3, REQ-2.1, REQ-2.2), confirm the producer's result is consumed by a caller in production code, not only by tests
- 3.4 Search every path in touches for stub markers (the project's own, then TODO, NotImplementedError, todo!(), panic("not implemented"), a bare return of nothing on a non-trivial path) and remove any that remain
- 3.5 For every path whose entry point belongs to a spec this one depends on (08), confirm the entry point is called from production code

**Touches:**

- `cmd/spec/root.go`
- `cmd/spec/banner.go`
- `cmd/spec/spinner.go`
- `cmd/spec/emit.go`

**Done when:**

- every smoke check above runs with its real components and reaches its end state
- `go test ./... -count=1` passes
- `go vet ./...` passes


