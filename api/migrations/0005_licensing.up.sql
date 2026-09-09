-- licensing bounded context (LLD-08; openspec change
-- licensing-capacity-grace). One table, and it breaks two rules the rest
-- of this schema keeps. Both breaks are deliberate and both are asserted
-- by test, so neither can later be mistaken for an oversight.
--
--   1. NO tenant_id and NO row-level security. Licensing is
--      INSTALLATION-scoped, not tenant-scoped (T-1, D-24 seam 1's one
--      exception): one signed licence governs one installed instance
--      however many tenants it serves. A tenant_id here would imply a
--      licence per tenant, which is not the product.
--
--   2. The readable columns are NOT the source of truth. signed_payload
--      and signature are (D-53). See below.
--
-- Note the contrast with 0004's ps_* tables, which also carry no
-- tenant_id: there the reason is that Asterisk connects as its own role
-- and cannot set app.tenant_id. Here the reason is that the data is not
-- tenant-shaped at all. Same shape of exception, different cause.

CREATE TABLE IF NOT EXISTS licensing_state (
    -- One row per installation. The PK is the instance identity the
    -- licence is issued against, so a second row is a bug rather than a
    -- second licence.
    instance_id UUID PRIMARY KEY,

    -- THE ENTITLEMENT OF RECORD (D-53). Every decision about what this
    -- installation may do is made by verifying this payload against the
    -- keys embedded in the binary and reading the result.
    --
    -- The platform runs on the partner's hardware, so they hold this
    -- database. If the columns below were trusted, one UPDATE would
    -- grant any capacity, any edition and any tenant count — without
    -- forging a signature, patching a binary, or defeating the hardware
    -- fingerprint. Verifying only at the moment a key is applied
    -- protects the DELIVERY of a licence and nothing about how it is
    -- KEPT.
    --
    -- Verification happens on load and on apply, never during call setup
    -- (AC-06.3).
    signed_payload BYTEA NOT NULL,
    signature BYTEA NOT NULL,

    -- Everything below is a DENORMALISED CACHE of what signed_payload
    -- says, for display, support and troubleshooting. Nothing reads
    -- these to decide what the installation may do. Editing them changes
    -- no entitlement decision, and a test asserts exactly that
    -- (LLD-08 DoD 10, AC-06.16).
    fingerprint JSONB NOT NULL,
    edition VARCHAR(50) NOT NULL,
    capacity INTEGER NOT NULL,
    max_tenants INTEGER NOT NULL,      -- 1 = single-tenant, 0 = unlimited (D-51)
    max_extensions INTEGER NOT NULL,   -- 0 = unlimited (D-49)

    -- Entitlement confirmation state. last_confirmed_at drives the 7-day
    -- offline grace (D-14): the licence state is a pure function of the
    -- time elapsed since it, never of process uptime, so a restart
    -- cannot reset the grace period.
    entitlement_status VARCHAR(50) NOT NULL DEFAULT 'VALID',
    last_confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    grace_started_at TIMESTAMPTZ
);

-- No row is seeded, deliberately (D-52). An installation with no row is
-- in Setup: administration works, there is no call path, and it is NOT
-- the degradation floor. A seeded row would be an unsigned entitlement
-- at rest, which is the precise thing signed_payload exists to prevent.

-- asterisk_engine gets nothing here. It reads ps_* to place calls and
-- has no business knowing what the installation is licensed for; the
-- absence of a grant is the enforcement. Stated rather than left
-- implicit, because 0004 grants that role several tables and the
-- omission should read as a decision.
