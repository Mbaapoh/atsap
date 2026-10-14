-- pbx-trunk-health-probing: a trunk's health is an observation with a
-- reason and a time, and a trunk nobody has checked is UNKNOWN — never
-- HEALTHY by default (design D3).
ALTER TABLE carrier_trunks DROP CONSTRAINT carrier_trunks_health_status_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_status_check
        CHECK (health_status IN ('UNKNOWN', 'HEALTHY', 'DEGRADED', 'UNHEALTHY')),
    ALTER COLUMN health_status SET DEFAULT 'UNKNOWN',
    -- Why the trunk has its health; NULL until first observed.
    ADD COLUMN health_reason VARCHAR(50)
        CHECK (health_reason IN ('ANSWERING', 'SLOW_RESPONSE', 'HOST_NOT_FOUND', 'NO_RESPONSE', 'CARRIER_ERROR')),
    -- When it was last observed; NULL until first observed.
    ADD COLUMN health_observed_at TIMESTAMPTZ;

-- Every existing trunk's HEALTHY was the creation default, not an
-- observation: none was ever checked. The first check follows within
-- seconds of the app starting.
--
-- carrier_trunks is FORCE RLS and the app that runs migrations owns it,
-- so without a tenant set this UPDATE would see no row and change
-- nothing. RLS is lifted for this transaction only and restored before
-- it commits (as in 0010).
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
UPDATE carrier_trunks SET health_status = 'UNKNOWN', health_reason = NULL, health_observed_at = NULL;
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
