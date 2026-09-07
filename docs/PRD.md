# AtsaPBX — Product Requirements Document

**Version 1.1 · Approved for Technical Design**
**Traces from:** BRD v1.0 · **Feeds:** High Level Design
**Owner:** Product Owner · **Audience:** Product, Design, Engineering, QA, Support, Delivery Partners

---

## 1. Purpose and structure

The BRD states why we are building and what the business must achieve. This document states **what the product does**, in behaviour a tester can pass or fail without asking a question.

Every epic traces to a BRD requirement. Every BRD requirement has an epic. The traceability matrix is §9, and anything appearing in a sprint that is not in it is scope creep.

This document contains no technology choices and no vendor names — those belong in the technical design. Where an implementation question arises, it is recorded in §16.2 rather than answered here.

**Status: approved for technical design**, subject to the readiness gate in §17.

---

## 2. Product principles

1. **The API is the product.** A capability that exists only in our interface is unfinished. Our console calls the same endpoints a partner would.
2. **The agent screen and the admin console are two different products.** One is optimised for speed under pressure; the other for clarity and safety. Never make one look like the other.
3. **No engineer required for a business change.** If a partner's customer must raise a ticket to change a menu, add a queue or adjust a route, the feature is incomplete.
4. **Asterisk is an implementation detail, never a user-facing one.** Every configuration is made in the console — extensions, call flows, IVR, auto-attendant, queues, trunks, routing, recording — exactly as VitalPBX and 3CX set the expectation. No customer edits a configuration file, opens a CLI, or needs to know Asterisk is there. No dialplan context, endpoint identifier, channel name or filename appears in any interface, API response, or error a user can see. The platform speaks in extensions, queues and call flows; the engine's vocabulary stays inside the engine.
5. **Never lie about state.** Stale data is visibly marked. A silently dead call is the worst outcome this product can produce.
6. **Compliance is enforced, never advised.** The platform prevents the unlawful call; it does not warn the agent and hope.
7. **Nothing we do to enforce a licence may harm a working phone system.** Degrade, never disable. Never touch an active call. Never touch an emergency call.

---

## 3. Users and their jobs

| User | Primary job | Where they spend their time |
|---|---|---|
| Partner engineer | Install, configure, upgrade, support | Admin console, command line, diagnostics |
| Partner developer | Integrate with the partner's own systems | API, webhooks, event streams |
| Tenant administrator | Configure the system for their business | Admin console |
| Employee | Make and take calls anywhere | Browser softphone, mobile app |
| Contact-centre agent | Handle the maximum number of good conversations | Agent workspace, one screen, all day |
| Supervisor | See the floor, coach, rescue escalations | Supervisor workspace, wallboards |
| Campaign manager | Build, run and tune outbound campaigns | Campaign console |
| Specialist / interpreter | Accept work, deliver it, be paid correctly | Phone keypad, mobile app |
| Finance (partner) | Bill their customers accurately | API, exports, reports |
| Support engineer | Diagnose a complaint quickly | Call detail, quality view, audit log |

---

## 4. Release plan

| Release | Theme | Definition of success | Priority key |
|---|---|---|---|
| **R1.0** | Sellable core | A partner installs, licenses and runs it as a business | **M** must — release blocks without it |
| **R1.1** | Enterprise edge | A regulated enterprise can adopt it | **S** should |
| **R2** | Contact centre and CRM | An outbound operation runs on it | **C** could |
| **R3** | Specialist and OPI platform | A language-services business runs on it | |

---

## 5. Scope alignment and boundaries

Aligned to BRD §7 (product scope by release) and BRD §11 (scope). Where this section and the BRD disagree, the BRD wins and this section is corrected.

### 5.1 In scope — R1.0

| Capability area | In scope | BRD reference |
|---|---|---|
| Everyday calling | Browser calling with no installation; hold, transfer, park, conference, presence, hot-desking, voicemail to email | §7.1 |
| Desk phones | Automatic configuration of supported handsets | §7.1 |
| Carrier connectivity | Multiple carriers, cheapest-route selection, automatic failover, per-carrier capacity limits | §7.1 |
| Tenancy | Complete isolation between customer organisations | §7.1 |
| Call handling | Multi-level menus, business hours, holidays, department routing, queues with basic distribution | §7.1 |
| Access and audit | Role-based permissions, append-only audit trail | §7.1 |
| Security | Fraud detection, rate limits, spend caps, minimal external exposure | §7.1 |
| Recording | Consent rules, announcements, retention, permanent deletion, access audit | §7.1 |
| Records | Immutable call records with per-participant detail and quality measurement | §7.1 |
| AI (optional) | Streaming to a partner-chosen provider; menu generation assistance | §7.1 |
| API | Every capability available through documented interfaces, with events and webhooks | §7.1 |
| Licensing | Capacity-based entitlement with graceful enforcement | §7.1 |
| Deployment | Installation, upgrade, rollback, backup, diagnostics on partner infrastructure | §7.1 |

### 5.2 Deferred — later releases, not R1.0

| Capability | Release | Why deferred |
|---|---|---|
| Mobile applications | R1.1 | Separate build and distribution effort; browser calling covers the launch case |
| Emergency calling with dynamic location | R1.1 | Requires a jurisdiction decision and an external location service. **Until delivered, emergency dialling is blocked and the limitation disclosed** — see §6, INV-02 |
| Fax delivery | R1.1 | Vertical-specific; commercially optional |
| Bulk migration tooling | R1.1 | Needed for displacement deals, not for a first deployment |
| Outbound dialling, detection, campaigns, compliance enforcement | R2 | A distinct product line with its own regulatory surface |
| Supervisor coaching tools, wallboards | R2 | Belongs with the contact-centre release |
| CRM and ERP connectors, embedded call controls | R2 | The API in R1.0 already permits partner-built integrations |
| Live assistance and automated after-call work | R2 | Depends on the R1.0 AI pipeline being proven |
| Specialist matching, rapid dispatch, budget tracking, per-second ledger | R3 | A distinct vertical product |
| Optional rating and invoicing module | R3 | Partners bill from API data in R1.0 and R2 |

### 5.3 Functional non-goals for R1.0

Stated so they can be declined quickly rather than debated repeatedly. **None of these is a "maybe if there is time."**

| Non-goal | The answer to give |
|---|---|
| Video calling, meetings, screen sharing | Not in R1.0–R3. Out of product scope entirely. |
| Messaging channels — text, chat, social | Deferred beyond R3 |
| Internal team chat | Deferred beyond R3 |
| Desktop application | Browser calling covers it; revisit only if a paying partner requires it |
| Analogue device management | Out of product scope |
| Payment processing | Partners collect from their own customers |
| Workforce scheduling and forecasting | Out of product scope |
| High availability across sites | R1.0 supports a single deployment; resilience is a later capability |
| Self-service sign-up by end customers | Partners onboard their own customers |
| Custom reporting builder | Fixed reports plus data export in R1.0 |
| White-label branding beyond name, logo and colours | Deferred |

**Scope control rule.** Any request outside §5.1 is recorded in the backlog with the requesting partner named, so demand is measured rather than argued. Only the Product Owner moves an item between releases, and only with a written note on the cost in time.

---

## 6. Product invariants and non-negotiable rules

These hold in every release, every configuration and every failure mode. A feature that requires one of these to be relaxed does not ship. They are the first thing a reviewer checks and the first thing a test suite proves.

| # | Invariant | Why it is absolute |
|---|---|---|
| **INV-01** | **Emergency calls bypass every check** — licence state, capacity limit, spend cap, suspension, menu, routing rule and AI feature. This path can never be gated by any commercial or operational condition. | A phone system that blocks an emergency call over a billing state is a liability no revenue justifies |
| **INV-02** | Where emergency obligations cannot be met in a jurisdiction, emergency dialling is **blocked and disclosed** in the interface and in the partner agreement. Silent mis-routing is never acceptable. | An unclear failure here can cause real harm |
| **INV-03** | **An active call is never dropped** by any licence state, capacity limit, spend cap, entitlement failure, AI failure, integration failure or configuration change. | Calls in progress represent live conversations with a customer's customers |
| **INV-04** | **AI failure never degrades a call.** An unavailable, slow or misbehaving AI provider affects AI features only. | AI is optional throughout the product |
| **INV-05** | **The platform operates fully with no AI configured.** All calling, routing, queueing, recording, reporting and API functions work with no provider connected. | AI is an enhancement, never a dependency |
| **INV-06** | **Telemetry, auditing, recording metadata and integration delivery are asynchronous** and never block or delay call processing. A failure in any of them is logged and alerted, never surfaced as a call failure. | Observability must not become an availability risk |
| **INV-07** | **Every capability is reachable through the API.** A function available only in our own interface is an incomplete feature. | The API is the product for partners |
| **INV-08** | **Records are immutable.** Call records, usage records and audit entries are never edited; corrections are separate, attributable adjustments. | Partners bill their customers from this data |
| **INV-09** | **Compliance rules are enforced, not advised.** Where a rule prohibits a call, the platform prevents it. No override exists for any role. | The alternative is a fine per call |
| **INV-10** | **Tenant isolation is absolute.** No user of one organisation may see, reach or infer another's data through any interface, including search results and error messages. | One breach ends the partner business |
| **INV-11** | **Credentials supplied by a partner or tenant are never exposed** — not in logs, exports, diagnostics, error messages, or to our own staff in readable form. | Trust is the product in a self-hosted model |
| **INV-12** | **Configuration changes never interrupt calls in progress.** | Nobody should schedule downtime to add an extension |

