# Open owner decisions and spikes from the D-98 LLD pass

**Raised:** 2026-11-07 by the writer agents that brought the LLDs to the
flow Definition of Ready (D-98). Each item has a recommended default.
Until the owner rules, **the recommended default is what the LLD
assumes**, and nothing irreversible depends on it.

Items in **bold** in the "Needs the owner" column are a new dependency,
a security trade-off or a legal or commercial choice. AGENTS.md requires
the owner to approve those explicitly. The rest are product defaults the
owner may simply accept.

## 1. Need the owner's explicit approval

| # | Decision | Recommended | Where | Needs the owner |
|---|---|---|---|---|
| A1 | New dependency: S3 client for recordings and backups | `minio-go/v7` (works with MinIO and S3) | LLD-09 §11, LLD-12 OD-6 | **Dependency** |
| A2 | New dependency: phone-number parsing and line type | Adopt a libphonenumber port for Go | LLD-06 | **Dependency** |
| A3 | New dependency: SFTP backup target | Defer to R1.1 (`pkg/sftp`) | LLD-12 | **Dependency** |
| A4 | SIP password of a phone-bound extension stored encrypted (so it can be provisioned) | Yes, with an envelope key; changes LLD-03 §7.3 | LLD-12 OD-2 | **Security trade-off** |
| A5 | Trust-on-first-use claim window for phones provisioned only by DHCP | Off by default; when on, requires an IP range | LLD-12 OD-3 | **Security trade-off** |
| A6 | Object-store encryption keys | Platform-managed key per installation; tenant keys later | LLD-09 §11 | **Security trade-off** |
| A7 | Recording and AI consent: does the AI tap need the same consent as recording? | Yes | LLD-09, LLD-10 | **Legal** |
| A8 | Jurisdiction seeds for recording consent | US and CA seeded; unknown jurisdictions treated as all-party consent | LLD-09 | **Legal** |
| A9 | Outbound consent model (TCPA express written consent for mobiles) | The US profile enforces it for mobile numbers | LLD-04, LLD-06 | **Legal** |
| A10 | Compliance profiles shipped in R1.0 | US plus a custom profile; UK values only after counsel confirms them | LLD-04 | **Legal** |
| A11 | Caller-ID ownership evidence | Carrier-delivered numbers on the tenant's word; caller-ID-only numbers need a callback code | LLD-03 OD-2 | **Legal / BR-06** |
| A12 | Two tenants authorised for the same caller-ID-only number | Allowed | LLD-03 OD-6 | **Legal** |
| A13 | Licence counting: consult legs and extra ad-hoc parties reserve capacity; dialler agent legs do not | Yes / no | LLD-01 OD-CC-1, LLD-06 | **Commercial** |
| A14 | Provisioning is not a paid module | Included in every licensed edition | LLD-12 OD-10 | **Commercial** |
| A15 | New role presets: CAMPAIGN_MANAGER, COMPLIANCE_OFFICER, AUDITOR | Add them (LLD-02 change) | LLD-04, LLD-06, LLD-09 | **Access model** |
| A16 | Supported desk-phone models for R1.0 | Yealink T4U/T5x, Poly VVX/Edge E, Grandstream GRP26xx | LLD-12 OD-1 | **Scope** |
| A17 | PRD additions: AC-03.18 (queue position/EWT) and a minimal R1.0 manual inbound disposition (US-03.18/AC-03.19) | Add both | LLD-03 §3.3.6 OD-7, OD-8 | **Scope (PRD)** |
| A18 | Paging/intercom | Out of R1.0 | LLD-03 OD-1 | **Scope** |
| A19 | Voicemail drop | One-click agent drop in R1.0; AMD and automatic drop in R2 | LLD-06 | **Scope** |
| A20 | LLD numbering: LLD-12 covers pbx-core devices plus lifecycle tooling, against D-48's "one LLD per context" | Amend D-48 to allow a cross-cutting operations LLD | LLD-12 OD-9 | **Process** |

## 2. Product defaults (accept unless the owner objects)

