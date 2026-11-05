DROP INDEX IF EXISTS call_participants_principal;
DROP INDEX IF EXISTS call_participants_extension;
ALTER TABLE call_participants DROP COLUMN IF EXISTS principal_id;
ALTER TABLE call_participants DROP COLUMN IF EXISTS extension_id;
