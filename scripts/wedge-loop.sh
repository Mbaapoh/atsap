#!/usr/bin/env bash
# Six consecutive runs of the telephony e2e suite on one rig with NO
# restart between them — the acceptance test for ARI-stall class bugs
# (docs/ASTERISK-INTEGRATION.md §2.1.1).
#
# Background: the first run always passes, on every engine version. Any
# loop shorter than two runs reports success, so a single green run proves
# nothing about wedging. Six runs with a flat engine thread count is the
# bar upstream issue #2020 set: on Asterisk 22.8.2 this loop gave 1 pass
# then 5 failures with threads climbing +4 per run; on 22.11.0 it gives
# 6 passes with threads flat — provided the client does not request
# subscribeAll, which deadlocks ARI teardown (upstream #2170, §2.1.2).
#
# Usage: ./scripts/wedge-loop.sh [runs]   (default 6)
# Exit status is 0 only if every run passed. Per-run logs go to /tmp.
# Do NOT run `mise run rig:restore` (or anything restarting Asterisk)
# between the runs — the absence of restarts is the test.
set -u

RUNS="${1:-6}"
LOGDIR="${TMPDIR:-/tmp}"

engine_threads() {
	# Thread count of the Asterisk process, counted host-side so it
	# works regardless of what userspace the engine image carries.
	local pid
	pid="$(docker inspect --format '{{.State.Pid}}' deploy-asterisk-1 2>/dev/null)" || { echo "?"; return 0; }
	[ -n "$pid" ] && [ "$pid" != "0" ] || { echo "?"; return 0; }
	ps -T -p "$pid" 2>/dev/null | tail -n +2 | wc -l
}

cd "$(dirname "$0")/../api" || exit 1

fail=0
i=1
while [ "$i" -le "$RUNS" ]; do
	threads="$(engine_threads)"
	if go test -tags e2e ./internal/telephony/e2e/ -count=1 -timeout 15m >"$LOGDIR/wedge-run-$i.log" 2>&1; then
		res="PASS"
	else
		res="FAIL"
		fail=1
	fi
	printf 'run %d: threads=%s result=%s log=%s\n' "$i" "$threads" "$res" "$LOGDIR/wedge-run-$i.log"
	i=$((i + 1))
done

[ "$fail" -eq 0 ]
