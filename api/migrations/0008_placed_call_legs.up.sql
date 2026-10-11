-- Calls placed through PbxService.PlaceCall (pbx-call-placement design D4).
--
-- One row per placed call: which extension placed it, which route matched
-- and which trunk carried it (AC-02.3/02.6 — the matched rule is on
-- record), and whether it was an emergency call. It is also how a trunk's
-- channel_limit is counted: a row with released_at NULL occupies a
-- channel on trunk_id until the call is observed to have ended.
--
-- A DOMAIN table: tenant_id, RLS enabled AND forced, policy — the pattern
-- every pbx table follows. The call itself is telephony-core's; this
-- records only pbx-core's part in it, and holds no engine identifier.
CREATE TABLE IF NOT EXISTS placed_call_legs (
    -- The leg's own id exists before the call does: a trunk channel is
    -- reserved under the trunk's lock BEFORE origination, and only
    -- telephony-core's InitiateCall mints the call id. call_id is
    -- attached once origination returns; a leg left without one (a crash
    -- between reserve and originate) is released by the next count.
    id UUID PRIMARY KEY,
    call_id UUID UNIQUE,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    extension_id UUID NOT NULL,
    -- NULL for an internal call, which uses no trunk.
    trunk_id UUID,
    route_id UUID,
    destination VARCHAR(33) NOT NULL,
    emergency BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ
);

-- The channel count: unreleased legs on one trunk.
CREATE INDEX IF NOT EXISTS placed_call_legs_live_idx
    ON placed_call_legs (tenant_id, trunk_id) WHERE released_at IS NULL;

ALTER TABLE placed_call_legs ENABLE ROW LEVEL SECURITY;
ALTER TABLE placed_call_legs FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_placed_call_legs ON placed_call_legs
    FOR ALL USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
