#!/usr/bin/env bash
# UAT for pbx-inbound-and-emergency: calls the platform did NOT originate,
# between two REAL baresip phones, with media measured at both ends.
#
#   1. Inbound: a real baresip "carrier" dials the tenant's DID through
#      Asterisk; the trunk is recognised by the carrier's address, the DID
#      resolves to an extension, and the real phone rings and answers.
#   2. Desk-phone: the same real phone dials an external number; the dial
#      plan routes it over the trunk and the carrier answers.
#
# Each call is hung up by ONE party and the script asserts the platform
# released the OTHER (both phones log the call ending), then that RTP
# flowed both ways on both phones.
#
#   phone   — baresip registered as an extension created through the API,
#             with the secret the API generated (not a static fixture)
#   carrier — baresip standing in for a SIP carrier, IP-authenticated,
#             answering whatever number it is sent (catchall)
#
# PlaceCall rings both at once (LLD-03 §4.1); both auto-answer; the script
# asserts the call reaches Active, RTP flows in BOTH directions on BOTH
# phones, and HangupCall ends it. The rig is restored on exit, green or red.
#
# Ports: the phones use 5070/5071 and RTP 18000-18039 — the ports a host
# firewall admitting the Docker bridge is expected to allow (the same
# ones scripts/uat-auto.sh uses). Run the two UATs one after the other.
#
# Preconditions: the dev stack up and licensed (make dev-licence), baresip
# and python3 installed. Usage: ./scripts/uat-placement.sh  (make uat-placement)

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

API="http://127.0.0.1:8080/atsapbx.v1."
PHONE_PORT=5070
CARRIER_PORT=5071
PHONE_CTRL=5550
CARRIER_CTRL=5551
PHONE_DIR="$HOME/.baresip-uati-phone"
CARRIER_DIR="$HOME/.baresip-uati-carrier"
PHONE_LOG=/tmp/uat-inbound-phone.log
CARRIER_LOG=/tmp/uat-inbound-carrier.log
PHONE_PID=/tmp/uat-inbound-phone.pid
CARRIER_PID=/tmp/uat-inbound-carrier.pid
DID="+442071234567"
OUT_DEST="+442079999999"

# Synthetic operator (D-39: no real credential). The hash is this
# repository's Argon2id output for the password, the same fixture the pbx
# e2e suite uses (internal/pbx/e2e/projection_test.go).
OP_PASSWORD="e2e-fixture-operator-password"
OP_HASH='$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc'

TENANT="$(python3 -c 'import uuid; print(uuid.uuid4())')"
PRINCIPAL="$(python3 -c 'import uuid; print(uuid.uuid4())')"
OP_USER="uati-op-$RANDOM"
TOKEN=""
EXT_ID=""
TRUNK_ID=""

fail() { echo "UAT-INBOUND FAIL: $1"; exit 1; }
log()  { echo "UAT-INBOUND: $1"; }

psql_app() { docker exec -i deploy-postgres-1 psql -q -v ON_ERROR_STOP=1 -U atsapbx_app -d atsapbx "$@"; }

# api <procedure> <json> — POST a Connect JSON request, print the body;
# exits non-zero on a non-200 status.
api() {
	python3 - "$API$1" "$2" "$TOKEN" <<'EOF'
import sys, urllib.request, urllib.error
url, body, token = sys.argv[1], sys.argv[2].encode(), sys.argv[3]
req = urllib.request.Request(url, data=body, method="POST", headers={"Content-Type": "application/json"})
if token:
    req.add_header("Authorization", "Bearer " + token)
try:
    print(urllib.request.urlopen(req, timeout=10).read().decode())
except urllib.error.HTTPError as e:
    print(e.read().decode()); sys.exit(1)
EOF
}

