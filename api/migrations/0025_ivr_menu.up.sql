-- IVR menu (D-90): a prompt and up to ten digit branches, plus no-input
-- and invalid-input fallbacks. Revisions remain immutable.
ALTER TABLE ivr_flow_versions ADD COLUMN menu JSONB NOT NULL DEFAULT '{}';
ALTER TABLE ivr_flow_versions ADD COLUMN menu_timeout_seconds INT NOT NULL DEFAULT 5;
ALTER TABLE ivr_flow_versions ADD COLUMN no_input_extension_id UUID;
ALTER TABLE ivr_flow_versions ADD COLUMN invalid_extension_id UUID;