---

## 7. Epics — Release 1.0

### EPIC-01 · Multi-Tenant Engine
**BRD:** FBR-R1-04, FBR-R1-08 · **Priority:** M

**Purpose.** Tenancy and access control are the foundation every other feature stands on. Retrofitting them is a rebuild.

| ID | User story |
|---|---|
| US-01.1 | As a partner engineer, I create a customer organisation that is completely isolated from every other. |
| US-01.2 | As a tenant administrator, I create departments with their own numbering, greetings and routing. |
| US-01.3 | As a tenant administrator, I grant permissions by role and scope. |
| US-01.4 | As a compliance officer, I see who changed what, when, and what it was before. |
| US-01.5 | As a tenant administrator, my changes take effect immediately without interrupting calls. |
| US-01.6 | As a partner engineer, I set concurrent-call and spend limits per tenant. |
| US-01.7 | As a partner engineer, I suspend one tenant without affecting any other. |

**Acceptance criteria**
- AC-01.1 A user of one tenant cannot see, reach or infer the existence of another tenant's data — through any interface, including search results and error messages.
- AC-01.2 Overlapping extension numbers between tenants are permitted and never collide.
- AC-01.3 Every configuration change produces exactly one audit entry with actor, time, target and before/after values.
- AC-01.4 The audit log cannot be edited or deleted by any role, including partner engineers and our own staff.
- AC-01.5 Disabling a user ends their sessions and their ability to place calls within 10 seconds.
- AC-01.6 A configuration change applies to the next call within 5 seconds, with no interruption to calls in progress.
- AC-01.7 Suspending a tenant takes effect within 10 seconds, blocks new calls, leaves active calls running, and is fully reversible.

---

### EPIC-02 · Carrier Connectivity and Least-Cost Routing
**BRD:** FBR-R1-03 · **Priority:** M

**Purpose.** Deliver the BYOT promise in practice. This epic is where the "no lock-in, no markup" positioning becomes real or hollow.

| ID | User story |
|---|---|
| US-02.1 | As a partner engineer, I connect a carrier through a guided wizard without editing configuration files. |
| US-02.2 | As a partner engineer, I connect several carriers and define which is preferred for which destination. |
| US-02.3 | As a partner engineer, calls move away from a failing carrier automatically. |
| US-02.4 | As a partner finance manager, I see the cost of calls by carrier and destination. |
| US-02.5 | As a tenant administrator, I control which caller identity is presented per department and destination. |
| US-02.6 | As a partner engineer, I set per-carrier channel limits so I never exceed what I have purchased. |

**Acceptance criteria**
- AC-02.1 A new carrier is connected and placing calls within 30 minutes by an engineer who has not used the system before.
- AC-02.2 With two carriers configured, disabling the primary moves new calls to the secondary within 5 seconds; calls in progress are unaffected.
- AC-02.3 Routing rules select the intended carrier in 100% of tested cases, and the selected route appears on the call record.
- AC-02.4 The platform refuses to present a caller identity not registered to that tenant.
- AC-02.5 A carrier failing beyond a configured threshold is removed automatically and an alert is raised.
- AC-02.6 Least-cost routing decisions are explainable — the record shows which rule matched and why.

---

### EPIC-03 · Browser Calling and PBX Core
**BRD:** FBR-R1-01, FBR-R1-02, FBR-R1-06 · **Priority:** M

**Purpose.** The everyday product. Most of a partner's customers will judge the platform entirely on this epic.

| ID | User story |
|---|---|
| US-03.1 | As an employee, I make and receive calls in my browser with nothing installed. |
| US-03.2 | As an employee, I hold, mute, transfer (announced or blind), park and add a third party. |
| US-03.3 | As an employee, I see who is available, busy or away before I transfer. |
| US-03.4 | As a shift worker, I log in at any device and my extension follows me. |
| US-03.5 | As an employee, I receive voicemail by email and see my call history with recordings. |
| US-03.6 | As a partner engineer, I register a desk phone by its identifier and it configures itself when powered on. |
| US-03.7 | As a tenant administrator, I build a multi-level IVR with business hours and holidays, visually. |
| US-03.8 | As a tenant administrator, I test a call flow before publishing it, and roll back a bad change. |
| US-03.9 | As a caller, I wait in a queue and hear my position or expected wait. |
| US-03.10 | As an employee on a poor network, I see an honest connection state rather than a dead call. |

**Acceptance criteria**
- AC-03.1 A user places and answers calls on office, home and mobile networks with nothing installed.
- AC-03.2 Transfer, park, conference and hold work in both directions between browser, desk phone and external parties.
- AC-03.3 Presence reflects a state change within 3 seconds.
- AC-03.4 Changing network mid-call does not end the call; any audio gap is under 2 seconds.
- AC-03.5 A supported desk phone, factory-reset, becomes usable with no manual configuration on the device; its configuration is retrieved over an encrypted channel and is unusable by any other device.
- AC-03.6 A non-engineer builds a two-level IVR with business hours and routes a live call through it, unaided, in under 30 minutes.
- AC-03.7 The builder refuses to publish a flow containing a dead end or an unreachable step.
- AC-03.8 Publishing applies to the next call with no restart and no effect on calls in progress; rollback completes in under one minute.
- AC-03.9 Loss of connectivity produces a visible state within 10 seconds. The interface never shows a call as active when it is not.
- AC-03.10 A call in progress survives the user navigating anywhere in the application.

---

### EPIC-04 · Open AI Media Pipeline
**BRD:** FBR-R1-07 · **Priority:** M

**Purpose.** Deliver zero AI lock-in as a working capability, and lay the foundation every R2 and R3 intelligence feature depends on.

| ID | User story |
|---|---|
| US-04.1 | As a partner engineer, I configure an AI provider by entering my own credentials, and select whether it is self-hosted or a commercial service. |
| US-04.2 | As a partner engineer, I switch providers by configuration, without a code change, a release or a restart. |
| US-04.3 | As a tenant administrator, I choose whether audio from my organisation is streamed at all, and to which configured provider. |
| US-04.4 | As a developer, I receive each speaker on a separate stream so speakers are never confused. |
| US-04.5 | As a compliance officer, I see which calls were streamed, to which provider, and for how long. |
| US-04.6 | As a partner engineer, an AI provider failing or slowing never affects a call. |
| US-04.7 | As a partner finance manager, I see streamed minutes per tenant so I can attribute my provider's bill. |

**Acceptance criteria**
- AC-04.1 Enabling streaming produces no measurable change in call quality, verified under full load rather than asserted.
- AC-04.2 Each speaker is delivered on a separate stream, and downstream transcription attributes speech to the correct party in at least 98% of tested segments.
- AC-04.3 Killing the AI service mid-call leaves the call entirely unaffected; the failure is logged and alerted.
- AC-04.4 A slow provider causes frames to be dropped, never the call path to block.
- AC-04.5 No audio leaves the deployment for a tenant that has not enabled it — proven by test, not by configuration review.
- AC-04.6 At least one self-hosted open-source provider and one commercial provider are demonstrated, interchangeable by configuration alone.
- AC-04.7 Provider credentials are stored encrypted, scoped to their owner, and never appear in logs, exports, diagnostic bundles or error messages.
- AC-04.8 Streamed duration per tenant is recorded and available through the API.

---

### EPIC-05 · API Telemetry and Webhooks
**BRD:** FBR-R1-11 · **Priority:** M

**Purpose.** The epic that makes AtsaPBX a platform rather than an application. A partner must be able to build their own portal, their own billing and their own automation without asking us for anything.

