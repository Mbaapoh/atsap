-- IVR flows: a DID can ring an extension directly (existing inbound
-- numbers) or enter a flow that chooses an open/closed destination by
-- business hours and holidays. Versions are immutable; publish and
-- rollback move the active pointer (D-89).
CREATE TABLE IF NOT EXISTS ivr_flows (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    number VARCHAR(32) NOT NULL,
    name VARCHAR(100) NOT NULL,
    active_version INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, number)
);
ALTER TABLE ivr_flows ENABLE ROW LEVEL SECURITY;
ALTER TABLE ivr_flows FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_ivr_flows ON ivr_flows
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON ivr_flows TO atsapbx_app;

CREATE TABLE IF NOT EXISTS ivr_flow_versions (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    flow_id UUID NOT NULL,
    version INT NOT NULL,
    open_extension_id UUID,
    closed_extension_id UUID,
    business_start_minute INT NOT NULL,
    business_end_minute INT NOT NULL,
    prompt VARCHAR(255) NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_by UUID NOT NULL,
    PRIMARY KEY (tenant_id, flow_id, version),
    FOREIGN KEY (flow_id) REFERENCES ivr_flows(id) ON DELETE CASCADE,
    CHECK (business_start_minute BETWEEN 0 AND 1439),
    CHECK (business_end_minute BETWEEN 0 AND 1439)
);
ALTER TABLE ivr_flow_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE ivr_flow_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_ivr_versions ON ivr_flow_versions
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT ON ivr_flow_versions TO atsapbx_app;

CREATE TABLE IF NOT EXISTS ivr_holidays (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    name VARCHAR(100) NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, day)
);
ALTER TABLE ivr_holidays ENABLE ROW LEVEL SECURITY;
ALTER TABLE ivr_holidays FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_ivr_holidays ON ivr_holidays
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT, DELETE ON ivr_holidays TO atsapbx_app;
