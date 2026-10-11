-- Trunk engine rows go with the trunks: with carrier_trunks dropped
-- nothing could account for them, and an endpoint the platform has no
-- record of is exactly what projection exists to prevent. They are
-- recognisable by construction ("t_" + hex, LLD-03 §7.1); extension rows
-- ("e_") are untouched.
DELETE FROM ps_endpoints WHERE id LIKE 't\_%';
DELETE FROM ps_auths WHERE id LIKE 't\_%';
DELETE FROM ps_aors WHERE id LIKE 't\_%';

ALTER TABLE ps_endpoints DROP COLUMN IF EXISTS from_domain;
ALTER TABLE ps_endpoints DROP COLUMN IF EXISTS from_user;
ALTER TABLE ps_endpoints DROP COLUMN IF EXISTS outbound_auth;

DROP TABLE IF EXISTS carrier_routes;
DROP TABLE IF EXISTS carrier_trunks;
