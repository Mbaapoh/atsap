#!/usr/bin/env bash
# console-soak.sh runs the console's integration tier against the Tilt rig
# RUNS times in a row (default 3), then proves it left nothing behind:
# every tenant-data and engine table holds as many rows as before, and
# `pbx reconcile` finds the engine matching the platform
# (console-admin-screens task 6.1).
set -euo pipefail

RUNS="${RUNS:-3}"
PG=(docker exec -i deploy-postgres-1 psql -q -At -v ON_ERROR_STOP=1 -U asterisk -d atsapbx)
TABLES=(tenants users user_credentials principals role_bindings sessions password_reset_tickets api_keys
  extensions carrier_trunks carrier_routes inbound_numbers emergency_numbers emergency_settings
  calls call_participants placed_call_legs usage_seconds
  ps_endpoints ps_auths ps_aors ps_endpoint_id_ips)

snapshot() {
  local sql=""
  for t in "${TABLES[@]}"; do sql+="SELECT '$t', count(*) FROM $t;"; done
  "${PG[@]}" -F ' ' -c "$sql"
}

before="$(snapshot)"
for i in $(seq 1 "$RUNS"); do
  echo "== integration run $i of $RUNS"
  (cd console && mise exec -- npm run -s integration)
done
after="$(snapshot)"

if [[ "$before" != "$after" ]]; then
  echo "FAIL: the integration tier left rows behind (table count before -> after):" >&2
  diff <(echo "$before") <(echo "$after") >&2 || true
  exit 1
fi
echo "No residue: ${#TABLES[@]} tables unchanged after $RUNS runs."
docker exec deploy-app-1 /out/app pbx reconcile
