-- pbx-core bounded context, first slice (LLD-03; openspec change
-- pbx-extensions-projection). Two kinds of table live here and they are
-- governed by opposite rules, so the split is stated up front:
--
--   1. `extensions` is a DOMAIN table. It carries tenant_id, has RLS
--      enabled AND forced, and is the source of truth.
--   2. `ps_endpoints` / `ps_auths` / `ps_aors` are the ACL's PROJECTION
--      of that truth into shapes Asterisk owns (D-47). They carry no
--      tenant_id and no RLS, deliberately — see the comment above them.

CREATE TABLE IF NOT EXISTS extensions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    extension_number VARCHAR(20) NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    department_id UUID,
    -- Generated, and deliberately NOT derived from extension_number:
    -- knowing a tenant numbers its extensions 1000-1050 must not tell an
    -- attacker what to present as a username.
    auth_username VARCHAR(100) NOT NULL,
    -- MD5 HA1 — MD5(auth_username:realm:secret) — NOT Argon2id, and
    -- never the plaintext. SIP digest authentication requires the server
    -- to hold the plaintext or the HA1; a one-way slow hash cannot
    -- answer a digest challenge, so the `password_hash` name this column
    -- carries in an earlier draft of docs/hld/03-domain-model.md §5 was
    -- describing something that cannot be built. Renamed here, and the
    -- HLD corrected in the same change (LLD-03 §7.3).
    secret_digest VARCHAR(32) NOT NULL,
    device_type VARCHAR(50) NOT NULL DEFAULT 'WEBRTC',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Extension numbers are tenant-local: two tenants may each have a
    -- 1000 and they are unrelated extensions with unrelated credentials.
    UNIQUE(tenant_id, extension_number)
);

ALTER TABLE extensions ENABLE ROW LEVEL SECURITY;
ALTER TABLE extensions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_extensions ON extensions
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------
-- ACL projection tables (D-47). Asterisk reads these directly through
-- PJSIP Realtime; nothing else in the system may read or write them
-- outside internal/pbx/acl/asterisk.
--
-- NO tenant_id and NO row-level security, deliberately. Asterisk
-- connects as its own database role and cannot set app.tenant_id, so an
-- RLS policy would hide every row from the one reader that needs them.
-- Isolation here is BY CONSTRUCTION instead: every id is derived from a
-- UUID ("e_" + hex), so two tenants' extension 1000 can never collide.
-- The enforcement is the grant at the bottom of this file, not a policy.
-- This is the same shape of deliberate exception as licensing_state's
-- missing tenant_id (D-24 seam 1), and it is asserted by test so it
-- cannot be mistaken later for an oversight.
--
-- Column sets are the subset of Asterisk's realtime schema this change
-- writes. Asterisk tolerates a narrower table than its full schema
-- (res_pgsql `requirements=warn`); columns are added when a feature
-- needs them, never speculatively.
-- ---------------------------------------------------------------------

-- `mailboxes` is not written by us. Asterisk queries it at every boot
-- (SELECT * FROM ps_endpoints WHERE mailboxes != '' ORDER BY mailboxes,
-- for MWI) and errors without it. The schema must cover what the engine
-- QUERIES, not only what we populate.
CREATE TABLE IF NOT EXISTS ps_endpoints (
    id VARCHAR(255) PRIMARY KEY,
    transport VARCHAR(40),
    aors VARCHAR(255),
    auth VARCHAR(255),
    context VARCHAR(40),
    disallow VARCHAR(200),
    allow VARCHAR(200),
    direct_media VARCHAR(5),
    mailboxes VARCHAR(80)
);

CREATE TABLE IF NOT EXISTS ps_auths (
    id VARCHAR(255) PRIMARY KEY,
    auth_type VARCHAR(20),
    username VARCHAR(255),
    -- Always NULL. Present because Asterisk queries it; never written,
    -- and asserted empty by test. The credential lives in md5_cred.
    password VARCHAR(255),
    md5_cred VARCHAR(32),
    realm VARCHAR(255)
);

CREATE TABLE IF NOT EXISTS ps_aors (
    id VARCHAR(255) PRIMARY KEY,
    max_contacts INTEGER,
    remove_existing VARCHAR(5),
    qualify_frequency INTEGER
);

-- ps_contacts is the ONE table Asterisk WRITES. A device registration
-- creates a contact row; we never insert one. Two consequences, both
-- learned by testing rather than reading:
--
--   1. The column set must be COMPLETE, not the subset we care about.
--      Asterisk's INSERT names all sixteen columns, and a missing one
--      fails the write while the REGISTER still returns 200 OK — so the
--      device believes it is registered, no contact binds, and nothing
--      can call it. Silent, and it would ship.
--   2. asterisk_engine needs INSERT/UPDATE/DELETE here, unlike the three
--      read-only tables above.
--
-- Mapping it also makes registration status a plain database read, which
-- is what the console badge needs, and delivers the multi-node contact
-- visibility D-08 wanted, earlier and for free.
CREATE TABLE IF NOT EXISTS ps_contacts (
    id VARCHAR(255) PRIMARY KEY,
    uri VARCHAR(511),
    expiration_time BIGINT,
    qualify_frequency INTEGER,
    outbound_proxy VARCHAR(40),
    path TEXT,
    user_agent VARCHAR(255),
    endpoint VARCHAR(255),
    reg_server VARCHAR(255),
    authenticate_qualify VARCHAR(5),
    via_addr VARCHAR(40),
    via_port INTEGER,
    call_id VARCHAR(255),
    prune_on_boot VARCHAR(5),
    qualify_timeout NUMERIC(10,3),
    qualify_2xx_only VARCHAR(5)
);

CREATE INDEX IF NOT EXISTS ps_contacts_endpoint_idx ON ps_contacts (endpoint);

-- ---------------------------------------------------------------------
-- The engine's database identity.
--
-- The asterisk_engine role itself is created by the Postgres bootstrap
-- script (deploy/postgres/init/02-atsapbx.sql), exactly as
-- atsap_outbox_worker is and for the same reason: atsapbx_app is
-- deliberately NOCREATEROLE, so this migration cannot create a login
-- role. It grants only the narrow object-level privileges the table
-- owner can grant without any elevated privilege.
-- ---------------------------------------------------------------------

GRANT USAGE ON SCHEMA public TO asterisk_engine;

-- Four tables and nothing else, ever. This grant is the isolation control
-- that replaces RLS for the projection, so it is asserted directly by a
-- test that connects as this role and proves reads of extensions,
-- principals and calls all fail.
--
-- The asymmetry is deliberate: three tables are ours to write and the
-- engine only reads them, while ps_contacts is the engine's own — it
-- creates a row per registration — so it needs write access there and
-- nowhere else.
GRANT SELECT ON ps_endpoints TO asterisk_engine;
GRANT SELECT ON ps_auths TO asterisk_engine;
GRANT SELECT ON ps_aors TO asterisk_engine;
GRANT SELECT, INSERT, UPDATE, DELETE ON ps_contacts TO asterisk_engine;
