-- Rows longer than 80 characters cannot go back; they are truncated to
-- nothing rather than to a partial (and wrong) match, and reconcile then
-- reports them.
UPDATE ps_endpoint_id_ips SET "match" = NULL WHERE length("match") > 80;
ALTER TABLE ps_endpoint_id_ips ALTER COLUMN "match" TYPE VARCHAR(80);
