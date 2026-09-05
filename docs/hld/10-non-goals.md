<!-- OpenSpec: TRD-HLD-11 -->
# Non-Goals, Scope Boundaries & Architectural Constraints

Establishing strict boundaries is vital to prevent scope creep and ensure timely delivery of the R1.0 sellable core (BRD §7, PRD §5, DECISIONS §Commercial & Architecture).

---

## 1. Release Roadmap Boundaries

Features are phased across releases to de-risk engineering and prove the media core before adding complex orchestration (DECISIONS D-26, D-27):

```text
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ R1.0: Sellable Core (IN SCOPE NOW)                                                              │
│ - WebRTC browser softphone & desk-phone HTTPS auto-provisioning (EPIC-03)                       │
│ - BYOT SIP carrier connectivity & Least-Cost Routing (LCR) with auto-failover (EPIC-02)         │
│ - Multi-tenant isolation with PostgreSQL RLS & scoped audit logs (EPIC-01)                       │
│ - Visual IVR flow-as-data engine with pure fold execution (EPIC-03)                              │
│ - Recording governance, PCI-DSS pause/resume & T-10 envelope encryption (EPIC-07)               │
│ - T-11 per-participant RTCP-XR quality scoring on 100% of calls (EPIC-07)                       │
│ - API-first ConnectRPC endpoints & durable webhook delivery (EPIC-05)                           │
│ - Ed25519 cryptographic licensing & weighted hardware fingerprinting (EPIC-06)                 │
│ - Optional out-of-band AI media pipeline with zero lock-in (EPIC-04)                            │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
                                                │
                                                ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ R1.1: Enterprise Edge (DEFERRED)                                                                │
│ - Native iOS / Android mobile applications with push notifications (EPIC-11)                    │
│ - Dynamic emergency calling (E911 / 112) with remote location management (EPIC-12)              │
│ - Inbound/outbound T.38 Fax to PDF & email delivery (EPIC-13)                                   │
│ - Bulk migration tooling from legacy PBX systems (EPIC-13)                                      │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
                                                │
                                                ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ R2: Contact Centre & CRM Integration (DEFERRED)                                                 │
│ - Power dialler loop followed by predictive pacing algorithm (D-27, EPIC-14)                     │
│ - Answering-Machine Detection (AMD targeting <= 3.5s, D-09, EPIC-14)                            │
│ - Automated FTC/FCC outbound compliance (DNC lists, calling hours, abandonment caps, EPIC-15)    │
│ - Supervisor workspace (live listen, whisper coaching, barge-in, wallboards, EPIC-16)           │
│ - Bi-directional CRM/ERP connectors (Salesforce, HubSpot, Zendesk, EPIC-17)                    │
│ - Live agent assist & automated after-call work (ACW) via optional AI (EPIC-18)                │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
                                                │
                                                ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ R3: Specialist Dispatch & OPI Platform (DEFERRED)                                               │
│ - Language pair, dialect, and subject-matter specialist matching (EPIC-19)                       │
│ - Sub-15-second simultaneous broadcast dispatch (first-accept-wins, D-10, EPIC-19)              │
│ - Department PIN & automated budget tracking (EPIC-20)                                          │
│ - Per-second margin ledger calculating revenue, cost, and profit per call (EPIC-21)             │
│ - Turn-key rating and customer invoicing module (EPIC-22)                                       │
│ - Data residency pinning & automated spend caps (EPIC-23)                                       │
│ - Dual-channel separated recording with real-time translation overlay (EPIC-23)                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 1.1 R1.0 Emergency Calling Boundary & Disclosure (PRD §5.2, INV-02)

Dynamic E911 location tracking for remote browser users is explicitly deferred to R1.1. In R1.0:
- Emergency calls (`911`, `112`, `999`) bypass all licensing, capacity, and spend checks (INV-01) and route out via the primary trunk.
- Where dynamic location cannot be guaranteed, the softphone UI displays a prominent warning informing users to dial emergency services from a physical mobile or landline device (INV-02).

---

## 2. Explicit Architectural & Technical Non-Goals

The following patterns and technologies were evaluated and explicitly rejected in the Decision Log:

| Rejected Technical Approach | Architectural Decision | Rationale / Ground Truth |
|---|---|---|
| **Custom SIP / RTP Media Stack** | Rejected (DECISIONS D-15) | Writing telephony from first principles requires years of protocol engineering. Asterisk 22.x LTS provides certified carrier compatibility and WebRTC support. |
| **Microservices Architecture** | Rejected (DECISIONS D-20) | Microservices introduce distributed transaction complexity, network latency, and operational overhead that harm partner self-hosting. A modular monolith provides clean seams without the penalty. |
| **Mandatory Kubernetes** | Rejected (PRD §5.1, DECISIONS D-01) | Partner deployments must be installable on a single bare-metal or cloud VM using Docker Compose or Ansible. Kubernetes is not required for the 500-channel launch target. |
| **Event Sourcing & Full CQRS** | Rejected (DECISIONS D-31) | Scaled-down DDD retains domain aggregates and outbox events, but drops event sourcing and per-entity repositories to keep codebase maintainable by small teams. |
| **5-Nines (99.999%) SLA Guarantee** | Rejected (DECISIONS D-07) | Five nines allows only 26 seconds of downtime monthly end-to-end. In partner-hosted BYOT environments with variable carriers, this claim creates legal liability. Target is 99.95%. |
| **Browser-to-PSTN End-to-End Encryption** | Rejected (DECISIONS §Known limitations) | Technically impossible when bridging WebRTC to traditional telephone networks. AtsaPBX must terminate encryption at Asterisk to perform call mixing, recording, and quality scoring. |
| **Marketplace AI Agent Skills** | Rejected (DECISIONS D-30) | Bulk-installing unreviewed external prompt/skill scripts introduces supply-chain vulnerabilities. AtsaPBX ships vetted internal adapters only. |
| **Predictive Before Power Dialling** | Rejected (DECISIONS D-27) | Power dialling proves telephony plumbing first; predictive pacing mathematics are layered on top in R2. |
| **Sequential Specialist Calling** | Rejected (DECISIONS D-10) | Sequential ring cycles take 15–20s per candidate, making sub-15s dispatch impossible. Simultaneous broadcast with first-accept-wins is used instead. |

---

## 3. Scope Governance & Architecture Verification

1. **Architecture Review Gate:** Any pull request introducing a new microservice, an external telephony engine, a Kubernetes dependency, or an unapproved external agent skill is rejected during architecture review.
2. **Feature Boundary Enforcement:** Automated tests verify that R2/R3 features (predictive dialler, live translation overlay, turnkey invoicing) do not leak into R1.0 release builds.


