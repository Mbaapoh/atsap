import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";

// The dev rig (make tilt-up): Postgres in deploy-postgres-1, the API on
// :8080 behind the console's dev proxy, Mailpit on :8025. Fixtures are
// written straight to the database as the UAT scripts do, then exercised
// only through the console and its public API.

// FIXTURE_PASSWORD and its Argon2id hash are the repository's synthetic
// operator credential (D-39), shared with the pbx e2e suite and the UAT
// scripts.
export const FIXTURE_PASSWORD = "e2e-fixture-operator-password";
const FIXTURE_HASH = "$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc";

const MAILPIT = "http://localhost:8025";

function psql(sql: string): string {
  return execFileSync("docker", ["exec", "-i", "deploy-postgres-1", "psql", "-q", "-At", "-v", "ON_ERROR_STOP=1", "-U", "asterisk", "-d", "atsapbx"], {
    input: sql,
    encoding: "utf8",
  });
}

// rigAvailable reports whether the rig is up, so a spec can skip with a
// reason instead of failing on a laptop without it.
export async function rigAvailable(): Promise<boolean> {
  try {
    psql("SELECT 1;");
    const res = await fetch("http://localhost:8080/healthz");
    return res.ok;
  } catch {
    return false;
  }
}

export interface RigUser {
  email: string;
  tenants: { id: string; name: string }[];
  cleanup: () => void;
}

// seedUser creates one person with a password and an administrator
// membership in each of tenantNames' new Tenants.
export function seedUser(tenantNames: string[]): RigUser {
  const tag = randomUUID().slice(0, 8);
  const email = `console-e2e-${tag}@example.test`;
  const tenants = tenantNames.map((n) => ({ id: randomUUID(), name: `${n} ${tag}` }));
  const memberships = tenants
    .map(
      (t) => `
BEGIN;
SELECT set_config('app.tenant_id', '${t.id}', true);
WITH p AS (
  INSERT INTO principals (id, tenant_id, user_id, role, status)
  VALUES (gen_random_uuid(), '${t.id}', (SELECT id FROM users WHERE email = '${email}'), 'TENANT_ADMIN', 'ACTIVE')
  RETURNING id
)
INSERT INTO role_bindings (principal_id, tenant_id, role, scope) SELECT id, '${t.id}', 'TENANT_ADMIN', 'tenant' FROM p;
COMMIT;`,
    )
    .join("\n");
  psql(`
${tenants.map((t) => `INSERT INTO tenants (id, name, status, residency_zone) VALUES ('${t.id}', '${t.name}', 'ACTIVE', 'EU');`).join("\n")}
WITH u AS (INSERT INTO users (email, display_name) VALUES ('${email}', 'Console e2e') RETURNING id)
INSERT INTO user_credentials (user_id, kind, secret) SELECT id, 'password', '${FIXTURE_HASH}' FROM u;
${memberships}
`);
  const ids = tenants.map((t) => `'${t.id}'`).join(",");
  // Throttle keys are digests (D-66), so a spec's own counters are found
  // by when they changed: deliberate failures (wrong password, used
  // link) must not lock out the next run from the same address.
  const startedAt = psql("SELECT now();").trim();
  return {
    email,
    tenants,
    cleanup: () =>
      psql(`
DELETE FROM audit_logs WHERE tenant_id IN (${ids});
DELETE FROM outbox WHERE tenant_id IN (${ids});
DELETE FROM auth_events WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
DELETE FROM principals WHERE tenant_id IN (${ids});
DELETE FROM users WHERE email = '${email}';
DELETE FROM tenants WHERE id IN (${ids});
DELETE FROM login_throttle WHERE updated_at >= '${startedAt}';
`),
  };
}

// latestLink waits for the newest email to `to` and returns the console
// link in it (/reset#… or /invite#…).
export async function latestLink(to: string, timeoutMs = 15_000): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const search = await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`);
    const { messages } = (await search.json()) as { messages: { ID: string }[] };
    const first = messages[0];
    if (first) {
      const msg = (await (await fetch(`${MAILPIT}/api/v1/message/${first.ID}`)).json()) as { Text: string };
      const link = /https?:\/\/\S+\/(?:reset|invite)#\S+/.exec(msg.Text)?.[0];
      if (link) return link;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`no link emailed to ${to}`);
}

// adminResetPassword runs the operator recovery command inside the app
// container (D-66 §3) and returns the temporary, must-change password it
// prints once.
export function adminResetPassword(email: string): string {
  const out = execFileSync("docker", ["exec", "deploy-app-1", "/out/app", "admin", "reset-password", "-email", email], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  });
  const temporary = out.trim().split("\n").pop()?.trim();
  if (!temporary) throw new Error("admin reset-password printed no password");
  return temporary;
}
