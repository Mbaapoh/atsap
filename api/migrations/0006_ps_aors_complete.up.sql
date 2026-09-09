-- ps_aors column completeness: the same trap as ps_contacts (0004), one
-- migration later. Asterisk polls AoRs itself on a ~25s cadence with
--   SELECT * FROM ps_aors WHERE contact != '' ORDER BY contact
-- (its scan for AoRs carrying permanent/static contacts), and our
-- four-column subset has no `contact` — so every poll fails with
-- `column "contact" does not exist` while nothing user-visible breaks.
-- The column set below is Asterisk's own alembic set for ps_aors minus
-- the four 0004 already created (id, max_contacts, remove_existing,
-- qualify_frequency). All nullable: NULL means unset and takes
-- Asterisk's defaults, and the projector's explicit column lists are
-- untouched, so nothing the platform writes changes shape. In
-- particular `contact` stays NULL — we never use permanent contacts —
-- which makes the failing scan return the empty set it was always
-- meant to return here.

ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS contact VARCHAR(255);
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS default_expiration INTEGER;
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS minimum_expiration INTEGER;
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS maximum_expiration INTEGER;
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS mailboxes VARCHAR(80);
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS authenticate_qualify VARCHAR(5);
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS outbound_proxy VARCHAR(40);
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS support_path VARCHAR(5);
ALTER TABLE ps_aors ADD COLUMN IF NOT EXISTS qualify_timeout NUMERIC(10,3);
