## ADDED Requirements

### Requirement: A WEBRTC extension is projected for browser media

The system SHALL project an extension whose device type is `WEBRTC` as a
browser endpoint: on the installation's configured WebRTC transport, with
DTLS-encrypted media, ICE, AVPF and RTCP multiplexing, and the narrow-band codecs. An extension whose device type is `SIP`
SHALL keep its existing transport and media projection. The engine-facing
identifier of either remains derived from the extension and never appears
in a public response.

#### Scenario: A WEBRTC extension uses the WebRTC transport

- **WHEN** a WEBRTC extension is created or updated and projected
- **THEN** its engine endpoint uses the configured WebRTC transport, not the SIP transport

#### Scenario: Browser media is encrypted

- **WHEN** a WEBRTC extension is projected
- **THEN** its endpoint enables DTLS media encryption, ICE, AVPF and RTCP multiplexing, and allows the narrow-band codecs

#### Scenario: A SIP extension is unchanged

- **WHEN** a SIP extension is projected
- **THEN** its endpoint uses the configured SIP transport and does not enable browser media settings

#### Scenario: The transport is configuration, not a guess

- **WHEN** the WebRTC transport is not defined in the engine configuration
- **THEN** the projection still names the configured transport; startup/reconcile reports the divergence rather than silently falling back to the SIP transport
