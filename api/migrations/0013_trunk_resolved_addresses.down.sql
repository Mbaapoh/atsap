-- Identify rows already written as addresses keep recognising their
-- carriers under the previous version, whose reconcile reports them as
-- divergent and can restore the names on request.
ALTER TABLE carrier_trunks
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS resolved_addresses;