| ID | User story |
|---|---|
| US-05.1 | As a partner developer, I perform every action available in the console through a documented API. |
| US-05.2 | As a partner developer, I subscribe to call events and receive them reliably, with retries when my endpoint is down. |
| US-05.3 | As a partner developer, I consume a live stream of channel and call-state telemetry. |
| US-05.4 | As a partner developer, I pull usage records granular enough to rate and invoice my own customers. |
| US-05.5 | As a partner developer, I trigger workflows in my ERP or billing system from platform events. |
| US-05.6 | As a partner developer, I discover the API from published documentation and a working sandbox, without contacting support. |
| US-05.7 | As a partner developer, I rely on the API not breaking under me. |

**Acceptance criteria**
- AC-05.1 **Every function in the administration console is demonstrably performed through the public API.** Any console-only capability is a defect, not a limitation.
- AC-05.2 Call state changes are delivered as events within 2 seconds of occurring.
- AC-05.3 Webhook delivery is at-least-once with signed payloads, exponential retry, a dead-letter queue and manual replay from the console.
- AC-05.4 Usage records carry, at minimum: tenant, department, direction, source, destination, start, answer and end times, billable seconds, carrier, route rule, disposition, service type and AI-stream duration.
- AC-05.5 A partner rates and invoices a full month of traffic from API data alone, with no access to the database — validated in acceptance testing with a real external billing system.
- AC-05.6 The API is versioned; no breaking change occurs within a major version; deprecation carries at least 12 months' notice.
- AC-05.7 Documentation is published with worked examples, and a sandbox tenant is available to every partner.
- AC-05.8 API responses under normal load return within 500 ms at the 95th percentile.

---

### EPIC-06 · Cryptographic Licensing and Capacity Enforcement
**BRD:** FBR-R1-12, BR-05 · **Priority:** M — *no sale is possible without it*

| ID | User story |
|---|---|
| US-06.1 | As a partner, I activate my licence with a key and the system configures its edition and capacity. |
| US-06.2 | As a partner, my system keeps working through an internet outage. |
| US-06.3 | As a partner, I see channel usage against my licence so I know when to upgrade. |
| US-06.4 | As a partner, upgrading capacity applies immediately without reinstalling or restarting. |
| US-06.5 | As a tenant administrator, features outside my edition are visible but clearly marked, with a route to upgrade. |
| US-06.6 | As a partner in an air-gapped deployment, I activate offline with a manually issued licence. |
| US-06.7 | As our own operations team, I issue, extend, suspend and revoke licences from one place. |

**Acceptance criteria**
- AC-06.1 Licence payloads are cryptographically signed and rejected if altered.
- AC-06.2 The licence is bound to a **weighted hardware fingerprint** with tolerance — a partial mismatch triggers a warning and a re-check, never an immediate failure.
- AC-06.3 Validation is local. **No licence check ever sits in the path of a call.**
- AC-06.4 A daily entitlement check runs automatically; failure to reach it produces administrator warnings from day 2.
- AC-06.5 A disconnected system operates fully for 7 days; after that it degrades to reduced capacity. **It is never fully disabled.**
- AC-06.6 Exceeding licensed channels rejects new call setup with a standard network-level rejection signal and a distinct telemetry reason code. **Calls in progress are never dropped.**
- AC-06.7 **Emergency calls connect in every licence state — valid, expired, degraded, over-capacity or tampered.** Tested in every release. This path cannot be gated.
- AC-06.8 A capacity upgrade applies within 60 seconds with no interruption to calls in progress.
- AC-06.9 A partner performs a limited number of self-service reactivations after legitimate infrastructure changes, without contacting us.
- AC-06.10 The concurrent-channel counter is atomic and correct under simultaneous call setup — verified by a race test at 10× normal setup rate.

---

### EPIC-07 · Recording Governance and Tamper-Evident Records
**BRD:** FBR-R1-09, FBR-R1-10, BR-07 · **Priority:** M

| ID | User story |
|---|---|
| US-07.1 | As a compliance officer, I set recording rules per tenant, department and jurisdiction. |
| US-07.2 | As a compliance officer, I require an announcement before recording where the law demands it. |
| US-07.3 | As an agent, I pause recording while a customer reads out payment details. |
| US-07.4 | As a compliance officer, I set retention, and deletion happens automatically and permanently. |
| US-07.5 | As an auditor, I see every occasion a recording was played, downloaded or exported, and by whom. |
| US-07.6 | As a support engineer, I find any call by number, user, time or outcome and see quality per participant. |
| US-07.7 | As a partner finance manager, I export records for any period through the API. |

**Acceptance criteria**
- AC-07.1 Where an announcement is required, recording does not begin until it has finished — verified by inspecting the recording itself.
- AC-07.2 A paused period contains no audio.
- AC-07.3 Expired recordings are unrecoverable after retention, including from backups, within the stated window.
- AC-07.4 Every access produces an audit entry naming the person. No role bypasses this.
- AC-07.5 Every call produces exactly one record — none missing, none duplicated — verified against carrier records for a full day.
- AC-07.6 Records are immutable; corrections appear as separate, attributable adjustments.
- AC-07.7 Every call carries a quality measurement per participant, not a sample.
- AC-07.8 Searching 10 million records returns results in under 2 seconds; a support engineer reconstructs any call from the previous 30 days in under 10 minutes.

---

### EPIC-08 · Edge Security and Fraud Control
**BRD:** FBR-R1-05 · **Priority:** M

| ID | User story |
|---|---|
| US-08.1 | As a partner engineer, I cap concurrent calls and daily spend per tenant and department. |
| US-08.2 | As a partner engineer, I block or allow whole countries. |
| US-08.3 | As a partner engineer, I am alerted to unusual calling patterns before the carrier bill arrives. |
| US-08.4 | As a partner engineer, I suspend outbound calling for one tenant instantly. |
| US-08.5 | As a security officer, the deployment presents the minimum possible surface to the internet. |

**Acceptance criteria**
- AC-08.1 Exceeding a cap blocks new outbound calls and alerts within 60 seconds. **Calls in progress are never cut off.**
- AC-08.2 A simulated fraud pattern — high volume to a high-cost destination outside business hours — is detected and blocked within 5 minutes.
- AC-08.3 Emergency numbers are never blocked by any spend, country or capacity rule.
- AC-08.4 Anonymous inbound signalling is refused. No default credentials exist anywhere in a shipped deployment.
- AC-08.5 An external scan of a default installation shows only the documented ingress points.

---

### EPIC-09 · Packaging, Installation and Upgrade
**BRD:** FBR-R1-13 · **Priority:** M

**Purpose.** We ship software onto machines we cannot access. This epic determines our support cost for the life of the product.

| ID | User story |
|---|---|
| US-09.1 | As a partner engineer, I install a working system on my own server in under an hour, following documentation. |
| US-09.2 | As a partner engineer, I upgrade without losing configuration, data or calls. |
| US-09.3 | As a partner engineer, I roll back a failed upgrade. |
| US-09.4 | As a partner engineer, I generate a diagnostic bundle and attach it to a support ticket. |
| US-09.5 | As a partner engineer, I back up and restore an entire deployment. |
| US-09.6 | As a partner engineer, a pre-flight check tells me before I install whether my server is suitable. |

**Acceptance criteria**
- AC-09.1 A certified engineer completes a fresh install unaided, on supported operating systems, in under 60 minutes.
- AC-09.2 A single-machine deployment profile is supported and passes the same test suite as a multi-node deployment.
- AC-09.3 Upgrade preserves all configuration, recordings and records, and is tested from every supported prior version before release.
- AC-09.4 Rollback restores the previous version and its data within 30 minutes.
- AC-09.5 The diagnostic bundle contains what support needs and **no call audio and no personal data** without explicit opt-in.
- AC-09.6 Every release ships with tested upgrade notes and a documented rollback path. A release without them does not ship.

---

### EPIC-10 · Console — Administration and Partner Portal
**BRD:** §12.1, §16 · **Priority:** M

**Purpose.** The single web application through which the platform is
configured and commercially managed. One product, one login, one
permission model: what a person sees is decided by their role and scope,
not by which application they opened. A partner engineer sees their
customers' configuration and their own licences; a tenant administrator
sees only their own organisation.

**This is the interface every non-agent user works in.** No customer of
AtsaPBX edits an Asterisk configuration file, uses the Asterisk CLI, or
is expected to know Asterisk exists — the same expectation VitalPBX and
3CX set. Everything a running system needs is configured here:
extensions and users, call flows and IVR menus, auto-attendant and
business hours, queues and contact-centre setup, SIP trunks and routing,
recording policy, and licences.

