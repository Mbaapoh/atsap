ALTER TABLE ps_endpoints
    DROP COLUMN IF EXISTS rtcp_mux,
    DROP COLUMN IF EXISTS use_avpf,
    DROP COLUMN IF EXISTS ice_support,
    DROP COLUMN IF EXISTS dtls_auto_generate_cert,
    DROP COLUMN IF EXISTS media_encryption,
    DROP COLUMN IF EXISTS webrtc;
