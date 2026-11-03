-- pbx-trunk-health-thresholds (D-80, HLD T-6): health is decided from a
-- rolling window of checks and call outcomes, and recovery takes five
-- continuous healthy minutes.

-- healthy_since starts the recovery window; nil while Healthy, or not
-- recovering.
ALTER TABLE carrier_trunks ADD COLUMN healthy_since TIMESTAMPTZ;

-- A call setup failure is a reason health changed, not an engine error.
ALTER TABLE carrier_trunks DROP CONSTRAINT IF EXISTS carrier_trunks_health_reason_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_reason_check
        CHECK (health_reason IN ('ANSWERING', 'SLOW_RESPONSE', 'HOST_NOT_FOUND', 'NO_RESPONSE',
                                 'CARRIER_ERROR', 'ADDRESS_NOT_ALLOWED', 'CALL_SETUP_FAILURES'));

-- One row per observation. Tenant-scoped and RLS-forced like the trunk
-- table; the rolling window is exact across restarts.
CREATE TABLE IF NOT EXISTS trunk_health_samples (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL,
    trunk_id UUID NOT NULL,
    source VARCHAR(8) NOT NULL CHECK (source IN ('PROBE', 'CALL')),
    healthy BOOLEAN NOT NULL,
    reason VARCHAR(50) NOT NULL,
    at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, trunk_id) REFERENCES carrier_trunks (tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS trunk_health_samples_window
    ON trunk_health_samples (tenant_id, trunk_id, at DESC);

ALTER TABLE trunk_health_samples ENABLE ROW LEVEL SECURITY;
ALTER TABLE trunk_health_samples FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_trunk_health_samples ON trunk_health_samples
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);

GRANT SELECT, INSERT, DELETE ON trunk_health_samples TO atsapbx_app;
GRANT USAGE, SELECT ON SEQUENCE trunk_health_samples_id_seq TO atsapbx_app;
