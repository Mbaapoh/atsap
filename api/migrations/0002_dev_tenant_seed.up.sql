-- Fixed-UUID dev tenant. Applying this migration at all is gated by the
-- Go migration runner (internal/postgres), which only migrates to this
-- version when ATSAPBX_SEED_DEV_TENANT=true — never in production.
-- Deleted once LLD-02 lands real tenant provisioning.
INSERT INTO tenants (id, name, status, residency_zone)
VALUES ('00000000-0000-0000-0000-000000000001', 'Dev Tenant', 'ACTIVE', 'EU')
ON CONFLICT (id) DO NOTHING;
