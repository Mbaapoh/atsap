-- Participant attribution (D-65, D-87): the extension a participant used
-- and the membership that owned it at the time. Reporting reads these;
-- engine endpoints stay out of every public response.
ALTER TABLE call_participants ADD COLUMN extension_id UUID;
ALTER TABLE call_participants ADD COLUMN principal_id UUID;
CREATE INDEX IF NOT EXISTS call_participants_extension ON call_participants (tenant_id, extension_id);
CREATE INDEX IF NOT EXISTS call_participants_principal ON call_participants (tenant_id, principal_id);
