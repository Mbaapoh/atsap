-- telephony-core walking skeleton schema.
-- Subset of the full DDL in docs/hld/03-domain-model.md §5: only the
-- tables this LLD-01 slice needs. Later LLDs add their own tables (and,
-- per that section's closing note, their own tenant_isolation_* policies)
-- in their own migrations.

CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE',
    residency_zone VARCHAR(50) NOT NULL DEFAULT 'EU',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS calls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    direction VARCHAR(20) NOT NULL, -- INBOUND, OUTBOUND, INTERNAL
    state VARCHAR(50) NOT NULL,    -- Initiated, Screening, Routing, Presenting, Active, Degraded, Terminating, Terminated
    source_number VARCHAR(100) NOT NULL,
    dest_number VARCHAR(100) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    answered_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    route_id UUID,
    recording_consent BOOLEAN NOT NULL DEFAULT FALSE,
    termination_reason VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS call_participants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL,     -- CALLER, AGENT, IVR, QUEUE, SUPERVISOR, SPECIALIST, AI
    endpoint_uri VARCHAR(255) NOT NULL,
    state VARCHAR(50) NOT NULL,    -- Invited, Ringing, Connected, OnHold, Transferring, Disconnected
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    answered_at TIMESTAMPTZ,
    left_at TIMESTAMPTZ,
    billable_seconds INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channel_history (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    participant_id UUID NOT NULL REFERENCES call_participants(id) ON DELETE CASCADE,
    channel_ref BYTEA NOT NULL,    -- Encrypted Asterisk Channel ID string
    bridge_id VARCHAR(100) NOT NULL,
    node_id VARCHAR(50) NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS usage_seconds (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    participant_id UUID NOT NULL REFERENCES call_participants(id) ON DELETE CASCADE,
    call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    second_ts TIMESTAMPTZ NOT NULL,
    direction VARCHAR(20) NOT NULL,
    carrier_id UUID,
    cost_micros BIGINT NOT NULL DEFAULT 0,
    revenue_micros BIGINT NOT NULL DEFAULT 0,
    quality_mos NUMERIC(3, 2) NOT NULL DEFAULT 4.50,
    quality_jitter NUMERIC(6, 2) NOT NULL DEFAULT 0.0,
    quality_loss NUMERIC(5, 2) NOT NULL DEFAULT 0.0,
    ai_streamed BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (participant_id, second_ts)
);

CREATE TABLE IF NOT EXISTS outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    event_type VARCHAR(100) NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

-- Row-Level Security: every table above except tenants itself (the root
-- of tenancy, has no tenant_id column) is tenant-scoped and fails closed
-- without app.tenant_id set (docs/hld/01-architecture.md §4).
--
-- FORCE (not just ENABLE) is required: the app connects as atsapbx_app,
-- which OWNS every table it migrates, and PostgreSQL exempts a table's
-- owner from its own RLS policies unless FORCE ROW LEVEL SECURITY is
-- also set. Without FORCE here, RLS silently does nothing for the app's
-- own connection — verified by TestRLSIsolation failing without it.
ALTER TABLE calls ENABLE ROW LEVEL SECURITY;
ALTER TABLE calls FORCE ROW LEVEL SECURITY;
ALTER TABLE call_participants ENABLE ROW LEVEL SECURITY;
ALTER TABLE call_participants FORCE ROW LEVEL SECURITY;
ALTER TABLE channel_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE channel_history FORCE ROW LEVEL SECURITY;
ALTER TABLE usage_seconds ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_seconds FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_calls ON calls
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_participants ON call_participants
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_channel_history ON channel_history
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_usage ON usage_seconds
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation_outbox ON outbox
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Outbox worker exception (docs/hld/03-domain-model.md §5, design.md
-- "Outbox worker runs as a narrow BYPASSRLS platform role"): the
-- atsap_outbox_worker role itself is created with BYPASSRLS by the
-- Postgres bootstrap script (deploy/postgres/init/02-atsapbx.sql), since
-- granting BYPASSRLS requires superuser and this migration runs as the
-- ordinary atsapbx_app owner role. This migration only grants the narrow
-- object-level privilege on the one table it needs, which the table
-- owner (atsapbx_app) can do without any elevated privilege.
GRANT SELECT, UPDATE ON outbox TO atsap_outbox_worker;
