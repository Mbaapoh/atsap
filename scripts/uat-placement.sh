#!/usr/bin/env bash
# UAT for pbx-call-placement: a call placed through the PUBLIC API, over a
# carrier trunk, between two REAL baresip phones, with media measured at
# both ends.
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
PHONE_DIR="$HOME/.baresip-uatp-phone"
CARRIER_DIR="$HOME/.baresip-uatp-carrier"
PHONE_LOG=/tmp/uat-placement-phone.log
CARRIER_LOG=/tmp/uat-placement-carrier.log
PHONE_PID=/tmp/uat-placement-phone.pid
CARRIER_PID=/tmp/uat-placement-carrier.pid
DEST="+442071234567"

# Synthetic operator (D-39: no real credential). The hash is this
# repository's Argon2id output for the password, the same fixture the pbx
# e2e suite uses (internal/pbx/e2e/projection_test.go).
OP_PASSWORD="e2e-fixture-operator-password"
OP_HASH='$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc'

TENANT="$(python3 -c 'import uuid; print(uuid.uuid4())')"
PRINCIPAL="$(python3 -c 'import uuid; print(uuid.uuid4())')"
OP_USER="uatp-op-$RANDOM"
TOKEN=""
EXT_ID=""
TRUNK_ID=""

fail() { echo "UAT-PLACEMENT FAIL: $1"; exit 1; }
log()  { echo "UAT-PLACEMENT: $1"; }

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
	python3 - "$1" "$2" "${3:-}" <<'EOF'
import socket, json, sys
port, cmd, params = int(sys.argv[1]), sys.argv[2], sys.argv[3]
s = socket.create_connection(('127.0.0.1', port), timeout=5)
payload = json.dumps({'command': cmd, 'params': params, 'token': 'uat-placement'})
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
DELETE FROM audit_logs WHERE tenant_id = '$TENANT';
DELETE FROM role_bindings WHERE tenant_id = '$TENANT';
DELETE FROM principals WHERE tenant_id = '$TENANT';
COMMIT;
DELETE FROM users WHERE email = '$OP_USER@uat.invalid';
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
		fail "UDP port $p is in use — stop the other baresip instance (uat-auto?) first"
	fi
done
docker logs deploy-app-1 2>&1 | grep 'entitlement in force' | tail -1 | grep -q '"state":"Valid"' ||
	fail "the dev app is not licensed — run 'make dev-licence' (an unlicensed app refuses every call)"
HOST_ADDR="$(docker inspect deploy-asterisk-1 --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}')"
[ -n "$HOST_ADDR" ] || fail "cannot resolve the address Asterisk reaches this host by"

# --- 1. a tenant operator, authenticated through the real RPC -----------
log "seeding a synthetic tenant and operator"
psql_app >/dev/null <<SQL || fail "seeding the UAT tenant failed"
INSERT INTO tenants (id, name, status, residency_zone) VALUES ('$TENANT', 'uat-placement', 'ACTIVE', 'EU');
-- A person (D-61) with a password credential, then their membership.
WITH u AS (INSERT INTO users (email, display_name) VALUES ('$OP_USER@uat.invalid', 'UAT operator') RETURNING id)
INSERT INTO user_credentials (user_id, kind, secret) SELECT id, 'password', '$OP_HASH' FROM u;
BEGIN;
SELECT set_config('app.tenant_id', '$TENANT', true);
INSERT INTO principals (id, tenant_id, user_id, role, status)
  VALUES ('$PRINCIPAL', '$TENANT', (SELECT id FROM users WHERE email = '$OP_USER@uat.invalid'), 'TENANT_ADMIN', 'ACTIVE');
INSERT INTO role_bindings (principal_id, tenant_id, role, scope) VALUES ('$PRINCIPAL', '$TENANT', 'TENANT_ADMIN', 'tenant');
COMMIT;
SQL
TOKEN="$(api IdentityService/AuthenticateUser "{\"tenantId\":\"$TENANT\",\"username\":\"$OP_USER@uat.invalid\",\"password\":\"$OP_PASSWORD\"}" | field token)"
[ -n "$TOKEN" ] || fail "AuthenticateUser returned no token"

# --- 2. an extension, a carrier trunk and a route — all through the API --
NUMBER="87$(printf '%04d' $((RANDOM % 10000)))"
EXT_JSON="$(api PbxService/CreateExtension "{\"tenantId\":\"$TENANT\",\"number\":\"$NUMBER\",\"displayName\":\"UAT phone\",\"deviceType\":\"DEVICE_TYPE_SIP\"}")" ||
	fail "CreateExtension: $EXT_JSON"
EXT_ID="$(echo "$EXT_JSON" | field extension.extensionId)"
AUTH_USER="$(echo "$EXT_JSON" | field extension.authUsername)"
SECRET="$(echo "$EXT_JSON" | field secret)"
[ -n "$SECRET" ] || fail "no generated secret returned"
log "extension $NUMBER created; the phone registers with the API-generated secret"

