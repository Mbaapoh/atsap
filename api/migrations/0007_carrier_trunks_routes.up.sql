-- Carrier trunks and outbound routes (LLD-03 §3/§7.4; openspec change
-- pbx-trunks-routes). The same two-kinds-of-table split 0004 states:
--
--   1. carrier_trunks / carrier_routes are DOMAIN tables: tenant_id, RLS
--      enabled AND forced, a tenant_isolation_* policy each.
--   2. The trunk's engine state goes into the existing ps_* projection
--      tables, which gain the three columns an outbound carrier endpoint
--      needs. No new ps_* table and no new engine grant (design D7).

CREATE TABLE IF NOT EXISTS carrier_trunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL DEFAULT 5060 CHECK (port BETWEEN 1 AND 65535),
    -- Optional carrier digest username (design D7). Configuration, not a
    -- secret: it is returned on reads, the way every SIP-trunk UI shows it.
    auth_username VARCHAR(100),
    -- A reference to where the secret rests, never the secret. NULL for
    -- an IP-authenticated carrier with no credential (LLD-03 §7.4).
    credentials_ref VARCHAR(255),
    channel_limit INTEGER NOT NULL DEFAULT 100 CHECK (channel_limit > 0),
    priority INTEGER NOT NULL DEFAULT 1,
    -- Observed, never set by a caller: written only by health reports.
    health_status VARCHAR(50) NOT NULL DEFAULT 'HEALTHY'
        CHECK (health_status IN ('HEALTHY', 'DEGRADED', 'UNHEALTHY')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Target of carrier_routes' composite foreign key below.
    UNIQUE (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS carrier_routes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    prefix_pattern VARCHAR(51) NOT NULL,
    trunk_id UUID NOT NULL,
    cost_rate_per_min BIGINT NOT NULL DEFAULT 0 CHECK (cost_rate_per_min >= 0),
    priority INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Composite, not trunk_id alone. Foreign-key checks bypass RLS, so a
    -- plain REFERENCES carrier_trunks(id) would let a route in tenant A
    -- point at tenant B's trunk if handed its id. Including tenant_id
    -- makes that impossible in the schema, not only in the application.
    FOREIGN KEY (tenant_id, trunk_id) REFERENCES carrier_trunks (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS carrier_routes_trunk_idx ON carrier_routes (tenant_id, trunk_id);

ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_routes ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_routes FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_carrier_trunks ON carrier_trunks
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_carrier_routes ON carrier_routes
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------
-- Projection columns for an outbound carrier endpoint. All nullable, so
-- every extension row 0004 wrote is unchanged: NULL takes Asterisk's
-- default, which is what an extension endpoint wants.
--
--   outbound_auth  the ps_auths row presented TO the carrier. Extension
--                  endpoints use `auth` (credentials a device presents to
--                  us); a trunk is the reverse direction.
--   from_user      the From user, the auth username (FreePBX's default).
--   from_domain    the From domain, the carrier host.
--
-- The trunk secret itself goes into ps_auths.password, which 0004
-- created and has left NULL for every extension. That is the LLD-03 §7.4
-- exception, and only trunk rows ("t_" ids) may use it.
-- ---------------------------------------------------------------------

ALTER TABLE ps_endpoints ADD COLUMN IF NOT EXISTS outbound_auth VARCHAR(255);
ALTER TABLE ps_endpoints ADD COLUMN IF NOT EXISTS from_user VARCHAR(100);
ALTER TABLE ps_endpoints ADD COLUMN IF NOT EXISTS from_domain VARCHAR(255);
