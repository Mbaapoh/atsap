-- Toll-fraud guard: configuration, a rated spend ledger, an attempt log
-- for anomaly detection, and a per-tenant suspension (D-85, AC-08.1/08.2).

CREATE TABLE IF NOT EXISTS tenant_outbound_policies (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    daily_spend_cap_micros BIGINT NOT NULL DEFAULT 0 CHECK (daily_spend_cap_micros >= 0),
    monthly_spend_cap_micros BIGINT NOT NULL DEFAULT 0 CHECK (monthly_spend_cap_micros >= 0),
    blocked_prefixes TEXT[] NOT NULL DEFAULT '{}',
    anomaly_call_threshold INT NOT NULL DEFAULT 20 CHECK (anomaly_call_threshold >= 0),
    anomaly_window_seconds INT NOT NULL DEFAULT 600 CHECK (anomaly_window_seconds >= 0),
    business_start_minute INT NOT NULL DEFAULT 480 CHECK (business_start_minute BETWEEN 0 AND 1439),
    business_end_minute INT NOT NULL DEFAULT 1080 CHECK (business_end_minute BETWEEN 0 AND 1439),
    suspended BOOLEAN NOT NULL DEFAULT FALSE,
    suspended_at TIMESTAMPTZ,
    suspended_reason TEXT NOT NULL DEFAULT '',
    last_cap_alert_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE tenant_outbound_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_outbound_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_outbound_policies ON tenant_outbound_policies
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT, UPDATE ON tenant_outbound_policies TO atsapbx_app;

CREATE TABLE IF NOT EXISTS tenant_spend_ledger (
    tenant_id UUID NOT NULL,
    day DATE NOT NULL,
    cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),
    call_count INT NOT NULL DEFAULT 0 CHECK (call_count >= 0),
    PRIMARY KEY (tenant_id, day)
);
ALTER TABLE tenant_spend_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_spend_ledger FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_spend_ledger ON tenant_spend_ledger
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT, UPDATE ON tenant_spend_ledger TO atsapbx_app;

-- Every trunk-bound attempt is recorded in the same transaction that
-- checks the anomaly window, so concurrent calls cannot all see an
-- under-threshold count.
CREATE TABLE IF NOT EXISTS outbound_attempts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL,
    destination VARCHAR(64) NOT NULL,
    international BOOLEAN NOT NULL,
    at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS outbound_attempts_window ON outbound_attempts (tenant_id, at DESC);
ALTER TABLE outbound_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbound_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_outbound_attempts ON outbound_attempts
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT ON outbound_attempts TO atsapbx_app;
GRANT USAGE, SELECT ON SEQUENCE outbound_attempts_id_seq TO atsapbx_app;
