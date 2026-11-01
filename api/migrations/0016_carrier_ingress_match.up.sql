-- pbx-carrier-ingress (D-77): the one cross-tenant read the platform
-- makes to attribute a carrier call. Given the call's transport source
-- address and the dialled number, it returns the (tenant, trunk) pairs
-- whose trunk recognises the address and whose tenant has the number,
-- and nothing else.
--
-- carrier_trunks and inbound_numbers are FORCE RLS, and the app owns them
-- without BYPASSRLS (deploy/postgres/init/02-atsapbx.sql). A BYPASSRLS
-- role needs the superuser at bootstrap, which existing installations
-- have already passed, so instead each table gets a SELECT-only policy
-- that applies only while app.ingress_lookup is 'on'. This function is
-- the only code that sets it, for its single query, and clears it before
-- returning, within the caller's transaction.
CREATE POLICY ingress_lookup_carrier_trunks ON carrier_trunks
    FOR SELECT USING (current_setting('app.ingress_lookup', true) = 'on');
CREATE POLICY ingress_lookup_inbound_numbers ON inbound_numbers
    FOR SELECT USING (current_setting('app.ingress_lookup', true) = 'on');

CREATE FUNCTION carrier_ingress_match(source INET, dialled TEXT)
RETURNS TABLE (tenant_id UUID, trunk_id UUID)
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    PERFORM set_config('app.ingress_lookup', 'on', true);
    RETURN QUERY
        SELECT t.tenant_id, t.id
        FROM carrier_trunks t
        WHERE (source = ANY (t.resolved_addresses)
               OR EXISTS (SELECT 1 FROM unnest(t.inbound_addresses) AS r WHERE source <<= r))
          AND EXISTS (SELECT 1 FROM inbound_numbers n WHERE n.tenant_id = t.tenant_id AND n.number = dialled)
        ORDER BY t.tenant_id, t.id;
    PERFORM set_config('app.ingress_lookup', '', true);
END
$$;

REVOKE ALL ON FUNCTION carrier_ingress_match(INET, TEXT) FROM PUBLIC;
