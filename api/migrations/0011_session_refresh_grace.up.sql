-- identity-refresh-grace (D-62 amendment): when a session's previous
-- refresh secret was rotated away, the start of the 30-second window in
-- which presenting it again is a retry rather than a replay.
--
-- Nullable, with no backfill: a session last rotated before this column
-- existed has no window, so its first reuse still revokes it, exactly as
-- before. Every rotation from now on sets it.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS previous_rotated_at TIMESTAMPTZ;