field() { python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d.get(k, "") if isinstance(d, dict) else ""
print(d)' "$1"; }

ctrl_rpc() { # <port> <command> [params]
	# Returns as soon as the command's own response arrives. baresip
	# streams call EVENTS over the same socket, so reading "until quiet"
	# blocked for the whole life of a call (a dial returned only when the
	# call ended — once after 2 min 46 s, once past a 300 s timeout).
	python3 - "$1" "$2" "${3:-}" <<'EOF'
import socket, json, sys, time
port, cmd, params = int(sys.argv[1]), sys.argv[2], sys.argv[3]
s = socket.create_connection(('127.0.0.1', port), timeout=5)
payload = json.dumps({'command': cmd, 'params': params, 'token': 'uat-inbound'})
s.sendall(f'{len(payload)}:{payload},'.encode())
buf, deadline = b'', time.time() + 10
while time.time() < deadline:
    try:
        chunk = s.recv(4096)
    except socket.timeout:
        break
    if not chunk:
        break
    buf += chunk
    while b':' in buf:  # netstrings: <len>:<payload>,
        n, rest = buf.split(b':', 1)
        if not n.isdigit() or len(rest) < int(n) + 1:
            break
        msg, buf = rest[:int(n)], rest[int(n) + 1:]
        try:
            obj = json.loads(msg)
        except ValueError:
            continue
        if obj.get('response') and obj.get('token') == 'uat-inbound':
            print(msg.decode(errors='replace'))
            sys.exit(0)
print(buf.decode(errors='replace'))
EOF
}

cleanup() {
	log "teardown: stopping phones, removing the UAT tenant's configuration"
	for f in "$PHONE_PID" "$CARRIER_PID"; do
		[ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null || true
		rm -f "$f"
	done
	if [ -n "$TOKEN" ]; then
		# Through the API, so the engine projection is removed with the rows.
		[ -n "$TRUNK_ID" ] && api PbxService/DeleteTrunk "{\"tenantId\":\"$TENANT\",\"trunkId\":\"$TRUNK_ID\"}" >/dev/null 2>&1
		[ -n "$EXT_ID" ] && api PbxService/DeleteExtension "{\"tenantId\":\"$TENANT\",\"extensionId\":\"$EXT_ID\"}" >/dev/null 2>&1
	fi
	psql_app >/dev/null 2>&1 <<SQL || true
BEGIN;
SELECT set_config('app.tenant_id', '$TENANT', true);
DELETE FROM placed_call_legs WHERE tenant_id = '$TENANT';
DELETE FROM inbound_numbers WHERE tenant_id = '$TENANT';
DELETE FROM audit_logs WHERE tenant_id = '$TENANT';
DELETE FROM role_bindings WHERE tenant_id = '$TENANT';
DELETE FROM principals WHERE tenant_id = '$TENANT';
COMMIT;
DELETE FROM tenants WHERE id = '$TENANT';
SQL
}
trap cleanup EXIT
# A killed run (Ctrl-C, timeout) must still tear down: exiting fires the
# EXIT trap once. A run killed before teardown once left a live trunk on
# the shared carrier address, which captured the next run's calls.
trap 'exit 1' INT TERM

# --- 0. preconditions ---------------------------------------------------
command -v baresip >/dev/null || fail "baresip not installed"
command -v python3 >/dev/null || fail "python3 not installed"
for p in "$PHONE_PORT" "$CARRIER_PORT"; do
	if ss -lun | grep -q ":$p "; then
		fail "UDP port $p is in use — stop the other baresip instance (uat-auto / uat-placement?) first"
	fi
done
docker logs deploy-app-1 2>&1 | grep 'entitlement in force' | tail -1 | grep -q '"state":"Valid"' ||
	fail "the dev app is not licensed — run 'make dev-licence'"
HOST_ADDR="$(docker inspect deploy-asterisk-1 --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}')"
[ -n "$HOST_ADDR" ] || fail "cannot resolve the address Asterisk reaches this host by"
# The carrier sends straight to the engine's address on the Docker bridge:
# baresip will not originate to loopback without an outbound flow ("no
# laddr for 127.0.0.1"), and over the bridge its source is HOST_ADDR —
# exactly what the trunk's identify row matches, as a real carrier's IP.
ENGINE_ADDR="$(docker inspect deploy-asterisk-1 --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
[ -n "$ENGINE_ADDR" ] || fail "cannot resolve the engine's bridge address"

# --- 1. a tenant operator, authenticated through the real RPC -----------
log "seeding a synthetic tenant and operator"
psql_app >/dev/null <<SQL || fail "seeding the UAT tenant failed"
INSERT INTO tenants (id, name, status, residency_zone) VALUES ('$TENANT', 'uat-inbound', 'ACTIVE', 'EU');
BEGIN;
SELECT set_config('app.tenant_id', '$TENANT', true);
INSERT INTO principals (id, tenant_id, username, email, password_hash, role, status)
  VALUES ('$PRINCIPAL', '$TENANT', '$OP_USER', '$OP_USER@uat.invalid', '$OP_HASH', 'TENANT_ADMIN', 'ACTIVE');
INSERT INTO role_bindings (principal_id, tenant_id, role, scope) VALUES ('$PRINCIPAL', '$TENANT', 'TENANT_ADMIN', 'tenant');
COMMIT;
SQL
TOKEN="$(api IdentityService/AuthenticateUser "{\"tenantId\":\"$TENANT\",\"username\":\"$OP_USER\",\"password\":\"$OP_PASSWORD\"}" | field token)"
[ -n "$TOKEN" ] || fail "AuthenticateUser returned no token"

# --- 2. extension, trunk, route and DID — all through the API -----------
NUMBER="87$(printf '%04d' $((RANDOM % 10000)))"
EXT_JSON="$(api PbxService/CreateExtension "{\"tenantId\":\"$TENANT\",\"number\":\"$NUMBER\",\"displayName\":\"UAT phone\",\"deviceType\":\"DEVICE_TYPE_SIP\"}")" ||
	fail "CreateExtension: $EXT_JSON"
EXT_ID="$(echo "$EXT_JSON" | field extension.extensionId)"
AUTH_USER="$(echo "$EXT_JSON" | field extension.authUsername)"
SECRET="$(echo "$EXT_JSON" | field secret)"
TRUNK_JSON="$(api PbxService/CreateTrunk "{\"tenantId\":\"$TENANT\",\"name\":\"UAT carrier\",\"host\":\"$HOST_ADDR\",\"port\":$CARRIER_PORT,\"channelLimit\":2}")" ||
	fail "CreateTrunk: $TRUNK_JSON"
TRUNK_ID="$(echo "$TRUNK_JSON" | field trunk.trunkId)"
api PbxService/CreateRoute "{\"tenantId\":\"$TENANT\",\"prefixPattern\":\"+44\",\"trunkId\":\"$TRUNK_ID\"}" >/dev/null || fail "CreateRoute failed"
api PbxService/CreateInboundNumber "{\"tenantId\":\"$TENANT\",\"number\":\"$DID\",\"extensionId\":\"$EXT_ID\"}" >/dev/null ||
	fail "CreateInboundNumber failed"
log "extension $NUMBER, an IP-authenticated trunk to $HOST_ADDR:$CARRIER_PORT, route +44, DID $DID → $NUMBER"

# --- 3. the two real phones ------------------------------------------------
write_config() { # <dir> <sip port> <ctrl port> <rtp range>
	mkdir -p "$1"
	sed -e "s/^sip_listen.*/sip_listen\t\t0.0.0.0:$2/" \
	    -e "s/^ctrl_tcp_listen.*/ctrl_tcp_listen\t\t127.0.0.1:$3/" \
	    -e "s/^rtp_ports.*/rtp_ports\t\t$4/" \
	    scripts/uat/baresip-1000-auto/config > "$1/config"
}
write_config "$PHONE_DIR" "$PHONE_PORT" "$PHONE_CTRL" "18000-18019"
write_config "$CARRIER_DIR" "$CARRIER_PORT" "$CARRIER_CTRL" "18020-18039"
# The phone registers straight to the engine's bridge address, as a real
# desk phone reaches its PBX: an outbound proxy on loopback would force
# its own calls through 127.0.0.1, which baresip cannot originate to.
echo "<sip:$AUTH_USER@$ENGINE_ADDR:5060;transport=udp>;auth_user=$AUTH_USER;auth_pass=$SECRET;regint=300" > "$PHONE_DIR/accounts"
# The carrier never registers and authenticates with nothing: it is
# recognised by its address alone (identify-by-IP). It sends through
# Asterisk, and answers whatever number it is sent (catchall).
echo "<sip:carrier@$ENGINE_ADDR:5060;transport=udp>;regint=0;catchall=yes" > "$CARRIER_DIR/accounts"

log "starting the phone and the carrier"
: > "$PHONE_LOG"; : > "$CARRIER_LOG"
nohup baresip -f "$PHONE_DIR" >"$PHONE_LOG" 2>&1 & echo $! >"$PHONE_PID"
nohup baresip -f "$CARRIER_DIR" >"$CARRIER_LOG" 2>&1 & echo $! >"$CARRIER_PID"

for _ in $(seq 1 30); do
	reg="$(api PbxService/GetExtension "{\"tenantId\":\"$TENANT\",\"extensionId\":\"$EXT_ID\"}" | field extension.registration)"
	[ "$reg" = "REGISTRATION_STATUS_REGISTERED" ] && break
	sleep 1
done
[ "$reg" = "REGISTRATION_STATUS_REGISTERED" ] || fail "the phone did not register (last: $reg)"
for p in "$PHONE_CTRL" "$CARRIER_CTRL"; do
	reply="$(ctrl_rpc "$p" answermode auto)"
	echo "$reply" | grep -q "changed to: auto" || fail "answermode auto rejected on ctrl port $p: $reply"
done
log "phone registered; both phones auto-answer"

# latest_call_state — the tenant's most recent call, read under the tenant.
latest_call_state() {
	psql_app -tA <<SQL 2>/dev/null | tail -1
BEGIN;
SELECT set_config('app.tenant_id', '$TENANT', true) \g /dev/null
SELECT state FROM calls ORDER BY started_at DESC LIMIT 1;
COMMIT;
SQL
}

# one_call <label> <dialler ctrl port> <uri> <other log> <dialler log>
# Dials, waits for Active, lets media flow, hangs up from the DIALLER and
# requires the OTHER phone to be released by the platform.
one_call() {
	label="$1"; ctrl="$2"; uri="$3"; other_log="$4"; dialler_log="$5"
	ended_before_other="$(grep -c 'terminated (duration' "$other_log")"
	ended_before_dialler="$(grep -c 'terminated (duration' "$dialler_log")"
	ctrl_rpc "$ctrl" dial "$uri" >/dev/null
	state=""
	for _ in $(seq 1 30); do
		state="$(latest_call_state)"
		[ "$state" = "Active" ] && break
		sleep 0.5
	done
	[ "$state" = "Active" ] || fail "$label: the call never became Active (last state: $state)"
	log "$label: call Active — letting media flow for 6 s"
	sleep 6
	ctrl_rpc "$ctrl" hangup >/dev/null
	released=0
	for _ in $(seq 1 20); do
		if [ "$(grep -c 'terminated (duration' "$other_log")" -gt "$ended_before_other" ] &&
		   [ "$(grep -c 'terminated (duration' "$dialler_log")" -gt "$ended_before_dialler" ]; then
			released=1; break
		fi
		sleep 0.5
	done
	[ "$released" = "1" ] || fail "$label: hanging up one end did not release the other — it was left on a dead call"
	log "$label: one party hung up and the platform released the other"
	sleep 1
}

# media_check <label> — RTP both ways on both phones, from the LAST call.
media_check() {
	ok=1
	for who in phone carrier; do
		if [ "$who" = phone ]; then LOG="$PHONE_LOG"; else LOG="$CARRIER_LOG"; fi
		summary="$(grep -A3 "Transmit:.*Receive" "$LOG" | tail -4)"
		tx="$(echo "$summary" | awk '/^packets:/ {print $2}')"
		rx="$(echo "$summary" | awk '/^packets:/ {print $3}')"
		if [ -z "$tx" ] || [ -z "$rx" ] || [ "$tx" -le 0 ] || [ "$rx" -le 0 ]; then
			echo "UAT-INBOUND FAIL: $1: no media on the $who (tx=$tx rx=$rx)"; ok=0
		else
			log "$1: $who media tx=$tx rx=$rx packets, errors=$(echo "$summary" | awk '/^errors:/ {print $2}')"
		fi
	done
	[ "$ok" = "1" ] || fail "$1: media did not flow on every leg (logs: $PHONE_LOG, $CARRIER_LOG)"
}

# --- 4. inbound: carrier → DID → phone; the carrier hangs up -------------
one_call "inbound" "$CARRIER_CTRL" "sip:$DID@$ENGINE_ADDR:5060" "$PHONE_LOG" "$CARRIER_LOG"
media_check "inbound"

# --- 5. desk phone → trunk → carrier; the phone hangs up ----------------
one_call "desk-phone" "$PHONE_CTRL" "sip:$OUT_DEST@$ENGINE_ADDR:5060" "$CARRIER_LOG" "$PHONE_LOG"
media_check "desk-phone"

log "PASS: a carrier's call to a DID and a desk phone's call out both connected real phones with media both ways, and one hang-up released both"
