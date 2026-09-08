-- Rolls the database back to the identity schema.
--
-- REVOKE before DROP: dropping the tables removes the grants on them,
-- but USAGE on the schema survives, and a role holding any privilege in
-- this database blocks nothing here — the role itself is NOT dropped.
-- It is created by the Postgres bootstrap script
-- (deploy/postgres/init/02-atsapbx.sql), so it is not this migration's
-- to remove, the same way 0001 revokes from atsap_outbox_worker without
-- dropping it.
REVOKE ALL ON ps_contacts FROM asterisk_engine;
REVOKE SELECT ON ps_aors FROM asterisk_engine;
REVOKE SELECT ON ps_auths FROM asterisk_engine;
REVOKE SELECT ON ps_endpoints FROM asterisk_engine;
REVOKE USAGE ON SCHEMA public FROM asterisk_engine;

DROP TABLE IF EXISTS ps_contacts;
DROP TABLE IF EXISTS ps_aors;
DROP TABLE IF EXISTS ps_auths;
DROP TABLE IF EXISTS ps_endpoints;
DROP TABLE IF EXISTS extensions;
