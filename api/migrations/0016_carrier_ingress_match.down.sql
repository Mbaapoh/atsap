DROP FUNCTION IF EXISTS carrier_ingress_sources();
DROP FUNCTION IF EXISTS carrier_ingress_match(INET, TEXT);
DROP POLICY IF EXISTS ingress_lookup_inbound_numbers ON inbound_numbers;
DROP POLICY IF EXISTS ingress_lookup_carrier_trunks ON carrier_trunks;
