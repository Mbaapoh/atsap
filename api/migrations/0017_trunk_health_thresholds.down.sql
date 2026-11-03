DROP TABLE IF EXISTS trunk_health_samples;
ALTER TABLE carrier_trunks DROP COLUMN IF EXISTS healthy_since;
-- Rows degraded by calls carry a reason the previous constraint did not
-- allow; give them a valid one before restoring it. RLS is lifted for
-- this statement only, as in 0012/0013/0014: the migration must see every
-- tenant's rows for the constraint to be valid.
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
UPDATE carrier_trunks SET health_reason = 'NO_RESPONSE' WHERE health_reason = 'CALL_SETUP_FAILURES';
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DROP CONSTRAINT IF EXISTS carrier_trunks_health_reason_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_reason_check
        CHECK (health_reason IN ('ANSWERING', 'SLOW_RESPONSE', 'HOST_NOT_FOUND', 'NO_RESPONSE',
                                 'CARRIER_ERROR', 'ADDRESS_NOT_ALLOWED'));
