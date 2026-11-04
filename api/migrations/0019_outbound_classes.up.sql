-- Outbound destination classes (LLD-03 §10.2): a compromised extension
-- cannot dial beyond the class an administrator gave it. Existing
-- extensions become NATIONAL, the safe default, not INTERNATIONAL.
ALTER TABLE extensions
    ADD COLUMN outbound_class VARCHAR(20) NOT NULL DEFAULT 'NATIONAL';

ALTER TABLE extensions
    ADD CONSTRAINT extensions_outbound_class_valid
        CHECK (outbound_class IN ('INTERNAL', 'NATIONAL', 'INTERNATIONAL'));
