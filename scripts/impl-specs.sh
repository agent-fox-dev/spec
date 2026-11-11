#!/usr/bin/env bash
# Implements every unfinished spec package in dependency order, one fresh
# headless Claude session per package running /af-impl. `speca --next` chooses
# each package: the first open one whose dependencies are all finished. Stops at
# the first package that does not land, so the next one never builds on a broken
# one, and when the open packages wait on something no run can finish (a missing
# dependency, a package that does not load) or depend on one another in a cycle.
#
# Usage: scripts/impl-specs.sh            # run until every package is done
#        MAX=1 scripts/impl-specs.sh      # land at most one package
#        LOGDIR=/some/dir scripts/impl-specs.sh
#        BG_WAIT_MIN=60 scripts/impl-specs.sh   # wait longer for background work
set -euo pipefail

for cmd in claude speca jq git; do
  command -v "$cmd" >/dev/null || { echo "missing command: $cmd" >&2; exit 2; }
done

cd "$(git rev-parse --show-toplevel)"
# Logs stay outside the repository: af-impl refuses a dirty working tree.
LOGDIR="${LOGDIR:-$HOME/.cache/parley-impl}"
MAX="${MAX:-999}"
mkdir -p "$LOGDIR"
# A headless session that ends its turn with subagents or shells still running
# waits this long for them before killing them (claude's own default is 10m,
# too short for a conformance review). 0 waits indefinitely.
BG_WAIT_MIN="${BG_WAIT_MIN:-30}"
export CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=$((BG_WAIT_MIN * 60 * 1000))

# next_spec prints the package to implement now, or nothing when every package
# is done. It exits the script when no package can start, or when speca fails
# (a dependency cycle among the packages is a failure).
next_spec() {
  local out
  if ! out="$(speca --next)"; then
    echo "speca --next failed: $(jq -r '.error.message // .summary' <<<"$out")" >&2
    exit 1
  fi
  if [ "$(jq -r '.result.all_done' <<<"$out")" = true ]; then
    return 0
  fi
  local spec
  spec="$(jq -r '.result.next_spec.spec_dir // empty' <<<"$out")"
  if [ -z "$spec" ]; then
    echo "$(jq -r '.summary' <<<"$out"):" >&2
    jq -r '.result.waiting[] | "  \(.spec_dir): \(.reason)"' <<<"$out" >&2
    exit 1
  fi
  echo "$spec"
}

# finished says whether no task of the package is pending or in progress.
finished() {
  speca "$1" | jq -e '.result.task_counts | .pending + .in_progress == 0' >/dev/null
}

n=0
while [ "$n" -lt "$MAX" ]; do
  git switch -q main
  git pull -q --ff-only origin main
  spec="$(next_spec)" || exit 1
  if [ -z "$spec" ]; then
    echo "Every package is done."
    break
  fi
  n=$((n + 1))
  before="$(git rev-parse HEAD)"
  log="$LOGDIR/$(basename "$spec").log"
  echo "=== $spec ($(date '+%Y-%m-%d %H:%M')) ==="

  # The landing checks below decide success, not claude's exit code.
  # Timers are blocked: a pending one keeps a headless session alive after its
  # work is done, and finished subagents and shells wake the session anyway.
  claude -p "/af-impl $spec" --permission-mode bypassPermissions \
    --disallowedTools "ScheduleWakeup,CronCreate" | tee "$log" || true

  # Landed: back on a clean main that moved, with the package finished.
  if [ "$(git branch --show-current)" != main ] ||
     [ -n "$(git status --porcelain)" ] ||
     [ "$(git rev-parse HEAD)" = "$before" ] ||
     ! finished "$spec"; then
    echo "Stopped at $spec; see $log" >&2
    exit 1
  fi
done

echo "Landed $n package(s)."
