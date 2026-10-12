-- Back to revoke-on-any-reuse: without the column there is no grace window.
ALTER TABLE sessions DROP COLUMN IF EXISTS previous_rotated_at;
