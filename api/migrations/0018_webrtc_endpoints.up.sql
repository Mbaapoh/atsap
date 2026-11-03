-- console-browser-phone (D-81): a WEBRTC extension needs the engine's
-- browser-media endpoint fields. The realtime ps_endpoints table was
-- created with only the SIP columns this platform used before.
ALTER TABLE ps_endpoints
    ADD COLUMN webrtc VARCHAR(5),
    ADD COLUMN media_encryption VARCHAR(40),
    ADD COLUMN dtls_auto_generate_cert VARCHAR(5),
    ADD COLUMN ice_support VARCHAR(5),
    ADD COLUMN use_avpf VARCHAR(5),
    ADD COLUMN rtcp_mux VARCHAR(5);
