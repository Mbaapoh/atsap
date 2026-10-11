DROP TABLE IF EXISTS ps_endpoint_id_ips;
ALTER TABLE ps_endpoints DROP COLUMN IF EXISTS set_var;
DROP TABLE IF EXISTS emergency_settings;
DROP TABLE IF EXISTS emergency_numbers;
DROP TABLE IF EXISTS inbound_numbers;
ALTER TABLE extensions DROP CONSTRAINT IF EXISTS extensions_tenant_id_id_key;
