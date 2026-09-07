-- Rolls the database back to the telephony-core schema. The dev tenant
-- row the retired 0002 seed created is not recreated: provisioning is
-- identity's job, and 0002 is a tombstone.
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS principals;
