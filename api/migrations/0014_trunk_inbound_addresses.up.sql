-- pbx-carrier-addresses (D-76): a carrier's published signalling
-- addresses and ranges, recognised alongside its host's addresses.
ALTER TABLE carrier_trunks
    ADD COLUMN inbound_addresses CIDR[] NOT NULL DEFAULT '{}';

-- A host name that resolves only to addresses that cannot be a carrier is
-- observed as ADDRESS_NOT_ALLOWED.
ALTER TABLE carrier_trunks DROP CONSTRAINT IF EXISTS carrier_trunks_health_reason_check;
ALTER TABLE carrier_trunks
    ADD CONSTRAINT carrier_trunks_health_reason_check
        CHECK (health_reason IN ('ANSWERING', 'SLOW_RESPONSE', 'HOST_NOT_FOUND', 'NO_RESPONSE', 'CARRIER_ERROR', 'ADDRESS_NOT_ALLOWED'));

-- Addresses recorded under D-75 that cannot be a carrier — a "localhost"
-- trunk's 127.0.0.1 and ::1 — are dropped: they are the platform's own.
-- RLS is lifted for this transaction only, as in 0012 and 0013.
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
UPDATE carrier_trunks t SET resolved_addresses = COALESCE((
    SELECT array_agg(a ORDER BY a) FROM unnest(t.resolved_addresses) AS a
    WHERE NOT (a <<= inet '127.0.0.0/8' OR a = inet '::1' OR a <<= inet '169.254.0.0/16'
            OR a <<= inet 'fe80::/10' OR a <<= inet '0.0.0.0/8' OR a = inet '::'
            OR a <<= inet '224.0.0.0/4' OR a <<= inet 'ff00::/8')
), '{}')
WHERE resolved_addresses <> '{}';
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
