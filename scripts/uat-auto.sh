#!/usr/bin/env bash
# Automated UAT with REAL baresip phones (docs/uat/walking-skeleton.md UC-04).
#
# Same endpoints and same cases as the manual pass — the only difference is
# who answers: `answermode auto` via each phone's ctrl_tcp instead of a
# human pressing 'a'. The Go e2e test still drives and asserts signaling,
# records, and events; this script additionally asserts endpoint-measured
# media (RTP packets both directions on both phones).
#
# Lifecycle: stops the sipua sidecar (fixtures allow max_contacts=1),
# runs everything, then restores the rig (kills the auto phones, restarts
# sipua) — the repo is left exactly as found, green or red.
#
# Usage: ./scripts/uat-auto.sh   (or: mise run uat:auto)

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

EXTS="1000 1001"
SIP_PORT_1000=5070
SIP_PORT_1001=5071
CTRL_PORT_1000=5550
CTRL_PORT_1001=5551
LOG_1000=/tmp/uat-auto-1000.log
LOG_1001=/tmp/uat-auto-1001.log
PID_1000=/tmp/uat-auto-1000.pid
PID_1001=/tmp/uat-auto-1001.pid

fail() { echo "UAT-AUTO FAIL: $1"; exit 1; }
log()  { echo "UAT-AUTO: $1"; }

# ctrl RPC helper: netstring-framed JSON, prints the raw reply.
ctrl_rpc() { # <port> <command> [params]
	python3 - "$1" "$2" "${3:-}" <<'EOF'
import socket, json, sys
port, cmd, params = int(sys.argv[1]), sys.argv[2], sys.argv[3]
s = socket.create_connection(('127.0.0.1', port), timeout=5)
payload = json.dumps({'command': cmd, 'params': params, 'token': 'uat-auto'})
s.sendall(f'{len(payload)}:{payload},'.encode())
s.settimeout(5)
out = b''
try:
    while True:
        chunk = s.recv(4096)
        if not chunk:
            break
        out += chunk
except socket.timeout:
    pass
print(out.decode(errors='replace'))
EOF
}

cleanup() {
	log "teardown: stopping auto phones, restarting sipua sidecar"
	[ -f "$PID_1000" ] && kill "$(cat "$PID_1000")" 2>/dev/null || true
	[ -f "$PID_1001" ] && kill "$(cat "$PID_1001")" 2>/dev/null || true
	rm -f "$PID_1000" "$PID_1001"
	docker compose -f deploy/docker-compose.yml up -d sipua >/dev/null 2>&1 || true
}
trap cleanup EXIT

# --- 0. preconditions: ports free (no manual instance holding them) ---
for p in "$SIP_PORT_1000" "$SIP_PORT_1001"; do
	if (exec 3<>/dev/tcp/127.0.0.1/"$p") 2>/dev/null; then
		fail "port $p is already bound — quit the manual baresip instance using it first"
	fi
done
command -v baresip >/dev/null || fail "baresip not installed"
command -v python3 >/dev/null || fail "python3 not installed (needed for ctrl_tcp RPC)"

# --- 1. deploy template configs, stop sidecar, start auto phones ---
log "installing auto-phone configs"
for EXT in $EXTS; do
	mkdir -p "$HOME/.baresip-$EXT-auto"
	cp "scripts/uat/baresip-$EXT-auto/accounts" "scripts/uat/baresip-$EXT-auto/config" "$HOME/.baresip-$EXT-auto/"
done
log "stopping sipua sidecar (max_contacts=1)"
docker compose -f deploy/docker-compose.yml stop sipua >/dev/null
log "starting auto phones"
: > "$LOG_1000"; : > "$LOG_1001"
nohup baresip -f "$HOME/.baresip-1000-auto" >"$LOG_1000" 2>&1 & echo $! >"$PID_1000"
nohup baresip -f "$HOME/.baresip-1001-auto" >"$LOG_1001" 2>&1 & echo $! >"$PID_1001"

# --- 2. wait for both registrations ---
log "waiting for registrations"
for i in $(seq 1 30); do
	n=$(docker compose -f deploy/docker-compose.yml exec -T asterisk asterisk -rx "pjsip show contacts" 2>/dev/null | grep -c "1000/sip\|1001/sip" || true)
	if [ "$n" = "2" ]; then break; fi
	sleep 2
done
[ "$n" = "2" ] || fail "both auto phones did not register (got $n/2 contacts)"

# --- 3. auto-answer on, then run the e2e (signaling/records/events) ---
log "enabling auto-answer on both phones"
for p in "$CTRL_PORT_1000" "$CTRL_PORT_1001"; do
	reply="$(ctrl_rpc "$p" answermode auto)"
	echo "$reply" | grep -q "changed to: auto" || fail "answermode auto rejected on ctrl port $p: $reply"
done
log "running Go e2e (this originates the call; phones answer themselves)"
(cd api && go test -tags e2e ./internal/telephony/e2e/ -count=1 > /tmp/uat-auto-e2e.log 2>&1)
e2e_status=$?
tail -8 /tmp/uat-auto-e2e.log
[ "$e2e_status" = "0" ] || fail "Go e2e test failed (exit $e2e_status) — see /tmp/uat-auto-e2e.log"

# --- 4. media evidence: RTP packets both directions, both phones ---
log "checking endpoint-measured media"
media_ok=1
for EXT in $EXTS; do
	if [ "$EXT" = "1000" ]; then LOG="$LOG_1000"; else LOG="$LOG_1001"; fi
	summary="$(grep -A3 "Transmit:.*Receive" "$LOG" | tail -4)"
	tx="$(echo "$summary" | awk '/^packets:/ {print $2}')"
	rx="$(echo "$summary" | awk '/^packets:/ {print $3}')"
	if [ -z "$tx" ] || [ -z "$rx" ] || [ "$tx" -le 0 ] || [ "$rx" -le 0 ]; then
		echo "UAT-AUTO FAIL: no media on $EXT (tx=$tx rx=$rx)"
		media_ok=0
	else
		log "phone $EXT media: tx=$tx rx=$rx packets, errors=$(echo "$summary" | awk '/^errors:/ {print $2}')"
	fi
done
[ "$media_ok" = "1" ] || fail "media did not flow on every leg (see phone logs above)"

log "PASS: automated UAT green — signaling, records, events, and bidirectional media on real phones"
