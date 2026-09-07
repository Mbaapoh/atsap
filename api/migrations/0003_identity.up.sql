-- identity bounded context schema (LLD-02; openspec change
-- identity-auth-rbac). Tables are exactly those in
-- docs/hld/03-domain-model.md §5 that this change owns.

CREATE TABLE IF NOT EXISTS principals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    -- Argon2id encoded hash, never a plaintext or reversible form
    -- (INV-11). Parameters live in the hasher, not here.
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'AGENT',
    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Usernames are unique per tenant, never globally: two tenants may
    -- each have an "admin" and they are unrelated principals (AC-01.2).
    UNIQUE(tenant_id, username)
);

CREATE TABLE IF NOT EXISTS role_bindings (
    principal_id UUID NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    role VARCHAR(50) NOT NULL,
    scope VARCHAR(100) NOT NULL DEFAULT 'tenant',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (principal_id, role, scope)
);

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    principal_id UUID NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    -- SHA-256 hex digest of the presented key. The raw key is returned
    -- to the caller once at creation and never stored.
    key_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS api_keys_key_hash_idx ON api_keys (key_hash);

-- Append-only: this schema deliberately provides no UPDATE or DELETE
-- path, and the Go repository exposes none either (AC-01.3/01.4).
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    -- A bare UUID, not a principals FK: system-initiated mutations (a
    -- scheduled worker, not a person) record a well-known sentinel
    -- actor, and actor_type distinguishes them from a real principal.
    actor_id UUID NOT NULL,
    actor_type VARCHAR(50) NOT NULL,
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    resource_id VARCHAR(100) NOT NULL,
    before_state JSONB,
    after_state JSONB,
    ip_address INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_logs_tenant_created_idx
    ON audit_logs (tenant_id, created_at DESC);

-- Row-Level Security. FORCE (not just ENABLE) is required because the
-- app owns these tables and PostgreSQL exempts an owner from its own
-- policies otherwise — the LLD-01 lesson, applied from the start here
-- rather than discovered again.
ALTER TABLE principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE principals FORCE ROW LEVEL SECURITY;
ALTER TABLE role_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_bindings FORCE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_principals ON principals
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_role_bindings ON role_bindings
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_api_keys ON api_keys
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_audit ON audit_logs
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Remove the row the retired 0002 dev-tenant seed used to create. Real
-- tenants are provisioned through identity from here on; a migration
-- never conjures one.
DELETE FROM tenants WHERE id = '00000000-0000-0000-0000-000000000001';