TRUNK_JSON="$(api PbxService/CreateTrunk "{\"tenantId\":\"$TENANT\",\"name\":\"UAT carrier\",\"host\":\"$HOST_ADDR\",\"port\":$CARRIER_PORT,\"channelLimit\":2}")" ||
	fail "CreateTrunk: $TRUNK_JSON"
TRUNK_ID="$(echo "$TRUNK_JSON" | field trunk.trunkId)"
api PbxService/CreateRoute "{\"tenantId\":\"$TENANT\",\"prefixPattern\":\"+44\",\"trunkId\":\"$TRUNK_ID\"}" >/dev/null || fail "CreateRoute failed"

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
echo "<sip:$AUTH_USER@127.0.0.1:5060;transport=udp>;auth_user=$AUTH_USER;auth_pass=$SECRET;outbound=\"sip:127.0.0.1:5060;transport=udp\";regint=300" > "$PHONE_DIR/accounts"
# The carrier never registers (regint=0) and answers any Request-URI
# (catchall) — a carrier is addressed by the number dialled, not by name.
echo "<sip:carrier@$HOST_ADDR:$CARRIER_PORT;transport=udp>;regint=0;catchall=yes" > "$CARRIER_DIR/accounts"

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
log "phone registered"

for p in "$PHONE_CTRL" "$CARRIER_CTRL"; do
	reply="$(ctrl_rpc "$p" answermode auto)"
	echo "$reply" | grep -q "changed to: auto" || fail "answermode auto rejected on ctrl port $p: $reply"
done

# --- 4. place the call through the public API ---------------------------
PLACED="$(api PbxService/PlaceCall "{\"tenantId\":\"$TENANT\",\"extensionId\":\"$EXT_ID\",\"destination\":\"$DEST\"}")" || fail "PlaceCall refused: $PLACED"
CALL_ID="$(echo "$PLACED" | field callId)"
log "PlaceCall $DEST → call $CALL_ID"

state=""
for _ in $(seq 1 30); do
	state="$(api TelephonyService/GetCall "{\"tenantId\":\"$TENANT\",\"callId\":\"$CALL_ID\"}" | field state)"
	[ "$state" = "Active" ] && break
	sleep 0.5
done
[ "$state" = "Active" ] || fail "the call never became Active (last state: $state)"
log "call Active — letting media flow for 6 s"
sleep 6

api PbxService/HangupCall "{\"tenantId\":\"$TENANT\",\"callId\":\"$CALL_ID\"}" >/dev/null || fail "HangupCall failed"
for _ in $(seq 1 20); do
	state="$(api TelephonyService/GetCall "{\"tenantId\":\"$TENANT\",\"callId\":\"$CALL_ID\"}" | field state)"
	[ "$state" = "Terminated" ] && break
	sleep 0.5
done
[ "$state" = "Terminated" ] || fail "HangupCall did not end the call (state: $state)"

# The record saying Terminated is not enough: each phone must actually be
# released by the engine. (It was not, before this change — telephony
# destroyed the bridge and left both legs up with media flowing.)
released=0
for _ in $(seq 1 20); do
	if grep -q "terminated (duration" "$PHONE_LOG" && grep -q "terminated (duration" "$CARRIER_LOG"; then
		released=1; break
	fi
	sleep 0.5
done
[ "$released" = "1" ] || fail "HangupCall left a phone on the line: the call ended in the record but not on the engine"
log "both phones released by HangupCall"
sleep 1 # baresip prints its RTP summary as the call closes

# --- 5. endpoint-measured media, both directions, both phones -----------
media_ok=1
for who in phone carrier; do
	if [ "$who" = phone ]; then LOG="$PHONE_LOG"; else LOG="$CARRIER_LOG"; fi
	summary="$(grep -A3 "Transmit:.*Receive" "$LOG" | tail -4)"
	tx="$(echo "$summary" | awk '/^packets:/ {print $2}')"
	rx="$(echo "$summary" | awk '/^packets:/ {print $3}')"
	if [ -z "$tx" ] || [ -z "$rx" ] || [ "$tx" -le 0 ] || [ "$rx" -le 0 ]; then
		echo "UAT-PLACEMENT FAIL: no media on the $who (tx=$tx rx=$rx)"
		media_ok=0
	else
		log "$who media: tx=$tx rx=$rx packets, errors=$(echo "$summary" | awk '/^errors:/ {print $2}')"
	fi
done
[ "$media_ok" = "1" ] || fail "media did not flow on every leg (logs: $PHONE_LOG, $CARRIER_LOG)"

log "PASS: a call placed through the public API crossed a carrier trunk between real phones, with media both ways"
