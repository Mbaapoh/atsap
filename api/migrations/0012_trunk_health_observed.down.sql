-- Back to three observed states with HEALTHY as the creation default.
-- UNKNOWN has no three-state equivalent; it returns to that default.
-- RLS is lifted for this transaction only, as in the up migration.
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
UPDATE carrier_trunks SET health_status = 'HEALTHY' WHERE health_status = 'UNKNOWN';
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks
    DROP COLUMN IF EXISTS health_observed_at,
    DROP COLUMN IF EXISTS health_reason,
    ALTER COLUMN health_status SET DEFAULT 'HEALTHY',
    DROP CONSTRAINT carrier_trunks_health_status_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_status_check
        CHECK (health_status IN ('HEALTHY', 'DEGRADED', 'UNHEALTHY'));
