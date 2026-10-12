#!/usr/bin/env bash
# dev-user.sh — a console login for trying the UI on the dev rig.
#
# Creates (or resets) dev@example.test with a password, as administrator of
# two dev Tenants, so the Tenant picker and switcher can be tried too. Dev
# rig only: it writes straight to the rig's Postgres, as the UAT scripts do.
# The password is the repository's synthetic fixture credential (D-39),
# never a real one. Safe to run again: it resets the password and clears
# the rig's sign-in throttle.
set -euo pipefail

EMAIL="dev@example.test"
PASSWORD="e2e-fixture-operator-password"
HASH='$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc'
ACME="d0000000-0000-4000-8000-00000000ac3e"
GLOBEX="d0000000-0000-4000-8000-0000000910be"

docker inspect deploy-postgres-1 >/dev/null 2>&1 || {
	echo "dev-user: the dev rig is not running (make tilt-up)" >&2
	exit 1
}

psql_rig() { docker exec -i deploy-postgres-1 psql -q -v ON_ERROR_STOP=1 -U asterisk -d atsapbx "$@"; }

membership() { # tenant-id
	cat <<SQL
BEGIN;
SELECT set_config('app.tenant_id', '$1', true);
INSERT INTO principals (id, tenant_id, user_id, role, status)
SELECT gen_random_uuid(), '$1', u.id, 'TENANT_ADMIN', 'ACTIVE' FROM users u
WHERE u.email = '$EMAIL' AND NOT EXISTS (SELECT 1 FROM principals p WHERE p.tenant_id = '$1' AND p.user_id = u.id);
INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
SELECT p.id, '$1', 'TENANT_ADMIN', 'tenant' FROM principals p JOIN users u ON u.id = p.user_id
WHERE p.tenant_id = '$1' AND u.email = '$EMAIL'
  AND NOT EXISTS (SELECT 1 FROM role_bindings b WHERE b.principal_id = p.id AND b.role = 'TENANT_ADMIN');
COMMIT;
SQL
}

psql_rig >/dev/null <<SQL
INSERT INTO tenants (id, name, status, residency_zone) VALUES
  ('$ACME', 'Dev Acme', 'ACTIVE', 'EU'), ('$GLOBEX', 'Dev Globex', 'ACTIVE', 'EU')
ON CONFLICT (id) DO NOTHING;
INSERT INTO users (email, display_name) VALUES ('$EMAIL', 'Dev administrator') ON CONFLICT DO NOTHING;
DELETE FROM user_credentials WHERE kind = 'password' AND user_id = (SELECT id FROM users WHERE email = '$EMAIL');
INSERT INTO user_credentials (user_id, kind, secret) SELECT id, 'password', '$HASH' FROM users WHERE email = '$EMAIL';
DELETE FROM login_throttle;
$(membership "$ACME")
$(membership "$GLOBEX")
SQL

cat <<MSG
Console login ready (dev rig):
  URL       http://localhost:5173
  Email     $EMAIL
  Password  $PASSWORD
  Tenants   Dev Acme, Dev Globex (you will be asked to choose)
Reset emails land in Mailpit: http://localhost:8025
MSG
