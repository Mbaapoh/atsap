ALTER TABLE extensions DROP CONSTRAINT IF EXISTS extensions_outbound_class_valid;
ALTER TABLE extensions DROP COLUMN IF EXISTS outbound_class;
