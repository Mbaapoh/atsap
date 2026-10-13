import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";

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

// rigAvailable reports whether the rig is up. A spec skips with a reason
// when it is not — unless ATSAPBX_RIG_REQUIRED=1 (set by the make targets
// that run these tiers), where a missing rig is a failure, never a pass.
export async function rigAvailable(): Promise<boolean> {
  let up = false;
  try {
    psql("SELECT 1;");
    up = (await fetch("http://localhost:8080/healthz")).ok;
  } catch {
    up = false;
  }
  if (!up && process.env.ATSAPBX_RIG_REQUIRED === "1") {
    throw new Error("the dev rig is not running (make tilt-up), and this tier requires it");
  }
  return up;
}

export interface RigUser {
  email: string;
  tenants: { id: string; name: string }[];
  cleanup: () => void;
}

export interface SeedOptions {
  // platformAdmin makes the membership in the first Tenant a platform
  // administrator with installation-wide (system) scope, as
  // `atsap-api bootstrap` creates one.
  platformAdmin?: boolean;
}

// seedUser creates one person with a password and an administrator
// membership in each of tenantNames' new Tenants.
export function seedUser(tenantNames: string[], options: SeedOptions = {}): RigUser {
  const tag = randomUUID().slice(0, 8);
  const email = `console-e2e-${tag}@example.test`;
  const tenants = tenantNames.map((n) => ({ id: randomUUID(), name: `${n} ${tag}` }));
  const memberships = tenants
    .map((t, i) => {
      const [role, scope] = options.platformAdmin && i === 0 ? ["PLATFORM_ADMIN", "system"] : ["TENANT_ADMIN", "tenant"];
      return `
BEGIN;
SELECT set_config('app.tenant_id', '${t.id}', true);
WITH p AS (
  INSERT INTO principals (id, tenant_id, user_id, role, status)
  VALUES (gen_random_uuid(), '${t.id}', (SELECT id FROM users WHERE email = '${email}'), '${role}', 'ACTIVE')
  RETURNING id
)
INSERT INTO role_bindings (principal_id, tenant_id, role, scope) SELECT id, '${t.id}', '${role}', '${scope}' FROM p;
COMMIT;`;
    })
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
DELETE FROM inbound_numbers WHERE tenant_id IN (${ids});
DELETE FROM emergency_numbers WHERE tenant_id IN (${ids});
DELETE FROM emergency_settings WHERE tenant_id IN (${ids});
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

// rigLicenceKey is the licence token the rig runs on (`make dev-licence`
// writes it to the repository's .env, which is never committed). Applying
// it again through the console leaves the rig licensed exactly as before.
export function rigLicenceKey(): string {
  const env = readFileSync(new URL("../../../.env", import.meta.url), "utf8");
  const key = /^ATSAPBX_LICENSE_TOKEN=(.+)$/m.exec(env)?.[1]?.trim();
  if (!key) throw new Error("no ATSAPBX_LICENSE_TOKEN in .env: run make dev-licence");
  return key;
}

// forgetUser removes a person a spec created through the console (their
// memberships must already be gone, e.g. with their tenant's cleanup).
export function forgetUser(email: string): void {
  psql(`
DELETE FROM principals WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
DELETE FROM auth_events WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
DELETE FROM users WHERE email = '${email}';
`);
}

// engineCredential reads the digest Asterisk holds for an endpoint id
// (ps_auths.md5_cred), so a spec can prove the engine's copy changed — or
// is gone — without ever reading a secret.
export function engineCredential(endpointId: string): string {
  return psql(`SELECT COALESCE(md5_cred, '') FROM ps_auths WHERE id = '${endpointId}';`).trim();
}

// engineAuthFingerprint is a digest of whatever credential the engine
// holds for an endpoint (ps_auths password and md5_cred), so a spec can
// prove it unchanged, changed or gone without reading it.
export function engineAuthFingerprint(endpointId: string): string {
  return psql(`SELECT md5(COALESCE(password, '') || '|' || COALESCE(md5_cred, '')) FROM ps_auths WHERE id = '${endpointId}';`).trim();
}

// trunkEndpointId is the engine endpoint id of a trunk ("t_" + its UUID's
// hex), as pbx/domain/trunk.go derives it.
export function trunkEndpointId(trunkId: string): string {
  return `t_${trunkId.replace(/-/g, "")}`;
}
