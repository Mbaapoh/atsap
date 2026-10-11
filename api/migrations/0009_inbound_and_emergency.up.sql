-- pbx-inbound-and-emergency: calls the platform did not originate.
--
-- Domain tables (tenant_id, RLS enabled AND forced, policy each):
--   inbound_numbers    — a tenant's DIDs, each pointing at one extension
--   emergency_numbers  — numbers a tenant ADDS to its emergency set
--   emergency_settings — the tenant's emergency country (design D6)
-- Engine-facing changes (no tenancy; see 0004's comment on ps_*):
--   ps_endpoints.set_var — carries ATSA_TENANT_ID onto every channel (D1)
--   ps_endpoint_id_ips   — identify-by-IP rows for trunks (D5)

-- Target of inbound_numbers' composite foreign key: a DID cannot point at
-- another tenant's extension even though FK checks bypass RLS (the same
-- guard carrier_routes uses).
ALTER TABLE extensions ADD CONSTRAINT extensions_tenant_id_id_key UNIQUE (tenant_id, id);

CREATE TABLE IF NOT EXISTS inbound_numbers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    -- Stored as digits only: a leading + is not significant (design D7).
    number VARCHAR(32) NOT NULL,
    extension_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, number),
    FOREIGN KEY (tenant_id, extension_id) REFERENCES extensions (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS emergency_numbers (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    -- Digits only. Only the tenant's OWN additions live here: 112 and the
    -- country catalogue are code, so no row can remove them.
    number VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, number)
);

CREATE TABLE IF NOT EXISTS emergency_settings (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
    -- ISO 3166-1 alpha-2, upper case.
    country_code CHAR(2) NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE inbound_numbers ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbound_numbers FORCE ROW LEVEL SECURITY;
ALTER TABLE emergency_numbers ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_numbers FORCE ROW LEVEL SECURITY;
ALTER TABLE emergency_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_settings FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_inbound_numbers ON inbound_numbers
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_emergency_numbers ON emergency_numbers
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_emergency_settings ON emergency_settings
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------
-- Engine-facing.
--
-- set_var: endpoint configuration that becomes channel variables. The
-- projector writes ATSA_TENANT_ID=<tenant> here, so a call arriving on
-- the endpoint carries its tenant — set by configuration, so no SIP
-- header can forge it, and no cross-tenant lookup is ever needed (D1).
ALTER TABLE ps_endpoints ADD COLUMN IF NOT EXISTS set_var TEXT;

-- Identify-by-IP. Asterisk's own alembic column set, COMPLETE rather than
-- the subset we write — the lesson of 0006: the engine queries columns
-- we never populate, and a missing one fails silently.
CREATE TABLE IF NOT EXISTS ps_endpoint_id_ips (
    id VARCHAR(255) PRIMARY KEY,
    endpoint VARCHAR(255),
    match VARCHAR(80),
    srv_lookups VARCHAR(5),
    match_header VARCHAR(255),
    match_request_uri VARCHAR(255)
);

-- The engine's first new grant since 0004, approved for this change and
-- recorded in the endpoint-projection spec: an engine-facing table, never
-- a tenant data table.
GRANT SELECT ON ps_endpoint_id_ips TO asterisk_engine;
