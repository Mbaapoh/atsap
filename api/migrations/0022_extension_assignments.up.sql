-- Phase B opens with extension ownership (D-65): a line belongs to at most
-- one membership. The link lives in pbx-core; identity is untouched.
CREATE TABLE IF NOT EXISTS extension_assignments (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension_id UUID NOT NULL,
    principal_id UUID NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, extension_id),
    FOREIGN KEY (extension_id) REFERENCES extensions(id) ON DELETE CASCADE
);
ALTER TABLE extension_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE extension_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_extension_assignments ON extension_assignments
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON extension_assignments TO atsapbx_app;
