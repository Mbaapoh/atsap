-- ADDRESS_NOT_ALLOWED has no earlier equivalent; it becomes NO_RESPONSE,
-- what the previous version reported for such a trunk. The dropped
-- loopback addresses are not restored: they never recognised a carrier.
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
UPDATE carrier_trunks SET health_reason = 'NO_RESPONSE' WHERE health_reason = 'ADDRESS_NOT_ALLOWED';
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DROP CONSTRAINT IF EXISTS carrier_trunks_health_reason_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_reason_check
        CHECK (health_reason IN ('ANSWERING', 'SLOW_RESPONSE', 'HOST_NOT_FOUND', 'NO_RESPONSE', 'CARRIER_ERROR'));
ALTER TABLE carrier_trunks DROP COLUMN IF EXISTS inbound_addresses;
