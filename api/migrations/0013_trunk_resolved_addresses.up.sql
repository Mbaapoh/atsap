-- pbx-trunk-host-resolution (D-75): a trunk's carrier is recognised by
-- address only. These are the addresses it is recognised by: its host when
-- that is an IP address, otherwise what the platform last resolved the
-- name to (empty until it has). resolved_at is when a name last resolved;
-- NULL for an IP host or a name not resolved yet.
ALTER TABLE carrier_trunks
    ADD COLUMN resolved_addresses INET[] NOT NULL DEFAULT '{}',
    ADD COLUMN resolved_at TIMESTAMPTZ;

-- An IP host is its own address from now on. A name is resolved by the
-- first check round after the app starts; until then its identify row
-- keeps its old value, so no carrier is refused across the upgrade.
--
-- A host is either an IP literal or a DNS name (domain.ValidateHost), and
-- a name such as "1.2.3" is a valid name but not an address, so each host
-- is tried as an address and skipped when it is not one.
--
-- carrier_trunks is FORCE RLS and the app that runs migrations owns it;
-- RLS is lifted for this transaction only and restored before it commits
-- (as in 0010 and 0012).
ALTER TABLE carrier_trunks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks DISABLE ROW LEVEL SECURITY;
DO $$
DECLARE
    t RECORD;
BEGIN
    FOR t IN SELECT id, host FROM carrier_trunks LOOP
        BEGIN
            UPDATE carrier_trunks SET resolved_addresses = ARRAY[t.host::inet] WHERE id = t.id;
        EXCEPTION WHEN invalid_text_representation THEN
            NULL; -- a name: resolved by the first check
        END;
    END LOOP;
END
$$;
ALTER TABLE carrier_trunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE carrier_trunks FORCE ROW LEVEL SECURITY;
