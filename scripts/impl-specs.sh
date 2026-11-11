#!/usr/bin/env bash
# Implements every unfinished spec package in number order, one fresh headless
# Claude session per package running /af-impl. Stops at the first package that
# does not land, so the next one never builds on a broken one.
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

next_spec() {
  speca --list | jq -r '.result.packages[]
    | select(.task_counts.done + .task_counts.dropped < .task_counts.total)
    | .spec_dir' | head -n1
}

n=0
while spec="$(next_spec)"; [ -n "$spec" ] && [ "$n" -lt "$MAX" ]; do
  n=$((n + 1))
  git switch -q main
  git pull -q --ff-only origin main
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
     [ "$(next_spec)" = "$spec" ]; then
    echo "Stopped at $spec; see $log" >&2
    exit 1
  fi
done

echo "Landed $n package(s)."