| Area | Default recommended by the LLD |
|---|---|
| Call control (LLD-01 §24) | No forced maximum hold time; an unanswered blind transfer follows the target's own rules, with ring-back after 15 s as a tenant option; in-call DTMF feature codes off |
| PBX features (LLD-03) | Voicemail email carries a link, not the audio; a forwarded external call shows the tenant's number, not the original caller's; a hot-desk phone's own number does not ring while someone is logged in |
| Contact centre (LLD-03 §3.3.6) | Supervisor monitoring on by default, agent beep off; queue callback calls the agent first, then the caller |
| Outbound (LLD-06, LLD-04) | Power ratio default 1.0 (reserved mode, cannot abandon); local presence available but off; manual dials from a dialler session get the full check, ordinary extension calls check DNC and opt-out only; opt-out removal by a compliance officer only; abandonment window is a rolling 30 days; attempt records kept 760 days; national DNC must be under 31 days old |
| Records (LLD-09) | Tenant recording retention default 90 days; CDRs kept 24 months; CDR chain anchors sent to partners, no external timestamp authority; phone numbers included in webhook payloads with a redaction option |
| AI (LLD-10) | Text redacted before the provider, audio never redacted; AI is an entitlement module; first provider adapters chosen when the AI spike runs |
| Platform (LLD-05, LLD-12) | Production NATS persists to a volume; upgrades run from the command line only (no Docker socket for the app); offsite backup to S3-compatible storage; backups kept at most 35 days; the operator uploads firmware; vendor redirect services use the partner's own accounts; never include call audio in a diagnostic bundle |

## 3. Spikes to run before the dependent changes are proposed

| Spike | Question | Gates |
|---|---|---|
| SPIKE-CC-1 (= HLD-21 §10.1) | Can phone-initiated transfers be handed to the app (`ari-only`, `ChannelTransfer`)? | Phone transfers, park by transfer |
| SPIKE-CC-2 | Does the held party hear MOH when the phone itself holds? | Hold |
| SPIKE-CC-3 / Q1 | Whisper direction, proven by a recording | Supervision (AC-16.1) |
| SPIKE-CC-4 | Hang-up cause for "answered elsewhere" | Pickup, ring groups |
| SPIKE-P1 | Busy lamps on desk phones without generated dialplan (`Stasis:` device states) | BLF, park lamps; console presence is the fallback |
| SPIKE-M1 | Loading tenant MOH classes from the database | Hold music |
| SPIKE-V1 | Playing voicemail audio to a phone | Voicemail |
| Q2, Q3 | Gap in the queue announcement; originate idempotency (409 on a reused id) | Queues |
| D1–D4 | SIP response code on hang-up via ARI; early media to a nailed-up agent; AMD baseline (R2); answer-to-bridge under 300 ms | Dialler |
| R-S1–R-S3 | AMI RTCP events for PJSIP and WebRTC; live-recording `mute` writes silence; fetching large stored recordings | Quality, PCI pause, recording |
| AI-S1 (= HLD-21 §10.2) | `externalMedia` slin16 latency and stalls; AudioSocket viability | The AI build and the R1.0 AI gate |
| S-12.1–S-12.3 | Factory-certificate MAC binding per vendor; NOTIFY event strings; removing a revoked phone's registration | Provisioning |

## 4. Cross-LLD follow-ups still open

- LLD-02: add the new permissions (`call.control`, `call.control.any`,
  the routing-feature, agent, campaign and compliance permissions) and
  the role presets (A15). Also design the hook by which identity tells
  pbx-core that a membership was disabled.
- HLD-04: §10.1 should say that reporting also consumes pbx-core and
  ai-pipeline events; §8 should use the Standard Webhooks headers.
- HLD-07: RTCP-XR → RTCP SR/RR.
- LLD-08: confirm licence counting (A13).
- LLD-03 §7.3: `RotateForDevice`; deleting an extension that is on a
  phone is refused.
- The API image needs the `pg_dump`/`pg_restore` 16 client tools
  (LLD-12).
- `PRIOR-ART.md`: vendor-specific claims in LLD-12 (file names, NOTIFY
  strings) must be checked against vendor documentation before the
  change is proposed.
