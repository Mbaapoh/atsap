-- identity revision 2 (LLD-02 §5.4; D-61–D-64, D-66; openspec change
-- identity-accounts-and-sessions).
--
-- A user is one person across the installation; a principal becomes that
-- user's membership of one tenant, keeping its id so every audit row and
-- role binding that names it still names the same membership.
--
-- The tables below that carry no tenant_id describe a person or the
-- installation, not a tenant. They are read before any tenant is known
-- (sign-in by email), so they have no tenant RLS policy; only
-- internal/identity/postgres may name them, which archtest enforces.
--
-- The whole file runs as one implicit transaction: the duplicate check
-- below aborts it with nothing changed.

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,             -- stored normalized
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_status_valid CHECK (status IN ('ACTIVE', 'DISABLED'))
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (lower(email));

-- Credentials by kind (D-66). Only 'password' may be written until
-- identity-mfa widens the CHECK, so nothing half-supported can exist.
CREATE TABLE IF NOT EXISTS user_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(20) NOT NULL,
    secret TEXT NOT NULL,                    -- password: Argon2id, never plaintext (INV-11)
    must_change BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_credentials_kind_valid CHECK (kind IN ('password'))
);
CREATE UNIQUE INDEX IF NOT EXISTS user_credentials_one_password
    ON user_credentials (user_id) WHERE kind = 'password';

-- principals is FORCE RLS, and the app that runs migrations owns it, so
-- under FORCE it would see no row without a tenant set — and the
-- duplicate check below would pass vacuously. RLS is lifted for this
-- transaction only and restored before it commits.
ALTER TABLE principals NO FORCE ROW LEVEL SECURITY;
ALTER TABLE principals DISABLE ROW LEVEL SECURITY;

-- Refuse to merge two principals that share an email but hold different
-- passwords: that is two credentials, and choosing one locks a person out
-- (D-61). The operator resolves it; nothing is merged silently.
DO $$
DECLARE
    conflicts TEXT;
BEGIN
    SELECT string_agg(lower(trim(email)) || ' (principals ' || ids || ')', '; ')
      INTO conflicts
      FROM (
        SELECT email, string_agg(id::text, ', ' ORDER BY id) AS ids
          FROM (SELECT id, lower(trim(email)) AS email, password_hash FROM principals) p
         GROUP BY email
        HAVING count(DISTINCT password_hash) > 1
      ) dup;
    IF conflicts IS NOT NULL THEN
        RAISE EXCEPTION 'identity accounts migration: principals share an email but have different passwords: %. Give each person a distinct email, or delete the duplicate principal, then migrate again.', conflicts;
    END IF;
END $$;

INSERT INTO users (email, display_name, created_at)
SELECT lower(trim(email)), '', min(created_at)
  FROM principals
 GROUP BY lower(trim(email));

INSERT INTO user_credentials (user_id, kind, secret)
SELECT DISTINCT ON (u.id) u.id, 'password', p.password_hash
  FROM principals p JOIN users u ON lower(u.email) = lower(trim(p.email))
 ORDER BY u.id, p.created_at;

ALTER TABLE principals ADD COLUMN user_id UUID REFERENCES users(id);
UPDATE principals p SET user_id = u.id FROM users u WHERE lower(u.email) = lower(trim(p.email));
ALTER TABLE principals ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE principals ADD CONSTRAINT principals_tenant_user_key UNIQUE (tenant_id, user_id);
ALTER TABLE principals DROP CONSTRAINT IF EXISTS principals_tenant_id_username_key;
ALTER TABLE principals DROP COLUMN username;
ALTER TABLE principals DROP COLUMN email;
ALTER TABLE principals DROP COLUMN password_hash;
CREATE INDEX IF NOT EXISTS principals_user_idx ON principals (user_id);

ALTER TABLE principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE principals FORCE ROW LEVEL SECURITY;

-- A person may read their own memberships, and those memberships' role
-- bindings, across tenants — and nothing else. Permissive policies are
-- OR'd with tenant_isolation_*, so a tenant-scoped transaction is
-- unaffected; a transaction that sets only app.user_id sees exactly that
-- person's rows (D-61: the one place a person's tenants are listed).
CREATE POLICY principals_self ON principals FOR SELECT
    USING (user_id = NULLIF(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY role_bindings_self ON role_bindings FOR SELECT
    USING (principal_id IN (
        SELECT id FROM principals
         WHERE user_id = NULLIF(current_setting('app.user_id', true), '')::uuid));

-- Console sessions (D-62). Secrets are stored only as SHA-256 digests.
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    principal_id UUID REFERENCES principals(id) ON DELETE SET NULL,
    refresh_digest VARCHAR(64) NOT NULL,
    previous_digest VARCHAR(64),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoke_reason VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS sessions_refresh_digest_key ON sessions (refresh_digest);
CREATE INDEX IF NOT EXISTS sessions_previous_digest_idx ON sessions (previous_digest);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);

CREATE TABLE IF NOT EXISTS password_reset_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    digest VARCHAR(64) NOT NULL UNIQUE,
    purpose VARCHAR(20) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT password_reset_tickets_purpose_valid CHECK (purpose IN ('reset', 'invite'))
);
CREATE INDEX IF NOT EXISTS password_reset_tickets_user_idx ON password_reset_tickets (user_id, created_at);

-- SSO-ready (D-63): modelled, no protocol yet. No client-secret column
-- until a protocol needs one — an unused secret column is a liability.
CREATE TABLE IF NOT EXISTS identity_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    kind VARCHAR(10) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    issuer VARCHAR(512) NOT NULL,
    mode VARCHAR(20) NOT NULL DEFAULT 'DISABLED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT identity_providers_kind_valid CHECK (kind IN ('OIDC', 'SAML')),
    CONSTRAINT identity_providers_mode_valid CHECK (mode IN ('DISABLED', 'OPTIONAL', 'ENFORCED'))
);
ALTER TABLE identity_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_identity_providers ON identity_providers
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
-- Sign-in discovery reads a provider by its verified domain before any
-- tenant is known; it may read only providers that hold a verified claim.
CREATE TABLE IF NOT EXISTS identity_provider_domains (
    domain VARCHAR(255) PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES identity_providers(id) ON DELETE CASCADE,
    verified_at TIMESTAMPTZ
);
CREATE POLICY identity_providers_discovery ON identity_providers FOR SELECT
    USING (current_setting('app.discovery', true) = 'on'
           AND id IN (SELECT provider_id FROM identity_provider_domains WHERE verified_at IS NOT NULL));

CREATE TABLE IF NOT EXISTS external_identities (
    provider_id UUID NOT NULL REFERENCES identity_providers(id) ON DELETE CASCADE,
    subject VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_id, subject)
);

-- Login throttling (D-64): keys are digests, never the email or address.
CREATE TABLE IF NOT EXISTS login_throttle (
    key_digest VARCHAR(64) PRIMARY KEY,
    failures INT NOT NULL DEFAULT 0,
    blocked_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Authentication history (D-64). Append-only: the store has no update or
-- delete path, as for audit_logs.
CREATE TABLE IF NOT EXISTS auth_events (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    kind VARCHAR(50) NOT NULL,
    source_address INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS auth_events_user_idx ON auth_events (user_id, created_at DESC);