*(The agent workspace remains a separate product — principle 2. This
epic is the administration and partner surface, not the agent's screen.)*

| ID | User story |
|---|---|
| US-10.1 | As a partner, I buy, view and renew licences for my customers. |
| US-10.2 | As a partner, I register a deal and my margin is protected for a defined period. |
| US-10.3 | As a partner, I download the current release, supported prior releases and documentation. |
| US-10.4 | As a partner, I receive a free demonstration licence. |
| US-10.5 | As a partner, I raise a support ticket and see its status against my entitlement. |
| US-10.6 | As a partner, my staff take certification and I see who is certified. |
| US-10.7 | As a tenant administrator, I create and manage users, extensions and their permissions, without an engineer. |
| US-10.8 | As a tenant administrator, I configure call flows, IVR menus, auto-attendant and business hours visually, and see what a caller will experience before I publish. |
| US-10.9 | As a tenant administrator, I set up queues, agents and contact-centre behaviour for my business. |
| US-10.10 | As a partner engineer, I configure SIP trunks and routing for a customer and see immediately whether the trunk registered. |
| US-10.11 | As a tenant administrator, I set recording, retention and consent policy for my organisation. |
| US-10.12 | As any user, I see only what my role permits, and an action I may not perform is not offered rather than refused after the fact. |

**Acceptance criteria**
- AC-10.1 A partner issues a licence to their own customer without contacting us.
- AC-10.2 Deal registration prevents a second partner registering the same customer, and both are told what happened.
- AC-10.3 Response-time commitments are shown per tier and tracked against actual response.
- AC-10.4 A partner sees only their own customers' licences — verified by test, like any other isolation boundary.
- AC-10.5 **A complete working deployment — extensions, trunk, IVR, queue, recording policy — is configured entirely through this console, with no file edited and no command run on the server.** This is the acceptance test for the whole epic.
- AC-10.6 **Nothing in the console reveals Asterisk.** No dialplan context, endpoint identifier, channel name or configuration filename appears in any screen, error message or export. A user who has never heard of Asterisk configures the system successfully.
- AC-10.7 Every console action is performed through the public API, verified by test — a console-only capability is a defect, not a limitation (AC-05.1).
- AC-10.8 Permissions are enforced server-side and reflected in the interface: an action the user's role does not permit is not shown, and is refused by the API even if the request is made directly.
- AC-10.9 A configuration change that is accepted but not yet live is shown as pending, never as applied. The console never reports success for something the platform has not actually done.

---

## 8. Epics — Release 1.1

### EPIC-11 · Mobile Applications
**BRD:** FBR-R1-14 · **Priority:** S

| ID | User story |
|---|---|
| US-11.1 | As a mobile user, I receive calls reliably even when the app is closed. |
| US-11.2 | As a mobile user, my call continues when I move between Wi-Fi and mobile data. |
| US-11.3 | As a mobile user, I have the same call controls as on the desktop. |
| US-11.4 | As a partner, the applications carry my branding where my tier permits it. |

**Acceptance criteria**
- AC-11.1 A closed application rings within 5 seconds of an incoming call in 95% of trials.
- AC-11.2 A network change mid-call does not end it; any audio gap is under 2 seconds.
- AC-11.3 Applications are published for both major mobile platforms and pass store review.
- AC-11.4 Battery consumption while idle and registered is within platform norms for a communications application.

---

### EPIC-12 · Emergency Calling
**BRD:** FBR-R1-15 · **Priority:** M — **release gate**

| ID | User story |
|---|---|
| US-12.1 | As a remote user, my current location is registered and kept up to date. |
| US-12.2 | As a user in danger, my emergency call reaches the correct authority with my location. |
| US-12.3 | As a security manager, I am notified immediately when someone dials emergency services. |
| US-12.4 | As a user in an unsupported jurisdiction, I am told clearly, in advance, that emergency calling is unavailable. |

**Acceptance criteria**
- AC-12.1 Emergency calls bypass menus, routing rules, spend blocks, capacity limits and every licence state, without exception.
- AC-12.2 A test emergency call is validated with the relevant service in each launch jurisdiction.
- AC-12.3 Location is confirmed or updated when a user's network changes.
- AC-12.4 Where the obligation cannot be met, emergency dialling is blocked, the limitation is displayed in the client, and it is stated in the partner agreement.

---

### EPIC-13 · Fax and Migration Tooling
**BRD:** FBR-R1-16, FBR-R1-17 · **Priority:** S

| ID | User story |
|---|---|
| US-13.1 | As a user, inbound faxes arrive as PDF in my email. |
| US-13.2 | As a user, I send a document as a fax from the web interface. |
| US-13.3 | As a partner engineer, I import users, extensions, numbers and routing in bulk from a previous system. |
| US-13.4 | As a partner engineer, I see exactly what imported and what did not, and why. |

**Acceptance criteria**
- AC-13.1 Fax succeeds against a representative set of real-world destinations at an agreed success rate, measured rather than assumed.
- AC-13.2 A failed fax produces a clear notification with a reason, never silent loss.
- AC-13.3 Bulk import of 1,000 users and their numbers completes with a reconciliation report of successes and failures.
- AC-13.4 Full export completes in open formats and re-imports into a fresh installation.

---

## 9. Epics — Release 2

### EPIC-14 · Outbound Dialler
**BRD:** FBR-R2-01, FBR-R2-02 · **Priority:** M

| ID | User story |
|---|---|
| US-14.1 | As a campaign manager, I import a contact list and build segments with a visual filter, not a query. |
| US-14.2 | As a campaign manager, I choose preview, power or predictive dialling per campaign. |
| US-14.3 | As an agent, I am connected only to answered calls, with the contact's details already on screen. |
| US-14.4 | As a campaign manager, voicemails are detected and never passed to agents. |
| US-14.5 | As an agent, I leave a pre-recorded message with one click and move on. |
| US-14.6 | As an agent, I record the outcome in two clicks and the system decides what happens next. |
| US-14.7 | As an agent, I schedule a callback in the contact's own time zone. |

**Acceptance criteria**
- AC-14.1 With 10 agents on a clean list, predictive mode sustains at least 45 minutes of talk time per agent-hour within the abandonment limit, over a two-hour run.
- AC-14.2 Answering-machine detection is at least 95% accurate with no more than 2% of humans wrongly classified as machines.
- AC-14.3 Callbacks are delivered within 5 minutes of the promised time in 95% of cases.
- AC-14.4 A campaign pauses or stops within 10 seconds, with no new calls placed afterwards.
- AC-14.5 Every dial attempt is recorded with its outcome and appears on the contact record and in the API.

---

### EPIC-15 · Outbound Compliance
**BRD:** FBR-R2-03 · **Priority:** M — **release gate**

| ID | User story |
|---|---|
| US-15.1 | As a compliance officer, I upload do-not-call lists at tenant and campaign level. |
| US-15.2 | As a compliance officer, calling hours are applied in the contact's local time. |
| US-15.3 | As a compliance officer, calling slows automatically as abandonment approaches the legal limit. |
| US-15.4 | As a contact, my opt-out is honoured immediately and permanently. |
| US-15.5 | As an auditor, I am shown evidence that the rules were applied to every call in a period. |

**Acceptance criteria**
- AC-15.1 A listed number is never dialled — including on a manual dial by an agent. Proven by an automated suite that must pass before every release.
- AC-15.2 Calling hours use the contact's location, not the agent's, and are re-checked at the moment of dialling rather than only at list load.
- AC-15.3 Approaching the abandonment limit triggers automatic slow-down within 60 seconds; breaching it stops the campaign.
- AC-15.4 An opt-out applies across every campaign within 60 seconds.
- AC-15.5 A compliance report for any period shows rules applied, calls blocked and the reason for each.

---

### EPIC-16 · Supervisor Workspace
**BRD:** FBR-R2-04 · **Priority:** M

| ID | User story |
|---|---|
| US-16.1 | As a supervisor, I see every agent state and every live call on one screen. |
| US-16.2 | As a supervisor, I listen to a live call without either party knowing. |
| US-16.3 | As a supervisor, I speak privately to my agent during a call. |
| US-16.4 | As a supervisor, I join a call to rescue it. |
| US-16.5 | As a supervisor, I move a call by dragging it to another agent or queue. |
| US-16.6 | As a manager, I display a wallboard on a screen on the floor. |

**Acceptance criteria**
- AC-16.1 Silent listening is inaudible to both parties; private coaching is audible only to the agent — verified by inspecting the recording.
- AC-16.2 Every listen, coach and join is logged with the supervisor's identity, and can be disabled where the law forbids it.
- AC-16.3 The workspace reflects reality within 2 seconds and shows when its data is stale.
- AC-16.4 A wallboard runs unattended for 24 hours without reload or drift.

---

### EPIC-17 · CRM and ERP Connectivity
**BRD:** FBR-R2-05 · **Priority:** M

| ID | User story |
|---|---|
| US-17.1 | As a tenant administrator, I connect our CRM in a few steps without a developer. |
| US-17.2 | As an agent, the caller's record opens automatically when the phone rings. |
| US-17.3 | As an agent, I click a number in any web system and the call is placed. |
| US-17.4 | As a manager, every call is written back with outcome, duration and a recording link. |
| US-17.5 | As an agent using an in-house system, I get call controls in a floating widget over it. |
| US-17.6 | As a partner developer, I build a connector to a system you have never heard of, using the public API. |

**Acceptance criteria**
- AC-17.1 Connecting a supported CRM takes under 15 minutes by an administrator.
- AC-17.2 The caller's record appears before the agent answers in 95% of calls where a match exists.
- AC-17.3 Every call appears in the connected system within 60 seconds of ending, with no duplicates.
- AC-17.4 An integration failure never affects the call; failed events are retried and visible.
- AC-17.5 The floating widget works in any modern browser without modifying the host system's code.

---

### EPIC-18 · Live Agent Assist and After-Call Work
**BRD:** FBR-R2-06 · **Priority:** S

| ID | User story |
|---|---|
| US-18.1 | As an agent, I see a live transcript of my call. |
| US-18.2 | As an agent, relevant guidance appears when a topic or objection arises. |
| US-18.3 | As an agent, my call summary and outcome are drafted for me. |
| US-18.4 | As a manager, I configure what triggers guidance without a developer. |
| US-18.5 | As a compliance officer, I control whether transcripts are stored, and for how long. |

**Acceptance criteria**
- AC-18.1 Transcript appears within 2 seconds of speech, with speakers correctly attributed at least 98% of the time.
- AC-18.2 The agent may correct a drafted summary before it is saved. Nothing is written to a customer's system without human confirmation in the first release of this feature.
- AC-18.3 AI features degrade gracefully — the call and the agent's work continue unaffected when the provider is unavailable.
- AC-18.4 Streamed minutes are attributed per tenant so the partner can reconcile their provider's invoice.

---

## 10. Epics — Release 3

### EPIC-19 · Specialist Matching and Sub-15-Second Dispatch
**BRD:** FBR-R3-01, FBR-R3-02, FBR-R3-03 · **Priority:** M

**Purpose.** Replace a coordinator phoning down a list with an unattended connection in under 15 seconds. This epic is the R3 product.

| ID | User story |
|---|---|
| US-19.1 | As a caller, I request a specialist by language and subject, by keypad or speech. |
| US-19.2 | As a caller, I am connected to a qualified specialist in under 15 seconds without speaking to a coordinator. |
| US-19.3 | As a specialist, I hear what the session is before accepting, and accept with one keypress. |
| US-19.4 | As an operator, an unfilled request escalates to a wider tier and then to a person. |
| US-19.5 | As an operator, I see exactly who was contacted, who accepted, and how long each step took. |
| US-19.6 | As a specialist, I set my availability and see my scheduled assignments. |
| US-19.7 | As a client, I book a specialist for a future time and it dispatches automatically. |

**Acceptance criteria**
- AC-19.1 Median request-to-connected time is under 15 seconds; the 90th percentile is under 30 seconds.
- AC-19.2 Qualified specialists are contacted **simultaneously**, not sequentially — sequential contact cannot meet the target and is a defect.
- AC-19.3 Exactly one specialist joins any session — verified across 1,000 simulated simultaneous acceptances with zero duplicates.
- AC-19.4 Specialists who did not win stop ringing within 2 seconds of another accepting.
- AC-19.5 Matching respects language pair, dialect and subject expertise, and the reason for each selection is recorded.
- AC-19.6 If the caller hangs up during dispatch, all contacts stop immediately and nobody is billed.
- AC-19.7 Every request has a complete, replayable timeline available through the API.

---

### EPIC-20 · Department PIN and Budget Tracking
**BRD:** FBR-R3-04 · **Priority:** M

| ID | User story |
|---|---|
| US-20.1 | As a caller, I identify my organisation and department with a PIN. |
| US-20.2 | As an operator, invalid PINs are handled without wasting a specialist's time. |
| US-20.3 | As a client administrator, I see usage and remaining budget by department. |
| US-20.4 | As an operator, a department over budget reaches a person rather than a chargeable specialist. |

**Acceptance criteria**
- AC-20.1 No specialist is contacted before the caller is identified and authorised. No exceptions.
- AC-20.2 Three failed attempts route to a coordinator, then to voicemail; the attempt is recorded.
- AC-20.3 Usage is attributed to the correct department on 100% of calls.
- AC-20.4 A suspended or over-budget client never reaches a chargeable specialist.

---

### EPIC-21 · Per-Second Margin Ledger
**BRD:** FBR-R3-05 · **Priority:** M

| ID | User story |
|---|---|
| US-21.1 | As an operator, I set prices by client, language, service type and time of day. |
| US-21.2 | As an operator, I change a price without affecting any call already recorded. |
| US-21.3 | As an operator, I see revenue, specialist cost and margin for every call. |
| US-21.4 | As a specialist, I see what I have earned, per call, as I earn it. |
| US-21.5 | As a partner developer, I pull the ledger through the API into my own invoicing system. |

**Acceptance criteria**
- AC-21.1 Calculation is performed once and is repeatable: re-running any period produces identical figures.
- AC-21.2 Ledger totals reconcile to call records exactly over a 10,000-call period.
- AC-21.3 A price change never alters an already-recorded call.
- AC-21.4 Revenue, cost and margin exist for every completed call within 60 seconds of it ending.
- AC-21.5 Ledger entries are immutable; corrections are separate credit entries.
- AC-21.6 Rounding rules are configurable per contract and visible on every output.

---

### EPIC-22 · Optional Rating and Invoicing Module
**BRD:** FBR-R3-06 · **Priority:** C — *sold as an add-on*

| ID | User story |
|---|---|
| US-22.1 | As a partner, I generate invoices for my customers from platform data without building anything. |
| US-22.2 | As a partner, I manage rate cards, credit limits and prepaid balances. |
| US-22.3 | As a partner, I produce specialist payment statements from the same records as the invoices. |
| US-22.4 | As a partner's customer, I receive an invoice I can check line by line against a call log. |

**Acceptance criteria**
- AC-22.1 Invoice totals reconcile to call records to the cent.
- AC-22.2 Issued invoices are immutable; corrections are credit notes.
- AC-22.3 The module is entirely optional — the platform functions identically without it, and partners using their own billing are never disadvantaged.

---

### EPIC-23 · Governance, Residency and Spend Caps
**BRD:** FBR-R3-07 · **Priority:** M

| ID | User story |
|---|---|
| US-23.1 | As a partner, I pin a tenant's recordings and records to a chosen region. |
| US-23.2 | As a compliance officer, I request deletion of one person's data and see it completed. |
| US-23.3 | As a partner, I set spend caps per tenant and receive alerts before they are reached. |
| US-23.4 | As an auditor, I export a full audit trail through the API. |

**Acceptance criteria**
- AC-23.1 Data for a region-pinned tenant never leaves that region — verified by test, not by configuration review.
- AC-23.2 A deletion request completes within the stated window, including backups, with evidence.
- AC-23.3 Spend caps block new outbound calls and alert without cutting off calls in progress.

---

## 11. Product lifecycle and state definitions

These describe what a user or operator **experiences**, and what the product is and is not permitted to do in each state. Implementation is out of scope here and belongs in technical design.

### 11.1 Call lifecycle

| State | Entered when | Valid next states | What is experienced | Functional restrictions |
|---|---|---|---|---|
| **Initiated** | A call is requested — inbound arrival, agent dial, or API request | Screening, Terminated | Caller hears nothing yet; originator sees "connecting" | No participant is billable. No recording. No AI streaming. |
| **Screening** | The request is checked against capacity, spend caps, suspension, compliance rules and destination permissions | Routing, Terminated | Imperceptible to the caller in normal operation | **Emergency calls skip this state entirely (INV-01).** No external contact is made until screening passes |
| **Routing** | Screening passed; the destination is being determined — menu, queue, department or direct | Presenting, Terminated | Caller may hear a menu, announcement or hold treatment | Still no second participant. Menu interaction permitted. Call is not yet billable to the customer |
| **Presenting** | A destination has been chosen and is being alerted | Active, Routing (no answer, try next), Terminated | Caller hears ringing or hold treatment; recipient's device alerts | Alerting has a defined maximum before returning to Routing or moving to the next destination |
| **Active** | Two or more participants can hear one another | Degraded, Active (participants change), Terminating | Normal conversation | Billable time accrues per participant. Recording, monitoring, participant add/remove and AI streaming are all permitted. **Cannot be terminated by any commercial condition (INV-03)** |
| **Degraded** | Quality falls below the acceptable threshold, or an optional service supporting the call has failed | Active (recovers), Terminating | Conversation continues; quality may be noticeably poorer; supervisor dashboards mark the call | AI features suspended. Recording continues if already started. New participants may not be added. The call is **never terminated because of degradation alone** |
| **Terminating** | A participant hangs up, or the last remaining participant leaves | Terminated | Call ends for all participants | Recording finalises; no new participant may join |
| **Terminated** | All participants have left and the record is closed | — | Call appears in history and reporting | Record becomes immutable. Billable durations are final. Usage records are emitted |

**Participants may join and leave without the call changing state.** A supervisor joining, or a third party being added, is a change to the participant list within Active — not a new call and not a state transition.

### 11.2 Licence and capacity state

| State | Entered when | Valid next states | What the administrator sees | Functional restrictions |
|---|---|---|---|---|
| **Valid** | Entitlement confirmed and capacity below the warning threshold | Capacity Warning, Entitlement Unverified, Suspended | Normal operation; usage visible against entitlement | None |
| **Capacity Warning** | Concurrent usage reaches 80% of entitlement | Valid, Capacity Reached | Persistent warning in the console with current usage and a route to upgrade | None. **All functions remain fully available** — this is informational |
| **Capacity Reached** | Concurrent usage reaches 100% of entitlement | Capacity Warning, Valid | Prominent alert; each rejected call is visible with its reason | New calls are declined with a standard network-level rejection. **Calls in progress continue untouched (INV-03).** A short overflow allowance absorbs busy periods before rejection begins. **Emergency calls always connect (INV-01)** |
| **Entitlement Unverified** | The daily entitlement confirmation has not succeeded | Valid, Degraded | Warnings escalate from the second day, stating the remaining period and what will change | **No functional restriction during the defined grace period.** Everything works normally |
| **Degraded** | The grace period elapsed without confirmation | Valid (on confirmation) | Clear explanation of the reduced state and how to restore it | Capacity reduces to a defined minimum. **The system is never fully disabled.** Existing calls continue. Emergency calls always connect. Administration and the API remain available so the situation can be resolved |
| **Suspended** | Deliberate administrative suspension of a tenant | Valid | Tenant users see a clear service message | New outbound calls declined. Calls in progress continue to completion. Emergency calls always connect. Data remains intact and exportable |

**Open technical question for TRD:** the mechanism of entitlement confirmation, the overflow allowance calculation, and how installation identity is established are technical design decisions and are deliberately not specified here.

### 11.3 AI streaming session

Applies only where a tenant has enabled AI. **Where AI is not configured, this lifecycle does not exist and no product behaviour changes (INV-05).**

| State | Entered when | Valid next states | What is experienced | Functional restrictions |
|---|---|---|---|---|
| **Disabled** | No provider configured, or the tenant has AI switched off | Idle | AI features are absent from the interface, not shown as broken | All non-AI functions unaffected |
| **Idle** | A provider is configured and available; no call currently streaming | Starting | AI features shown as available | — |
| **Starting** | A call begins for which AI is enabled | Active, Fallback | Live features show as preparing, for a bounded period | If not Active within the defined start window, the session moves to Fallback. **The call is unaffected either way** |
| **Active** | Audio is flowing and results are returning | Reconnecting, Closed | Live transcript, prompts and other enabled features are updating | Results are advisory. Nothing generated is written to a customer system without human confirmation |
| **Reconnecting** | Results stop, or provider responses exceed the acceptable delay | Active, Fallback | Live features show a visible "reconnecting" state — **never a frozen display presented as current** | Features stop updating. **Call quality and continuity are unaffected (INV-04)** |
| **Fallback** | Reconnection did not succeed within the defined window | Idle (at call end) | Agent is told plainly that assistance is unavailable for this call, and continues manually | AI features unavailable for the remainder of the call. Menus revert to keypad interaction. **The call continues normally.** Partial results already produced remain visible and are marked incomplete |
| **Closed** | The call ended, or the tenant disabled AI mid-call | Idle, Disabled | Any completed output is retained per policy | Streaming stops; any retention rule applies |

### 11.4 Integration and webhook health

Per tenant, per configured destination.

| State | Entered when | Valid next states | What the administrator sees | Functional restrictions |
|---|---|---|---|---|
| **Active** | Deliveries are succeeding | Retrying | Healthy indicator with recent delivery history | None |
| **Retrying** | One or more deliveries have failed and are within the retry schedule | Active, Unhealthy | Pending count and next attempt time | Events queue in order. **No effect on calls (INV-06)** |
| **Unhealthy** | Three consecutive deliveries have failed | Active, Disabled | Prominent alert naming the destination and the failure reason | Retries continue on a slower schedule. Undelivered events are retained and replayable |
| **Disabled** | Continuous failure for 24 hours, or an administrator disables it | Active (manual re-enable) | Clear disabled state, with the retained event count and a replay control | Delivery stops. Events continue to be retained for the retention window. Manual replay is available. **Calls are never affected** |

---

## 12. Multi-actor scenario traces

Each scenario traces one journey across every actor. **Partner/Admin** operates the platform. **End-user** is an agent, employee or caller. **Carrier** is the external telephone network. **System** is the platform, including any AI engine.

### 12.1 Standard call — setup and teardown

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | End-user (caller) | Dials a published number | Carrier delivers the call to the platform |
| 2 | Carrier | Presents the incoming call | System accepts and creates a call record |
| 3 | System | Screens capacity, suspension and destination permission | Passes; caller is unaware |
| 4 | System | Applies the configured menu, hours and department rules | Caller hears a greeting or menu |
| 5 | End-user (caller) | Makes a keypad selection | System routes to the matching queue |
| 6 | System | Selects an available recipient by the configured method | Recipient's device alerts |
| 7 | End-user (agent) | Answers | Participants can hear one another; billable time begins for each |
| 8 | System | Starts recording if policy requires, plays any required announcement first | Recording begins only after the announcement completes |
| 9 | System | Starts AI streaming **if the tenant has enabled it** | Live features appear. If not enabled, nothing changes |
| 10 | End-user | Conversation | — |
| 11 | End-user (either) | Hangs up | Call moves to Terminating |
| 12 | System | Finalises recording, closes the record, emits usage data and integration events | Record becomes immutable and appears in reporting |
| 13 | Partner/Admin | Views the call in reporting or retrieves it through the API | Full detail available, including quality measurement per participant |

### 12.2 Carrier failure during a campaign

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | Carrier | Primary route begins failing — setup failures rise | Calls in progress on that route continue |
| 2 | System | Health checking detects failures crossing the defined threshold | Route marked degraded |
| 3 | System | Stops offering new calls to the degraded route | New calls use the next configured route within the failover window |
| 4 | System | Alerts the administrator with the route, the reason and the affected volume | Alert raised immediately, not on a schedule |
| 5 | End-user (agents) | Continue working | **Calls in progress are unaffected. Callers notice nothing.** New calls proceed on the alternate route |
| 6 | Partner/Admin | Reviews the alert; may disable the route manually | Manual override always available |
| 7 | System | Continues probing the degraded route | Route returns to service automatically after a sustained healthy period, or on manual re-enable |
| 8 | System | Records the incident with its duration and affected call count | Available in reporting and through the API for a carrier conversation |

**Explicitly not promised:** a call already in progress on a failed route cannot be moved. That is an external limitation, it is stated in the service commitments, and it is not presented to partners as something the platform can do.

### 12.3 Emergency call

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | End-user | Dials an emergency number | System recognises it before any other rule is applied |
| 2 | System | **Bypasses menus, routing rules, capacity limits, spend caps, suspension and licence state entirely (INV-01)** | The call proceeds regardless of every commercial condition |
| 3 | System | Attaches the registered location for that user | Location accompanies the call |
| 4 | Carrier | Routes to the correct emergency authority | Call connects |
| 5 | System | Notifies the designated organisational contact immediately | Notification is immediate, not batched |
| 6 | System | Records the event in the audit trail | Retained and reportable |
| 7 | Partner/Admin | Reviews emergency call activity | Full history available |

**Where the capability is not yet available in a jurisdiction (R1.0):** the attempt is **blocked with a clear spoken and on-screen message telling the user to dial from another phone.** It is never silently accepted and mis-routed (INV-02).

### 12.4 Capacity exhaustion — before setup, and mid-call

**Before setup (new call arrives at the limit):**

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | System | Concurrent usage reaches the entitlement | State becomes Capacity Reached |
| 2 | System | Applies the short overflow allowance | Busy periods absorb without rejection while allowance remains |
| 3 | Carrier | Presents one more inbound call | System declines with a standard rejection signal, so the carrier can route elsewhere |
| 4 | End-user (external caller) | Hears a busy or unavailable treatment | Not a silent failure; the caller receives a normal telephone experience |
| 5 | End-user (agent) | Attempts an outbound call | Told plainly that capacity is reached and to try again shortly |
| 6 | System | Alerts the administrator with current usage and rejection count | Includes a direct route to upgrade |
| 7 | Partner/Admin | Increases entitlement | Applies without restart; **calls in progress unaffected** |

**Mid-call (limit reached while calls are running):**

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | System | Entitlement is reduced, expires, or usage is recounted while calls are active | State changes |
| 2 | System | **Takes no action against any active call (INV-03)** | Every conversation continues to natural completion |
| 3 | System | Declines only new call setup | Existing participants notice nothing |
| 4 | System | Emergency calls continue to connect | Always (INV-01) |
| 5 | Partner/Admin | Sees active count above entitlement, clearly explained as calls draining | Not presented as an error requiring intervention |

### 12.5 AI stream disconnection mid-call

| # | Actor | Action | Result |
|---|---|---|---|
| 1 | End-user (agent) | On a call with live assistance active | Transcript and prompts updating |
| 2 | System (AI) | Provider stops responding, or exceeds the acceptable delay | Session moves to Reconnecting |
| 3 | System | **Call continues, unchanged (INV-04)** | Neither party hears any difference |
| 4 | End-user (agent) | Sees a visible reconnecting indicator | **Never a frozen transcript presented as live** |
| 5 | System | Attempts recovery within the defined window | If recovered, features resume and the gap is marked in the transcript |
| 6 | System | If not recovered, moves to Fallback | Agent is told assistance is unavailable for this call and continues manually |
| 7 | System | An automated menu that was using AI reverts to keypad interaction | The caller experiences a standard menu, not a failure |
| 8 | System | Alerts the administrator; records the outage against the tenant | Visible in reporting; usable in a conversation with the AI provider |
| 9 | End-user (agent) | Completes after-call work manually | No work is lost; partial output remains and is marked incomplete |

---

## 13. Failure paths — what happens next

Every failure mode has a defined user experience. "Undefined behaviour" is not an acceptable answer for anything in this table.

### 13.1 AI stream interruption

| Aspect | Behaviour |
|---|---|
| Effect on the call | **None.** The call continues at full quality (INV-04) |
| Agent experience | Visible reconnecting state, then a plain message that assistance is unavailable for this call |
| Automated menu behaviour | Reverts to keypad interaction; the caller experiences a normal menu |
| Recovery | Automatic reconnection attempts within a defined window; features resume if successful, with the gap marked |
| Data | Partial results retained and marked incomplete. Nothing partial is written to a customer system |
| Alerting | Administrator alerted on repeated failures, with per-tenant frequency |
| Escalation | Repeated failure within a period suspends AI for that tenant with an explanatory notice, rather than retrying indefinitely |

### 13.2 Licence or capacity breach

| Aspect | Behaviour |
|---|---|
| Calls in progress | **Always complete.** No commercial condition ends a live conversation (INV-03) |
| New inbound | Declined with a standard rejection signal so the carrier may route elsewhere; caller hears normal busy treatment |
| New outbound | Blocked with a plain message stating capacity is reached |
| Emergency calls | **Always connect (INV-01)** |
| Administration and API | Remain fully available so the situation can be resolved |
| Overflow | A short allowance absorbs peaks before rejection begins |
| Alerting | At 80% and 100% of entitlement, and on each rejection, with a direct upgrade route |
| Resolution | Increased entitlement applies without restart or interruption |

### 13.3 Integration and webhook delivery failure

| Aspect | Behaviour |
|---|---|
| Effect on calls | **None. Delivery is asynchronous and never blocks call processing (INV-06)** |
| Retry schedule | Increasing intervals — approximately 1 minute, 5 minutes, 15 minutes, 1 hour, 6 hours, 24 hours |
| Retry cap | Six attempts across 24 hours per event |
| Ordering | Events are delivered in order per destination; a blocked destination queues rather than skipping |
| Marked unhealthy | After three consecutive failures |
| Disabled | After 24 hours of continuous failure, or manually |
| Retention | Undelivered events retained for the tenant's retention window and replayable from the console and the API |
| Alerting | On entering Unhealthy, on Disabled, and on queue depth crossing a threshold |
| Recovery | Automatic on first success; manual replay available for the retained backlog |

### 13.4 Carrier route degradation

| Aspect | Behaviour |
|---|---|
| Detection | Continuous health checking plus setup-failure rate over a rolling window |
| Threshold | Consecutive failed checks, or setup failures crossing the configured rate |
| Failover for new calls | Within a defined short window, to the next configured route by priority |
| Calls in progress | Continue on the degraded route. **They cannot be moved — an external limitation, stated openly** |
| Caller experience | New calls proceed normally with no visible difference |
| Alerting | Immediate, naming the route, the reason and the affected volume |
| Return to service | Automatic after a sustained healthy period, or manual re-enable |
| No route available | New calls declined with a clear reason; administrator alerted at highest severity; emergency calls attempted on every configured route regardless of health |
| Record | Incident duration and affected call count retained for a carrier conversation |

### 13.5 Other failure paths

| Failure | What happens next |
|---|---|
| Recording fails to start | Call continues. Administrator alerted. Call record marked as not recorded, with the reason. **Where policy requires recording for compliance, the tenant may configure the call to be declined instead — an explicit choice, never a silent default** |
| Storage unavailable | Calls continue. Recording suspended with an alert. Records buffered and written when storage returns |
| Reporting unavailable | Calls continue. Live dashboards show a stale-data indicator rather than misleading figures |
| Administration interface unavailable | Calls continue. Configuration changes are unavailable; existing configuration keeps operating |
| Entitlement service unreachable | No functional change during the grace period. Warnings escalate. See §11.2 |
| Provisioning of a handset fails | The device is unusable; the administrator sees the specific failure. No effect on any other device or call |
| Import of contacts partially fails | Successful rows are imported; failures are listed with reasons in a reconciliation report. **Never a silent partial import** |

---

## 14. Cross-cutting product requirements

| Area | Requirement |
|---|---|
| **Responsiveness** | Call controls respond within 100 ms. Live figures update within 2 seconds. Any list returns within 2 seconds regardless of size. |
| **Honesty of state** | Every live figure shows its freshness. Stale data is visibly marked, never silently displayed. |
| **Accessibility** | Full keyboard operation of all call controls; screen-reader labelling; no meaning carried by colour alone; WCAG 2.2 AA for agent and admin interfaces. |
| **Localisation** | All interfaces translatable from the first release; tenant time zone and currency respected everywhere. |
| **Browsers** | Latest two versions of the major browsers. Anything unsupported is stated clearly rather than failing oddly. |
| **Errors** | Every error states what happened, whether it is the user's problem or ours, and what to do next. |
| **Empty states** | Every empty list explains what it is for and what to do first. |
| **Branding** | Partners at the appropriate tier apply their own name, logo and colours to end-user interfaces. |
| **Telemetry** | Every feature ships with the measurement that proves it works — §10. |

---

## 15. Measurement requirements

A feature without measurement cannot be improved or defended.

| Release | Instrumented from day one |
|---|---|
| R1.0 | Call setup success, quality per participant, install time, licence activations and rejections, capacity-limit rejections, API call volume and latency, webhook delivery success, AI-streamed minutes per tenant |
| R1.1 | Emergency-call tests, mobile notification delivery rate, fax success rate, migration completeness |
| R2 | Talk time per agent-hour, abandonment rate, detection accuracy and false-machine rate, callback punctuality, CRM write success |
| R3 | Connect-time distribution, first-wave fill rate, double-acceptance count (must be zero), ledger reconciliation variance, margin per call |

---

## 16. Open questions

### 16.1 Open product questions

| # | Question | Needed by |
|---|---|---|
| Q-1 | Which CRM connectors first? Driven by the first ten partners' customers, not by market share. | R2 planning |
| Q-2 | Which desk-phone families at R1.0? Driven by what partners' customers already own. | R1.0 planning |
| Q-3 | Does a drafted call summary write back automatically or after agent confirmation? Recommend confirmation until accuracy is proven. | R2 |
| Q-4 | Do specialists accept by keypad only, or also by mobile app? Keypad is far cheaper and works everywhere. | R3 |
| Q-5 | How much interface branding do partners get, at which tier? | R1.0 |
| Q-6 | Do we ship reference billing integrations as examples, or supported connectors? Recommend examples — supported connectors create an obligation that grows without limit. | R1.0 |


### 16.2 Open technical questions for TRD

Raised deliberately and left unanswered here. Each is an implementation decision, and answering it in a product document would constrain design prematurely.

| # | Question | Product constraint it must satisfy |
|---|---|---|
| T-1 | How is entitlement confirmed, and how is installation identity established? | Must never sit in the path of a call. Must tolerate an unreachable service for the defined grace period without functional change |
| T-2 | How is concurrent capacity counted accurately under simultaneous call setup? | Must be exact under load. Must never miscount in a way that rejects a call below the limit |
| T-3 | How is the short overflow allowance calculated and reset? | Must absorb ordinary peaks without allowing sustained over-use |
| T-4 | How is call audio delivered to an AI provider without affecting call quality? | Zero measurable quality impact, verified under load. A provider failure must be invisible to the call |
| T-5 | How is per-participant identity maintained when a call is transferred? | Billable duration must reflect continuous participation, not internal reconnection |
| T-6 | How is route health measured, and over what window? | Must detect genuine degradation without reacting to isolated failures |
| T-7 | How is undelivered integration data retained and replayed? | Ordering preserved per destination; retention honoured; replay available through the console and the API |
| T-8 | How is data residency enforced and proven? | Must be demonstrable by test, not by inspecting configuration |
| T-9 | How is deletion completed across all copies, including backups? | Must complete within the stated window and be evidenced |
| T-10 | How are recordings encrypted and access-controlled per tenant? | No credential or key readable by another tenant or by our own staff |
| T-11 | How is a call's quality measured per participant? | Every call, not a sample; sufficient to resolve a complaint within ten minutes |
| T-12 | What upgrade and rollback mechanism preserves configuration and data? | Rollback within 30 minutes; no configuration loss; tested from every supported prior version |

---
## 17. Definition of Ready — technical design gate

This PRD is ready for handoff to engineering when every line below is
satisfied. A partially satisfied gate is not a gate.

> **Gate status (2026-09-06):** the checkboxes below are the living
> readiness record, ticked as each artefact is produced and verified during
> technical design and implementation. They are evidence, not decoration:
> PRD §1's "approved for technical design" is held against this gate until
> every item is checked.

**Functional completeness**

- [ ] 1. Every BRD requirement traces to at least one epic, and every epic traces to at least one BRD requirement (§18)
- [ ] 2. Every epic has user stories and acceptance criteria written so a tester can pass or fail them without asking a question
- [ ] 3. Release boundaries agree with BRD §7; no capability appears in a release the BRD assigns elsewhere
- [ ] 4. In-scope, deferred and explicit non-goals are recorded and signed off (§5)
- [ ] 5. Every product state has defined entry conditions, valid next states and functional restrictions (§11)
- [ ] 6. Every failure mode has a defined user experience — no path resolves to undefined behaviour (§13)
- [ ] 7. Multi-actor journeys are traced for the standard path and for each identified edge case (§12)

**Invariants and compliance**

- [ ] 8. Product invariants are agreed and each is covered by at least one named acceptance criterion (§6)
- [ ] 9. Emergency-call behaviour is specified for both the supported and the unsupported jurisdiction case, and legal has confirmed the disclosure wording
- [ ] 10. Recording consent, retention and deletion behaviour is specified per jurisdiction and reviewed by compliance
- [ ] 11. Outbound compliance rules are specified as enforced behaviour with no override, and the evidence an auditor receives is defined
- [ ] 12. Tenant isolation expectations are stated as testable criteria, including search results and error messages
- [ ] 13. Credential handling expectations are stated for logs, exports, diagnostics and support access

**Commercial alignment**

- [ ] 14. Licence states and capacity behaviour match the commercial model, and confirmed by the CFO or channel owner
- [ ] 15. Every capability is assigned to an edition, and edition boundaries produce no feature that is unreachable in any purchasable combination
- [ ] 16. AI-dependent features are individually marked optional, and the product is demonstrably complete with no AI configured

**Experience and interface**

- [ ] 17. Every user-facing failure has defined wording that states what happened, whose problem it is, and what to do next
- [ ] 18. Accessibility, localisation and browser expectations are stated as testable criteria (§14)
- [ ] 19. Every capability in this document is expected to be reachable through the API, and any exception is explicitly listed and justified
- [ ] 20. Measurement requirements are defined for each release, so every claim in this document can be proven after launch (§15)

**Handoff**

- [ ] 21. Open technical questions are recorded (§16.2) and none of them is blocking a product decision
- [ ] 22. Product Owner, compliance and the commercial owner have each signed the version being handed over

---

## 18. Traceability

| BRD requirement | Epic | Release |
|---|---|---|
| FBR-R1-01 Browser calling and core telephony | EPIC-03 | R1.0 |
| FBR-R1-02 Desk-phone provisioning | EPIC-03 | R1.0 |
| FBR-R1-03 Carrier neutrality and least-cost routing | EPIC-02 | R1.0 |
| FBR-R1-04 Multi-tenant isolation | EPIC-01 | R1.0 |
| FBR-R1-05 Edge security and fraud control | EPIC-08 | R1.0 |
| FBR-R1-06 Visual call-flow and IVR builder | EPIC-03 | R1.0 |
| FBR-R1-07 Open AI engine choice | EPIC-04 | R1.0 |
| FBR-R1-08 Access control and audit | EPIC-01 | R1.0 |
| FBR-R1-09 Recording governance | EPIC-07 | R1.0 |
| FBR-R1-10 Tamper-evident records and quality | EPIC-07 | R1.0 |
| FBR-R1-11 API telemetry and extensibility | EPIC-05 | R1.0 |
| FBR-R1-12 Cryptographic licence security | EPIC-06 | R1.0 |
| FBR-R1-13 Deployment and lifecycle | EPIC-09 | R1.0 |
| Commercial model — partner enablement | EPIC-10 | R1.0 |
| FBR-R1-14 Mobile applications | EPIC-11 | R1.1 |
| FBR-R1-15 Emergency calling | EPIC-12 | R1.1 |
| FBR-R1-16 Fax | EPIC-13 | R1.1 |
| FBR-R1-17 Migration and exit | EPIC-13 | R1.1 |
| FBR-R2-01 Outbound dialling | EPIC-14 | R2 |
| FBR-R2-02 Answering-machine detection | EPIC-14 | R2 |
| FBR-R2-03 Outbound compliance | EPIC-15 | R2 |
| FBR-R2-04 Supervisor workspace | EPIC-16 | R2 |
| FBR-R2-05 CRM and ERP connectivity | EPIC-17 | R2 |
| FBR-R2-06 Live agent assist and after-call work | EPIC-18 | R2 |
| FBR-R3-01 Specialist matching | EPIC-19 | R3 |
| FBR-R3-02 Sub-15-second broadcast dispatch | EPIC-19 | R3 |
| FBR-R3-03 Availability and escalation | EPIC-19 | R3 |
| FBR-R3-04 Department PIN and budget tracking | EPIC-20 | R3 |
| FBR-R3-05 Per-second margin ledger | EPIC-21 | R3 |
| FBR-R3-06 Optional rating and invoicing module | EPIC-22 | R3 |
| FBR-R3-07 Governance and residency | EPIC-23 | R3 |

**Coverage:** every BRD requirement has an epic; every epic traces to a requirement. Business rules BR-01 to BR-16 are enforced across EPIC-01, EPIC-06, EPIC-07, EPIC-08, EPIC-12 and EPIC-15, and each is verified by a named acceptance criterion above.
