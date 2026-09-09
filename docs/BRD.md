# AtsaPBX — Business Requirements Document

**Version 1.0 · Executive Baseline for Launch**
**Audience:** CEO, CFO, Sales, Delivery Partners, Product, Engineering Leadership
**Owner:** Product Owner / Business Analyst
**Status:** Approved baseline

---

## 1. Executive summary

AtsaPBX is a **software platform for communications operators**. We licence a multi-tenant engine that runs business phone systems, contact centres and specialist dispatch — and our partners deploy it on their own infrastructure, with their own carriers, for their own customers.

The market we enter is fragmented in a way that costs everyone money. An organisation running phones, an outbound contact centre and any form of specialist dispatch buys three separate products from three vendors, reconciles three sets of reports, and pays per user in perpetuity for at least one of them. Meanwhile the operators serving those organisations — managed service providers, telecom resellers, language service providers, BPOs — have no platform that lets them serve all three needs from one system they control.

AtsaPBX exists for those operators. It is one platform covering business telephony, contact centre and specialist dispatch, licensed by call capacity rather than headcount, deployed wherever the partner chooses, integrated with the business systems their customers already run, and open at every boundary that competitors keep closed: carriers, AI providers, and the API itself.

> **AI is an optional enhancement, not a dependency.** The platform operates fully and natively without any LLM installed, AI agent connected, or external provider configured — all core telephony, routing, queuing, recording, reporting and API functionality works out of the box. AI features are configured per tenant and can be enabled, disabled, or switched between providers without affecting the underlying telephony service.

**The commercial shape of the business is deliberately simple.** We sell software licences and support. We do not carry calls, resell minutes, host by default, process end-user payments, or resell artificial intelligence. Our costs stay fixed while our partners' businesses grow.

---

## 2. Strategic position and operating model

### 2.1 We are a software vendor

AtsaPBX is a platform provider in the same category as 3CX and VitalPBX, not a service provider competing with our own partners.

| We supply | The partner supplies |
|---|---|
| The platform software and its licence | The infrastructure — their cloud, bare metal or data centre |
| Product updates, security fixes, supported versions | Their own SIP trunks and carrier contracts (BYOT) |
| Documentation, certification, tier-2 and tier-3 support | Their own AI provider accounts and API keys |
| APIs, webhooks and telemetry to build on | Billing, invoicing, collections and credit control for their customers |
| The engine behind their service | The commercial relationship with the end customer |

This is a choice, not a limitation. It removes us from carrier billing and AI billing entirely, keeps our cost base fixed while partner revenue is variable, and means a partner in a market we have never operated in can deploy us without waiting for us to sign a local carrier or AI vendor.

### 2.2 Platform and partner responsibility matrix

The most useful page in this document for a partner conversation. Anything not in the AtsaPBX column is not our commercial or operational responsibility.

| Domain | **AtsaPBX (platform)** | **Partner (operator)** |
|---|---|---|
| Media engine, codecs, call quality | Owns | — |
| PBX routing, menus, queues, contact-centre logic | Owns | Configures for their customers |
| Specialist and interpreter dispatch logic | Owns | Defines their languages, tiers and rules |
| Licence issuance and enforcement | Owns | Purchases and maintains the licence |
| Platform security, updates, vulnerability response | Owns | Applies updates; secures their own infrastructure |
| APIs, webhooks, telemetry, event streams | Owns | Consumes them; builds portals and automation |
| Call detail records and usage data | Generates and exposes | Consumes for rating and invoicing |
| **Rating, invoicing, credit control, collections** | Not ours — *optional R3 add-on module* | Owns |
| **Carrier contracts and minute purchasing** | Not ours | Owns |
| **AI provider accounts, keys and consumption cost** | Not ours — **optional and per-tenant** | Owns — or chooses not to use AI at all |
| **End-user support (tier 1)** | Not ours | Owns |
| **Infrastructure, hosting, backups, capacity** | Not ours — *except our limited SaaS* | Owns |
| **Number ranges, porting, regulatory registration** | Not ours | Owns |
| Emergency-call capability | Owns the capability in the software | Owns the jurisdictional obligation and configuration |

> **Two boundaries are shared, not divided.** Emergency calling and data protection are the areas where "not our responsibility" is not a complete answer. We must build the capability correctly and document it honestly; the partner must configure and operate it lawfully. Both obligations are written into the partner agreement, and neither should be described to a partner as entirely theirs.

### 2.3 Technology foundation — why we do not build telephony from scratch

This section exists because it is the question every technical buyer, investor and prospective partner asks, and the answer is a business advantage rather than an admission.

**AtsaPBX does not write low-level telephony or SIP protocol code.** Building a telephony stack from first principles — SIP signalling, media transport, codec negotiation, network address traversal, interoperability with thousands of carrier equipment variations — is five to ten years of specialist engineering. It is work that has already been done, exhaustively, in public.

We build on **Asterisk**, the most widely deployed and battle-tested open-source telecom engine in the world, and we control it through its programmable interfaces. Asterisk carries the audio; AtsaPBX makes every decision about what should happen to it.

| What this buys us | Why it matters commercially |
|---|---|
| **No protocol engineering risk** | The hardest, least differentiating and most defect-prone layer is mature, audited and deployed at global scale. We inherit that reliability on day one. |
| **Years removed from time to market** | Engineering effort goes into the product, not into rebuilding infrastructure that already works. |
| **Instant carrier compatibility** | Carriers worldwide already interoperate with this engine. Partners connect their existing trunks without a certification project. |
| **A large, available talent pool** | Engineers who know this technology can be hired in most markets. A proprietary stack would make every hire a training project. |
| **Ongoing security and standards work we do not pay for** | Protocol-level vulnerabilities and standards changes are handled by a global community with far more eyes than we could fund. |

**Our proprietary intellectual property sits above the engine**, and this is where every hour of our engineering budget goes:

- The **multi-tenant control plane** — isolation, routing intelligence, capacity management, and configuration that applies without service interruption
- The **open AI media pipeline** — streaming live audio to any provider the partner chooses, without touching the call path
- The **specialist dispatch engine** — simultaneous broadcast, first-accept-wins, sub-15-second connection
- The **API-first platform layer** — the telemetry, events and endpoints partners build businesses on
- The **licensing and packaging system** that makes it a sellable product rather than a project

The engine is a component we orchestrate. It is not the product, and it is not what a competitor would have to copy.

### 2.4 API-first as a commercial strategy

Every capability is available through a documented API before it appears in our own interface. Our administration console is a consumer of those APIs, with no privileged path into the system.

This is a commercial mechanism, not a technical preference. **A partner who can only use our screens is limited to the business we imagined. A partner who can call every endpoint builds a business we did not anticipate** — their own branded portal, their own rating engine, their own provisioning automation, their own workflows into the customer's ERP.

It is also the strongest retention mechanism available to a platform business. A partner who has built their operational tooling on our API does not migrate away over a price difference.

**The API is always present. AI is always optional.** Every API endpoint works regardless of whether AI is enabled. Partners building on the API do not depend on AI being present. The platform's core value — telephony, routing, recording, reporting, control — is API-accessible with or without AI.

---

## 3. Market and target partners

### 3.1 Who we sell to

| Partner type | What they run today | Why AtsaPBX |
|---|---|---|
| **Managed service providers and IT resellers** | Reselling a hosted phone system at thin margin, or an ageing on-premise product | Own the platform, keep the margin, add contact centre without a second vendor |
| **Telecom operators and ITSPs** | Selling minutes with a commodity PBX attached | Move up the value chain; monetise the platform, not just the traffic |
| **BPO and contact-centre operators** | A legacy dialler plus a separate phone system | One platform, modern interface, proper API, no per-agent licence |
| **Language service providers** | Manual interpreter dispatch — a coordinator phoning down a list | Automated sub-15-second dispatch with per-second margin visibility |
| **Systems integrators serving enterprise and government** | Assembling point products per project | A licensable platform that deploys in the customer's own jurisdiction |
| **Corporates with internal IT capability** | Per-seat cloud telephony that grows with headcount | Capacity licensing, data sovereignty, integration with their own systems |

### 3.2 Who we decline

Organisations under roughly fifty seats with no technical capability and no partner — hosted services serve them better and cheaper. Buyers whose primary need is video conferencing or team messaging. Partners unwilling to provide tier-1 support to their own customers, who will otherwise route every end-user question to us and destroy the economics for everyone.

---

## 4. Value proposition and differentiators

**In one sentence:** one licensed platform that runs a partner's phone systems, contact centres and specialist dispatch — on their carriers, their infrastructure, their AI provider — priced by call capacity, and open at every boundary competitors keep closed.

| # | Differentiator | Why it is hard to copy |
|---|---|---|
| **VP-1** | **One platform for PBX, contact centre and specialist dispatch** | Incumbents are one of the three. Unification is an architectural decision made at the start or never. |
| **VP-2** | **Capacity-based licensing: strictly per concurrent call channel, with unlimited extensions on every licensed edition** | Per-seat pricing is the incumbents' revenue model. They cannot follow without cutting their own revenue. Unlimited extensions removes the most common complaint about the closest comparable product — which prices per user *on the paid tiers*, where this claim is made and where it competes. The Free Community tier (§10.4) is capped at 10 extensions precisely so that it reads as an evaluation, not a product. |
| **VP-3** | **Bring your own trunk, with least-cost routing and no per-minute markup** | Most competitors earn margin on minutes. Neutrality is a business-model choice, not a feature. |
| **VP-4** | **API-first: 100% of signalling events, call states and channel telemetry exposed via REST, gRPC and webhooks** | Competitors expose a subset of their product because the API came afterwards. Ours is the product. Partners plug in external billing stacks or build custom portals without asking us. |
| **VP-5** | **AI-native, zero lock-in, fully optional** | The partner chooses the provider and pays them directly. AI is embedded throughout the platform — IVR and call-flow generation, agent assist, summarisation, quality scoring, interpreter matching — all through one consistent media pipeline and API. **The platform is designed AI-first, not AI-dependent.** It operates natively with no AI enabled. AI features are per-tenant and can be enabled, disabled or switched between providers by configuration. We have no margin reason to favour one provider — or to require AI at all. |
| **VP-6** | **Frictionless CRM and ERP integration** | Pre-built connectors and embeddable widgets that fit into the tools a customer already runs, rather than demanding replacement. Integration breadth is a sustained investment competitors under-fund because it does not demonstrate well. |
| **VP-7** | **Native specialist dispatch: sub-15-second simultaneous connection with per-second margin visibility** | Nobody in the PBX or contact-centre market has this. It is the wedge into language services and expert-on-demand. |
| **VP-8** | **Deploys where the partner chooses — their cloud, their hardware, or ours** | Vendors who host and resell cannot match this without abandoning their own model. |

**One claim to state accurately.** Self-hosted open-source AI is free of *licence* cost, not free of cost — it requires hardware and someone to run it. The honest and still-compelling position is: **no per-minute fee to AtsaPBX, and no obligation to use any particular provider.** A low-volume partner will find a commercial API cheaper; a high-volume partner will find self-hosting cheaper. The ability to choose, and to change, is the differentiator.

---

## 5. Stakeholders

| Who | What they need | They judge us on |
|---|---|---|
| **Partner principal** | A platform to build a business on | Margin, renewal economics, whether their customers stay |
| **Partner engineer** | To install, configure and support it | Whether they can resolve a problem without calling us |
| **Partner developer** | To integrate with their own systems | Whether the API is complete and stable |
| **End customer — CFO** | Predictable cost that stops scaling with headcount | Whether the telephony line falls |
| **End customer — IT lead** | A system their team can run | How often it wakes someone up |
| **End customer — security** | Minimal exposure, provable data control | Passing assessment first time |
| **Contact-centre manager** | Agent productivity and regulatory safety | Talk time per agent; never receiving a regulator's letter |
| **Agent** | One screen that works | Whether they ever think about the phone |
| **Specialist / interpreter** | Fast, fair work and correct pay | Being paid accurately and on time |
| **Compliance officer** | Consent, retention, emergency calling, residency | Answering an auditor without engineering help |
| **Our own delivery team** | Predictable releases and low support load | Tickets per partner per month |

---

## 6. Competitive position

| | Legacy on-premise PBX | Hosted cloud telephony | Contact-centre software | **AtsaPBX** |
|---|---|---|---|---|
| Pricing basis | Capital plus maintenance | Per user, per month | Per agent, per month | **Per concurrent channel** |
| Cost of growth | Hardware steps | Linear with headcount | Linear with agents | **Flat until capacity is reached** |
| Extension limits | Hardware-bound | Priced per user | Priced per agent | **Unlimited on every licensed edition** (10 on the free tier) |
| Carrier choice | Yours | Usually theirs | Usually theirs | **Yours, several at once, least-cost routed** |
| AI provider choice | None | Theirs | Theirs | **Yours — open or commercial** |
| API completeness | Minimal | Partial | Partial | **Complete — the product is the API** |
| Contact centre included | No | Extra product | Yes | **Yes, one platform** |
| Specialist dispatch | No | No | No | **Yes** |
| Where data lives | On site | Their jurisdiction | Their jurisdiction | **Partner's choice** |
| Operational simplicity | Hard | **Easy — their advantage** | Medium | Medium |

The last row is deliberately unflattering. A comparison in which we win every line reads as marketing and is discounted entirely by technical buyers. Hosted services genuinely win on ease of operation; that is the trade a partner makes in exchange for owning the platform and the margin. Saying so is what makes the rest of the table credible.

---

## 7. Product scope by release

### 7.1 R1.0 — Sellable core

The first release a partner can buy, install and run a business on.

| Capability | Business value |
|---|---|
| WebRTC softphone in the browser, nothing to install | Staff work anywhere; onboarding in minutes |
| Full call handling — hold, transfer, park, conference, presence, hot-desking, voicemail-to-email | The everyday calls customers remember when they fail |
| Desk-phone auto-provisioning | Protects existing hardware; removes a migration blocker |
| BYOT carrier connectivity with least-cost routing and automatic failover | No lock-in, no markup, no single-carrier outage |
| Multi-tenant isolation | One platform serves many customer organisations safely |
| Visual IVR and call-flow builder | Routing changes become a business task, not an engineering ticket |
| Role-based access control and append-only audit logs | "Who changed that?" always has an answer |
| Edge security and fraud protection | Prevents the fastest way to lose money on a phone system |
| Call recording governance — consent, retention, permanent deletion | Compliance without manual policing |
| Tamper-evident call detail records with quality measurement on every call | Partners bill from this data; disputes must be answerable |
| Optional AI-native platform: IVR generation, agent assist, summarisation, quality scoring through any LLM the partner chooses | **AI is optional, per-tenant and provider-agnostic.** Partners choose their AI provider — open or commercial — or choose none; the platform operates fully without AI. We provide the media pipeline and the API. No lock-in, no platform surcharge. AI is not a separate product; it is woven through the platform for those who want it. |
| API telemetry, events and webhooks | Partners build portals, billing and automation on top |
| Cryptographic licensing with capacity enforcement, and a permanent free tier | Protects licence revenue without ever disabling a system, and lets an engineer prove an install before anyone buys (§10.4) |
| Per-tenant module enablement within the installation's entitlement | An operator turns a module on for one customer and not another, without buying twice (BR-17, BR-LIC-03) |

### 7.2 R1.1 — Enterprise edge

| Capability | Business value |
|---|---|
| Native mobile applications with push notification | A mobile becomes a real extension, not a diverted number |
| Dynamic emergency calling (E911 / 112) | Legally required in most markets — a go-live gate, not a feature |
| Fax to PDF and email | Unlocks healthcare, legal and government buyers |
| Bulk migration and import tooling | Migration from an incumbent system becomes affordable |

### 7.3 R2 — Contact centre and CRM

| Capability | Business value |
|---|---|
| Predictive and power outbound dialling | Agent talk time rises from roughly 15–20 minutes per hour to 45 or more |
| Answering-machine detection with message drop | Agents stop spending a third of the day on voicemail greetings |
| Campaign management, lists, outcomes, callbacks | Campaign managers work without an analyst |
| Automated compliance — do-not-call, calling hours, abandonment limits | Prevents fines that can exceed the platform's cost |
| Supervisor workspace — listen, whisper, barge, wallboards | Coaching in real conversations; escalations rescued |
| CRM and ERP connectors, bi-directional | Call activity lands where the work happens |
| Embeddable CTI widget for any web application | Works with in-house systems we have never seen |
| Live agent assist and automated after-call work | New agents perform sooner; after-call admin largely disappears |

### 7.4 R3 — Specialist and OPI platform

| Capability | Business value |
|---|---|
| Language, dialect and skill matching | The right specialist, not merely an available one |
| Sub-15-second simultaneous broadcast dispatch | Replaces a coordinator phoning down a list at 60–180 seconds |
| Availability tracking with automatic tier escalation | Fill rate rises; no request silently dies |
| Department PIN identification and budget tracking | Correct attribution to the correct budget, automatically |
| Per-second margin ledger | Revenue, specialist cost and profit visible per call |
| Optional turn-key rating and invoicing module | For partners who would rather buy billing than build it |

---

## 8. Functional business requirements

### Release 1.0

- **FBR-R1-01 — Browser calling and core telephony.** Staff shall make and receive calls from a web browser with no installation, including hold, transfer, park, conference, presence, hot-desking and voicemail-to-email.
- **FBR-R1-02 — Desk-phone provisioning.** The platform shall configure supported desk phones automatically and securely, without an engineer visiting each device.
- **FBR-R1-03 — Carrier neutrality and least-cost routing.** The platform shall connect to any standards-compliant carrier through guided setup, use several carriers simultaneously, route each call by lowest cost, and switch away from a failing carrier without human intervention.
- **FBR-R1-04 — Multi-tenant isolation.** Data, configuration and media shall be completely isolated between customer organisations, and configuration changes shall take effect without interrupting service.
- **FBR-R1-05 — Edge security and fraud control.** The platform shall limit call rates, detect and block fraudulent calling patterns, hide internal topology, and restrict administrative access to authenticated identities regardless of network location.
- **FBR-R1-06 — AI-assisted visual call-flow and IVR builder.** Administrators shall build multi-level menus, business-hours and holiday rules and department routing themselves, with versioning and rollback, without engineering help. The platform may generate an initial IVR tree or call-flow from a natural-language description supplied by the administrator, using an LLM accessed through the same provider choice defined in FBR-R1-07 **if AI is enabled**. A generated flow is a starting point — the administrator remains in control, with the ability to edit, test, version and roll back. **The visual builder works fully without AI; generation is an accelerator, not a requirement.** The flow-generation capability shall be exposed through the public API, so partners can embed IVR creation into their own customer portals, provisioning systems and automated workflows.
- **FBR-R1-07 — Open AI engine choice (optional).** The platform shall stream live call audio to any AI service specified by the partner or tenant — self-hosted open-source models or commercial APIs — selected by configuration and switchable without a code change or release. **AI is optional: the platform operates fully without any AI provider configured.** No platform surcharge, per-minute fee or approved-provider list applies. Streaming shall cause no measurable degradation of call quality when enabled. The partner holds and pays for the provider account.
- **FBR-R1-08 — Access control and audit.** Permissions shall be granular by role and scope, and every administrative action shall be recorded in an append-only, attributable audit log.
- **FBR-R1-09 — Recording governance.** Recording shall follow rules set per customer, department and jurisdiction, including any required announcement, with defined retention, permanent deletion, and an audit trail of every access.
- **FBR-R1-10 — Tamper-evident records and quality measurement.** Every call shall produce a complete, immutable record and a measured quality score for each participant, so that any complaint or billing dispute can be resolved with evidence.
- **FBR-R1-11 — API-first extensibility.** **100% of core signalling events, call state changes, channel telemetry, usage records, administrative controls and AI-generated flow outputs (where AI is enabled) shall be available through documented REST APIs, gRPC streams and webhooks**, with no capability reachable only through our own interface. Every administrative action — including IVR creation and call-flow generation — is available through the same endpoints our own console uses. Data shall be granular enough for a partner to run their own rating and invoicing — per participant, per second, with direction, destination, duration, answer state, tenant, department and service type — and to drive external billing stacks and ERP workflows. Partners embed these capabilities into their own portals, provisioning systems, customer self-service interfaces and automation. **The API works fully without AI; AI outputs are additional data on top of an already complete API.** *We publish and support the interfaces; we do not build or maintain connectors to specific billing products.*
- **FBR-R1-12 — Cryptographic licence security and the Free Community tier.** Licences shall be validated from a cryptographically signed payload bound to a hardware fingerprint, with a daily entitlement check and a defined offline grace period. A perpetual free tier shall be available to any registered user, activated by a signed key like any other; an installation with no key applied shall present an administration surface but no call path, and an expired or grace-elapsed licence shall degrade to the free tier's floor rather than being disabled. Full requirement in §10, including the tier definitions in §10.4.
- **FBR-R1-13 — Deployment and lifecycle.** The platform shall install on a partner's own infrastructure from documented media, upgrade without loss of configuration or data, roll back a failed upgrade, and produce a diagnostic bundle for remote support.

### Release 1.1

- **FBR-R1-14 — Mobile applications.** Native applications with reliable push notification, so a mobile device functions as a full extension.
- **FBR-R1-15 — Emergency calling.** The platform shall route emergency calls with accurate, dynamically maintained location for remote users, in accordance with the obligations of each jurisdiction. Where an obligation cannot be met, emergency dialling shall be blocked and the limitation disclosed prominently. There is no third option, and this is a go-live gate.
- **FBR-R1-16 — Fax.** Inbound and outbound fax delivered as PDF by email, over T.38 with G.711 fallback and no fax hardware. **Included in every licensed edition, not sold separately** — see §12.4.
- **FBR-R1-17 — Migration and exit.** Bulk import of users, numbers and routing from a previous system, and export of all data in open formats.

### Release 2

- **FBR-R2-01 — Outbound dialling.** Predictive, power and preview dialling with adaptive pacing, campaign and list management, outcomes and automatic re-attempt rules.
- **FBR-R2-02 — Answering-machine detection.** Automatic differentiation of humans from voicemail at high accuracy, optimised for a low rate of humans wrongly classified as machines.
- **FBR-R2-03 — Outbound compliance.** Do-not-call lists, permitted calling hours and abandonment limits enforced automatically by the platform — including on manual dials — and evidenced to an auditor on demand.
- **FBR-R2-04 — Supervisor workspace.** Live listen, private coaching, barge-in, live wallboards and drag-to-transfer call control.
- **FBR-R2-05 — CRM and ERP connectivity.** Bi-directional integration with standard CRM and ERP platforms, configurable by an administrator rather than a developer, plus embeddable call-control widgets for any third-party web application. The objective stated in business terms: **an agent should never re-key information that already exists in another system.**
- **FBR-R2-06 — Live agent assist (optional AI).** Real-time transcription driving contextual prompts, suggested responses and sentiment indication to the agent. **These features require AI to be enabled for that tenant. Agents operate fully without them.**
- **FBR-R2-07 — Automated after-call work (optional AI).** Where enabled, the platform shall automatically transcribe, summarise and disposition calls, writing structured records into the connected CRM or ERP. **This feature is optional and requires AI to be enabled.** Without AI, agents complete after-call work manually through the interface — the platform does not depend on AI for core operation.

### Release 3

- **FBR-R3-01 — Specialist matching.** Requests matched by language pair, dialect and subject expertise, with configurable tiers.
- **FBR-R3-02 — Sub-15-second broadcast dispatch.** The caller is placed on hold, qualified specialists are contacted **simultaneously**, and the first to accept is bridged into a three-way call in under 15 seconds. Simultaneous contact is what makes the target achievable; tiered roll-over exists as the escalation path only.
- **FBR-R3-03 — Availability and escalation.** Live specialist state, configurable ring timeout, automatic escalation to the next tier, coordinator fallback, and support for both on-demand and scheduled assignments.
- **FBR-R3-04 — Department PIN and budget tracking.** Callers identify their organisation and department by PIN or speech; usage is tracked against allocated budget, and a department over its limit reaches a person rather than a chargeable specialist.
- **FBR-R3-05 — Per-second margin ledger.** Revenue and specialist cost calculated per second in a tamper-evident ledger, with margin visible per call, per client, per language and per specialist.
- **FBR-R3-06 — Optional rating and invoicing module.** A turn-key client rating and invoicing add-on for partners who prefer to buy billing rather than build it on the API.
- **FBR-R3-07 — Governance and residency.** Data pinned to a chosen jurisdiction, automated spend caps, and audit analytics.
- **FBR-R3-08 — Live translation overlay and separated recording (optional AI).** The platform shall record and transcribe caller and specialist audio on separate channels, presenting real-time translated text and custom glossary hints to the specialist. **This feature requires AI to be enabled and is optional.** Separated recording itself works without AI; only the transcription and translation overlay depend on it.
- **FBR-R3-09 — Automated quality and script compliance (optional AI).** Where enabled, automated evaluation shall assess 100% of recorded calls for script adherence, talk-over, silence gaps and compliance standards. **This feature requires AI to be enabled and is optional.** Without it, quality review is performed manually on a sample, as it is today.

> **Note on requirements classification.** These requirements include end-user features, platform capabilities, API contracts, licensing enforcement and compliance obligations. A partner's end customer experiences the features — browser calling, IVR, outbound dialling, specialist dispatch — directly. The platform capabilities — multi-tenancy, carrier routing, audit, deployment — are what makes the product sellable and supportable. The API is the product for partners who build on it. AI is optional throughout.

---

## 9. Business rules

- **BR-01** No call is connected for an unauthenticated party. Anonymous access is refused everywhere.
- **BR-02** Call audio is protected in transit at all times, in every configuration.
- **BR-03** Where the law requires notification before recording, the announcement completes before recording begins.
- **BR-04** Every recording access is logged with the person, time and reason.
- **BR-05 — Licence capacity enforcement.** The engine maintains an atomic, shared counter of concurrent channels. Exceeding licensed capacity **gracefully rejects new call setups with a standard SIP response**, records the rejection in telemetry under a distinct reason code, and alerts the administrator. **Calls in progress are never dropped for any capacity or spending limit.**
  - *Response-code guidance for implementation: use `503 Service Unavailable` with `Retry-After` for capacity exhaustion — it is semantically correct for resource limits and allows a carrier to reroute — and reserve `486 Busy Here` for a genuinely busy destination. Using `486` for a licence limit misreports the cause to the carrier, distorts the partner's answer-seizure statistics, and hides the reason when they investigate.*
- **BR-06** Only telephone numbers the customer owns or is authorised to present may be displayed to the called party.
- **BR-07 — Usage telemetry integrity.** Call detail records, AI stream duration logs, webhook deliveries and usage counters are **immutable, append-only and exportable through the API**. They are never edited; corrections are separate, attributable adjustments. Partners bill their customers from this data, so its integrity is a commercial obligation as much as a technical one.
- **BR-08** A failing carrier is removed from service automatically and the operator alerted.
- **BR-09** Emergency numbers bypass menus, routing rules, spending blocks and every licence state. This path cannot be gated.
- **BR-10** No campaign call is placed to a number on any applicable do-not-call list, or outside permitted local hours — including a manual dial by an agent.
- **BR-11** Retention and deletion are enforced by the system, not by a person remembering.
- **BR-12** Billing calculations are performed once and are repeatable: re-running a period produces identical results.
- **BR-13** A specialist assignment is accepted by exactly one specialist. Two people are never billed for one session.
- **BR-14** Live call audio leaves the platform for AI processing only where the customer has explicitly enabled it, and only to a provider they have specified.
- **BR-15 — Credential isolation.** AI provider keys, carrier credentials and any other secret supplied by a partner or tenant are stored encrypted, scoped strictly to that partner or tenant, and never readable by another tenant, another partner, or by our staff in plain text. They never appear in logs, diagnostic bundles, exports or error messages, and are revocable and rotatable by their owner without our involvement.
- **BR-16** Every platform capability is reachable through the public API. A function that exists only in our own interface means the API is incomplete and the feature is not finished.
- **BR-LIC-01 to BR-LIC-03 — Setup and the Free Community tier, the degradation floor, and edition-based module gating.** Stated in full in §10.4, where the market rationale that produced them belongs with them.
- **BR-17 — AI is optional and per-tenant.** The platform must operate fully and natively with no AI enabled. All core telephony, routing, queuing, recording, reporting and API functionality must work without any LLM installed, AI agent connected, or external AI provider configured. One tenant may use AI while another on the same deployment does not. AI is an enhancement, never a dependency.

---

## 10. Licensing and anti-bypass security

Licence revenue is the entire business. The enforcement mechanism must be strong enough to prevent casual copying and capacity under-buying, and restrained enough that it never damages a partner's service.

### 10.1 Requirement

**FBR-R1-12** — Licence validation shall use a **cryptographically signed payload** carrying edition, channel capacity, expiry and instance identity, bound to a **weighted hardware fingerprint** derived from host identifiers. The fingerprint is tolerant of changes in virtualised environments — see §10.3. The platform shall perform an **automated daily entitlement check** with a **7-day offline grace period**.

### 10.2 Enforcement behaviour

| Situation | Behaviour |
|---|---|
| Concurrent channels exceeded | New call setup rejected with a standard SIP response and a distinct telemetry reason code. **Active calls continue.** A short burst allowance prevents customers being penalised for a busy hour. |
| Daily check unreachable | Full function for 7 days, with escalating administrator warnings **from day 2** |
| Grace expired | Degrade to the **Free Community floor of 4 simultaneous calls** (§10.4) — **never a full shutdown of a working phone system** |
| Tampering detected — including an edited entitlement row | Log, alert, degrade to the same 4-call floor. Never disable, and never honour what the tampered value claimed (§10.6) |
| No licence ever applied | **Setup state** (§10.4, §10.5). Administration only, no call path. A pre-provisioning state, not a licence state, and never the target of degradation |
| **Emergency calls** | **Connect in every licence state — valid, expired, degraded, over-capacity or tampered.** Verified by test in every release. |

### 10.4 Setup and the Free Community tier — a hybrid of the two market benchmarks

The two dominant commercial platforms monetise differently, and each
solves a problem the other does not. We take the useful half of both.

| Benchmark | Their free tier | Their monetisation | What we take |
|---|---|---|---|
| **3CX** | 4 simultaneous calls, capped extensions, no key required | Channel capacity increments plus edition tiers | **The capacity floor.** A zero-configuration install that works out of the box, and a floor to fall back to rather than a shutdown |
| **VitalPBX** | Core PBX free, channels bound only by hardware | Modular add-ons and feature gating | **Module gating.** Core telephony stays open; advanced modules are unlocked by edition |

**BR-LIC-01 — A perpetual Free Community tier, activated by a signed key
(D-52).** Every entitlement, **including the free one**, arrives as a
cryptographically signed token obtained by registering on our portal.
The **Free Community** tier is perpetual and costs nothing: **4
simultaneous calls, a maximum of 10 extensions, and a single tenant**.

Before any token is applied the installation is in **Setup**: the
administration console works and displays the instance ID and hardware
fingerprint, but there is no call path. Setup is a pre-provisioning
state, **not a licence state** — see BR-LIC-02.

*Why registration rather than zero-configuration:* the free tier is
free of charge, not free of contact. Requiring a key for every tier means
every deployment is a known contact and every upgrade is a new token
against an instance we already know, rather than a reinstall. The
comparable product does the same, and the friction is small when key
issuance is instant and self-service (§10.5).

*Why the caps:* an engineer must be able to prove an install works before
anyone commits to buying (AC-09.1), and the cap is what keeps that an
evaluation rather than a product — 4 channels, 10 extensions and one
tenant cannot run a business, which is the point.

**BR-LIC-02 — Degradation falls back to the Free Community floor, never
to Setup.** Where a licence expires, its 7-day offline grace elapses, or
tampering is detected, capacity shall fall back to **4 simultaneous
calls** rather than disabling telephony. **Calls in progress are never
dropped** at the transition (BR-05), and **emergency calls always
connect** (BR-09).

*One cause, one behaviour:* expiry, elapsed grace and tampering are three
routes to the same floor, distinguished by the reason reported to the
administrator, not by three different degraded states.

*And one distinction that must never be collapsed (D-52):* **Setup is not
a degraded state.** A never-activated installation has no phone system to
protect; a degraded one has a live system with calls in progress. If
degradation ever fell back to Setup, an expired licence would disable a
working phone system — the one outcome this product promises never to
produce. The floor is therefore a fixed 4 simultaneous calls, held
independently of whether any token was ever applied.

**BR-LIC-03 — Modules are entitled by edition.** Advanced modules shall
check their entitlement against the edition carried in the signed licence
payload. Gating is by edition, not by a separate module-key mechanism.

| Gated by edition | Never gated on any licensed edition |
|---|---|
| AI media pipeline (AI add-on) | Multi-tenancy |
| Predictive and power dialling (Contact Centre) | Core call routing and basic IVR |
| Call recording and webhooks (Contact Centre) | **Emergency calling (BR-09)** |
| Specialist dispatch (Language Services) | Administration, the API, and data export |
| White-label and custom branding (Operator) | Fax (FBR-R1-16) |

**Multi-tenancy is never sold as a module and never differs between
licensed editions.** It is what the product *is* (§1), it is native to
the engine rather than a bolt-on — one of the few places we differ
structurally from the module-catalogue benchmark — and the Operator
pooled-channel licence in §12.2 is sold on it.

**The Free Community tier is single-tenant.** That is the one boundary
tenancy carries, and it is deliberate: it makes the free tier an
evaluation of the product rather than a small operator business run for
nothing, and it protects the Operator licence, which is the only place
tenancy is monetised. Every licensed edition, including the smallest, is
multi-tenant with no tenant count limit.

**The cap is a quantity on the licence, not a feature that can be
absent** (D-51). The signed payload carries **`MaxTenants`** beside the
channel count: `1` for Free Community, `0` for unlimited on every licensed
edition. Tenant isolation itself is enforced by row-level security on
every table, in **every** mode including single-tenant — one schema, one
code path, no single-tenant build. `MaxTenants` constrains who may
*provision* a tenant; it never changes how isolation works, and it is
never a reason to omit a `tenant_id`.

**Reaching the cap refuses the creation, not the caller.** The
administrator has the permission; the installation lacks the entitlement,
and the refusal says so — reads are unaffected, since a single-tenant
installation still has one tenant its console must be able to show.

**BR-LIC-03 applies per tenant, within the installation's
entitlement** (D-50). An operator entitled to a module may switch it on
for one customer and not another; what they may never do is switch on
something the installation was not entitled to. That check is answered in
one place, and it is answered by the service performing the operation —
never by the console (§16 R-12).

**Administration and the API remain available in every state**, including
Setup and degraded, so that a partner can always resolve the
situation from the system itself rather than being locked out of the
thing they need to fix.

### 10.3 Two operational safeguards that must be designed in

**Hardware fingerprinting breaks in virtualised environments, and false lockouts are the single largest support-cost driver in licensed software.** A partner's cloud instance is rebuilt and the network address changes; a virtual machine migrates and the host identifier changes — and a working phone system stops taking calls for a reason the partner cannot diagnose. The fingerprint must therefore be **weighted across several attributes with a tolerance** (for example, three of five must match), permit a limited number of **self-service reactivations** without contacting us, and treat a mismatch as a warning and a re-check rather than an immediate hard failure.

**A 7-day grace makes our entitlement service critical infrastructure.** If it is unreachable for eight days — our outage, their firewall change, a certificate expiry, a DNS fault — every partner in the field begins degrading at once. That is a self-inflicted mass outage, and a far worse business event than some licence leakage. It requires high availability and a public status page for the entitlement service, warnings to administrators from day 2, a documented emergency extension procedure for when the fault is ours, and a **manually issued long-term offline licence** for air-gapped, government and regulated deployments where a 7-day grace is not viable.

**On piracy generally:** enforcement should stop casual over-use, not defeat a determined attacker. Software that runs on a partner's own hardware can eventually be circumvented, and effort spent hardening beyond reasonable measures is effort not spent on product. The durable commercial defences are updates, support, certification and directory listing — the things legitimate partners want and unlicensed users cannot get.

---

### 10.5 Acquisition and activation (D-52)

Registration is required for every tier, and issuance is instant and
self-service. The path is identical for a free key and a purchased one,
which is what keeps the free path low-friction and the upgrade path
trivial.

| Step | Where | What happens |
|---|---|---|
| 1 | The deployment | Boots into **Setup**. The console shows the instance ID and hardware fingerprint, and how to register |
| 2 | Our portal | The administrator registers — name, work email, organisation — and chooses Free Community or a commercial edition |
| 3 | Our portal | Instance ID and fingerprint are entered; a signed token is issued carrying edition, capacity, `MaxTenants`, `MaxExtensions`, expiry and instance identity |
| 4 | The deployment | The token is pasted or uploaded. Signature and 3-of-5 fingerprint are verified, and the entitlement takes effect **with no restart** |

**Upgrading is a new token, never a reinstall.** The portal already holds
the instance ID, so Free → commercial is a key application against a
system that keeps running.

**Supported deployment artefacts.** R1.0 ships **OCI container images**
(`atsapbx/*`) run under Docker Compose or Helm — cloud, Kubernetes, and
DevOps-managed infrastructure. A **turnkey Linux appliance** (ISO, AMI,
OVA) for on-premises hardware and hypervisors is **R1.1**: an appliance
build pipeline, an OS patching path and appliance QA are real work and
are not in §12.3's estimate. Activation is identical on both.

*One consequence worth stating for the appliance case:* a cloud instance
rebuild changes MAC address and host UUID, so the weighted 3-of-5
fingerprint tolerance (§10.3) is what stops an AMI redeploy from
presenting as a different machine. It is load-bearing there, not a
nicety.

### 10.6 The stored entitlement is the signed token (D-53)

**The entitlement of record is the signed payload itself**, re-verified
when loaded — not a row of parsed values. The readable claim columns
exist for display and support, and are never consulted to decide what an
installation may do.

This closes an exposure that the cryptography otherwise left open. The
platform runs on the partner's hardware, so they hold the database. If
the entitlement were a plain row, a single `UPDATE` would grant any
capacity, any edition and any tenant count — without forging a signature,
touching a binary, or defeating the fingerprint. Verifying only at the
moment a key is applied protects the *delivery* of a licence and nothing
about how it is *kept*.

Consistent with §10.3, this is proportionate rather than an arms race: it
stops a partner editing a row to take twenty channels, which is the
realistic behaviour. It does not stop someone patching the binary, and no
reasonable measure would. A row that fails verification degrades to the
4-call floor and is reported as tampering — it is never honoured, and it
never disables the system.

**There is no development or testing bypass** (D-54). Verification runs
in every build; what differs is which keys a build trusts. A development
key is compiled into development builds only, so a token that unlocks an
engineer's laptop is inert against a released binary. This matters
commercially as much as technically: a "disable licensing" switch of any
kind, however well hidden, is the first thing found and shared, and it
would make every other control in this section decorative.

---

## 11. Service-level and quality expectations

| Expectation | Commitment |
|---|---|
| **Platform availability** | 99.95% monthly for services we operate, measured by independent test calls. End-to-end availability at a partner site depends on their infrastructure and carriers, and the agreement says so. |
| **AI availability** | The platform's core telephony is unaffected by AI availability. AI features are optional and per-tenant. If an AI provider is unreachable, the call continues without AI features. **No call is dropped because AI is unavailable.** |
| **Entitlement service availability** | 99.95% monthly, with a public status page. A 7-day grace makes this critical infrastructure. |
| **Conversation quality** | One-way audio delay under 150 ms within a region, and under 200 ms for three-way specialist calls, at the 95th percentile. |
| **Connection speed** | Internal calls connect in under one second. A specialist is connected in under 15 seconds. |
| **Capacity** | Launch capacity 500 concurrent channels per deployment, scaling to 50,000 per region without redesign. |
| **Data protection** | All call audio and signalling encrypted in transit; all stored data encrypted; no credential stored in readable form. |
| **Regulatory alignment** | Designed to support payment-card scope reduction, healthcare-grade handling of recordings, and emergency-calling obligations. Certification and the agreements it requires are a separate, funded programme, not a product feature. |
| **Investigation** | Any quality or billing dispute investigable with evidence within 10 minutes. |
| **Change** | Routine configuration changes take effect immediately without interrupting service. |
| **Support** | Tier-1 support is the partner's. Our response commitments run to partners only, by partner tier. |

---

## 12. Commercial model and business case

### 12.1 How AtsaPBX earns revenue

| # | Revenue line | Basis |
|---|---|---|
| 1 | **Concurrent-channel software licence** | Channels × edition, annual subscription. Never per user, never per extension. |
| 2 | **Annual support and maintenance** | Percentage of licence value, by partner tier. Updates, security fixes, tier-2 and tier-3 support. |
| 3 | **Customisation and professional services** | Day rate — integrations, migrations, bespoke workflows, deployment assistance |
| 4 | **Certification and training** | Per seat. Not a profit centre; it exists so unqualified partners do not generate support costs exceeding their licence value. |
| 5 | **Sellable modules** | White-label and branding, the operator switchboard as an add-on to Core, and the AI voice agent by concurrent AI channel. Catalogue and rationale in §12.4. Never core telephony, never tenancy, never security. |

Optional later: the R3 turn-key rating and invoicing module, and metered AI resale within our own limited SaaS only, where the meter is ours.

**AtsaPBX carries zero direct financial liability for third-party consumption.** Carrier minutes are bought by the partner on their own contract. AI inference is bought by the partner on their own account and API key. We never sit in either payment chain, never mark either up, and neither appears in our cost of goods sold. **Our costs are fixed while our partners' costs are variable** — a partner tripling their traffic triples their carrier and AI bills and pays us nothing more until they cross a channel tier.

> *One qualification for sales to understand: zero liability is a statement about money, not about duty. Where a partner streams call audio to an AI provider we are generally a processor in that chain and carry data-protection obligations regardless of who pays the invoice; and having built the emergency-calling capability, we cannot disclaim whether it works. Both are covered in §15 and in the partner agreement.*

### 12.2 Editions and indicative pricing

Four editions, cumulative: **Core** (business PBX), **Contact Centre**, **Language Services**, and an **AI add-on** available with any of them. Below them sits **Free Community** (§10.4) — perpetual, costs nothing, still activated by a signed key. **Setup** is not an edition at all: it is the pre-activation state of a freshly booted installation (D-52).

| | Setup | Free Community | Core | Contact Centre | Language Services | AI add-on | Operator |
|---|---|---|---|---|---|---|---|
| Licence key | **none yet** | signed, free | signed | signed | signed | signed | signed |
| Simultaneous calls | **none — no call path** | **4** | purchased | purchased | purchased | — | pooled |
| Extensions | — | **10** | unlimited | unlimited | unlimited | — | unlimited |
| Tenants (`MaxTenants`, D-51) | — | **1** | unlimited (`0`) | unlimited (`0`) | unlimited (`0`) | — | unlimited (`0`) |
| Administration console | **yes** | yes | yes | yes | yes | — | yes |
| Core routing, basic IVR | — | yes | yes | yes | yes | — | yes |
| **Emergency calling** | — | **yes** | **yes** | **yes** | **yes** | — | **yes** |
| Fax (FBR-R1-16) | — | — | yes | yes | yes | — | yes |
| Trunks | — | single | multi-trunk, failover | + least-cost routing | + least-cost routing | — | + least-cost routing |
| SIP intrusion protection | yes | yes | yes | yes | yes | — | yes |
| Recording, webhooks | — | — | — | yes | yes | — | yes |
| Operator switchboard | — | — | add-on | yes | yes | — | yes |
| Queue callback | — | — | — | yes | yes | — | yes |
| Predictive and power dialling | — | — | — | yes | yes | — | yes |
| Specialist dispatch | — | — | — | — | yes | — | — |
| AI media pipeline | — | — | — | — | — | yes | add-on |
| White-label and branding | — | — | — | — | — | — | **yes** |

**Setup has no call path, and that is why it is not the degradation
floor.** An expired or grace-elapsed licence falls back to Free
Community's 4 simultaneous calls (BR-LIC-02), never to Setup — a working
phone system is never disabled by a licence state. The individually
sellable modules
in that table are described in §12.4.

**AI add-on.** Provides access to the full AI media pipeline: streaming audio to the partner's chosen provider, and receiving transcription, summarisation, sentiment and generated IVR flows. **The add-on is optional — the platform operates fully without it.** We do not mark up inference; the partner pays their provider directly. What the add-on sells is the *pipeline and orchestration*, not the model. Partners may enable AI for some tenants, all tenants, or none.

| Concurrent channels | Core | Contact Centre | Language Services | AI add-on |
|---|---|---|---|---|
| 16 | $560 | $890 | — | +$320 |
| 32 | $980 | $1,590 | $1,990 | +$560 |
| 64 | $1,890 | $2,690 | $3,390 | +$980 |
| 128 | $3,590 | $4,990 | $6,290 | +$1,800 |
| 256 | $6,490 | $8,990 | $11,490 | +$3,200 |
| 512 | $11,900 | $16,900 | $21,900 | +$5,800 |

**Unlimited extensions on every licensed edition.** Partners running many customer organisations buy a flat pooled-channel Operator licence instead — $6,900 for 128 pooled channels through $34,900 for 1,024, with unlimited tenants. Partner discounts run 20–35% by tier with deal registration. Full price list, partner programme and support entitlements are in the Commercial Model annex.

### 12.3 Investment and returns

| | R1.0 | R1.1 | R2 | R3 |
|---|---|---|---|---|
| Engineering effort (hours) | 3,000–4,000 | 1,500–2,200 | 3,500–4,800 | 2,500–3,500 |
| Elapsed with a 5–6 person team | 5–7 months | 3–4 months | 6–8 months | 4–6 months |

Channel-business workstreams — licence issuance and enforcement, installer and upgrade path, diagnostics tooling, partner portal, certification — add a further **2,100–3,100 hours**, and most of it must exist *before the first sale*.

**Total programme: approximately 12,600–17,600 hours, $380k–530k at a blended rate, over 20–28 months.** Running costs during build and pilot are $1.5k–4k monthly.

**Revenue expectation:** 8 partners in year one (~$19k net of discount), 30 in year two (~$92k), 75 in year three (~$270k), plus maintenance renewals at 85–90% retention, services and certification.

**Break-even arrives in year three at roughly 75–110 active partners.** A licensed channel business ramps more slowly than direct sales and compounds far better — partner three costs a fraction of partner one to acquire, and each partner brings many end customers. **Year one revenue will not cover the build. Fund 24–30 months.**

### 12.4 Sellable modules — what we take from the module-catalogue model, and what we refuse

The module-catalogue benchmark (VitalPBX) monetises by unbundling: the
core PBX is free, and multi-tenancy, recording, branding and a
switchboard are each a separate paid module. That model funds the product
but fragments it, and a buyer discovers the real price only after
assembling the list.

**Our rule: never unbundle the engine; monetise what sits on top of it.**

| Their module | Our position | Why |
|---|---|---|
| Multi-tenant | **Native, never sold separately** | It is the product (§1). Tenant isolation is enforced in the database itself, not by an add-on that could be absent — a security property cannot be an optional purchase. Monetised through the Operator licence instead |
| Call recording | **In Contact Centre** | A compliance obligation for the buyers who need it; pricing it separately prices compliance |
| Rebranding / white label | **Adopt as a module** | Genuine upsell with high willingness to pay, and it changes nothing about how calls work. §12.1 revenue line 5 |
| Switchboard console | **Adopt, in Contact Centre** | Extends the R2 supervisor workspace to receptionists and dispatchers |
| Queue callback | **Adopt, in Contact Centre** | Directly reduces abandon rate; the clearest value story in the catalogue |
| Geo firewall / intrusion protection | **Native, never sold separately** | Same reasoning as tenancy: a platform that can be brute-forced unless you buy the protection module is not secure, it is negotiable |
| Virtual fax (T.38) | **Included, already committed** | FBR-R1-16 promises it in R1.1. Charging later for a promised feature is the repackaging partners remember |
| Real-time AI translation | **Ours; they have nothing comparable** | §12.4.2 |

#### 12.4.1 Modules we will sell

**White-label and branding suite — Operator licence.** Custom domain,
logo and favicon, theme, notification templates, and softphone skinning,
so an ITSP or MSP sells the platform as their own. Included with the
Operator licence and available as an add-on to a single-tenant
enterprise deployment. It is the natural companion to pooled channels:
the partners who want many tenants are the partners who want their own
brand on them.

**Operator switchboard console — Contact Centre, or an add-on to Core.**
A live console for receptionists, dispatchers and supervisors: call state
across departments, drag-and-drop blind and attended transfer, queue
monitor, agent pause, and listen / whisper / barge. The supervisor half
is already committed in R2 (§7.3); this extends the same console to the
receptionist role and makes it sellable to a Core customer who needs a
front desk but not a contact centre.

**Smart queue callback — Contact Centre.** A caller keeps their place in
the queue and hangs up; the platform dials them back when an agent frees.
With wait-time triggers and sticky routing back to the agent they last
spoke to.

**SIP intrusion protection — included, not sold.** Registration
brute-force detection, rate limiting and geographic restriction, in every
edition including Free Community. Listed here because the benchmark sells
it and we deliberately do not: see the table above.

#### 12.4.2 AI modules — and how they relate to the human specialist business

The AI add-on already covers transcription, summarisation, sentiment and
generated IVR flows (§12.2). Two additions are worth stating explicitly
because they are where we have no equivalent competitor.

**Real-time translation pipeline — Language Services with the AI
add-on.** Bidirectional speech-to-text, translation and speech synthesis
into a live call, with a running transcript.

**This augments the specialist dispatch business; it does not replace
it.** The relationship is deliberate and needs to be understood by anyone
selling either:

- AI **bridges the wait** — a caller is understood from the first second
  rather than after a specialist is found and connected.
- AI **covers what dispatch cannot** — rare language pairs where no
  qualified specialist is available at that moment.
- The **human remains the product** for regulated, medical, legal and
  safeguarding work, where accuracy carries liability and a machine
  transcript is not an acceptable record.

So AI raises the fill rate that §7.4 sells and shortens the gap the
coordinator model creates. It does not remove the specialist, and the
per-second margin ledger and availability tiers stay exactly as they are.
A partner buys Language Services for the specialists and adds AI to make
the wait for one survivable.

**AI voice agent / virtual receptionist — separate consumption add-on.**
Conversational intent handling, appointment booking, and API lookups
during a live call, before or instead of routing to a person. Priced by
concurrent AI channel rather than by edition, because its cost driver is
inference concurrency, not the size of the phone system. **It is an
IVR alternative, never an IVR replacement in the emergency path:** BR-09
applies unchanged, and an AI agent is never in the way of an emergency
call.

**All of it remains optional (BR-17).** Every module in §12.4 can be
absent and the platform is complete without it.

---

## 13. Success measures

| Measure | Target | Reviewed |
|---|---|---|
| Active partners | 8 by month 12, 30 by month 24, 75 by month 36 | Quarterly |
| Maintenance renewal rate | ≥85% | Quarterly |
| Partners using the API beyond our console | ≥50% by month 18 — *the best available predictor of retention* | Quarterly |
| Support tickets per partner per month | ≤2 — *rising numbers mean documentation or certification is failing* | Monthly |
| Licence false-lockout incidents | Zero | Monthly |
| Entitlement service availability | ≥99.95% | Monthly |
| Platform availability | ≥99.95% | Monthly |
| Calls connected successfully | ≥99.5% | Daily |
| Calls meeting the quality target | ≥95% | Weekly |
| Time to install a new deployment | ≤60 minutes by a certified engineer | Per install |
| Agent talk time per hour (R2) | ≥45 minutes | Weekly |
| Humans wrongly classified as machines (R2) | ≤2% | Weekly |
| Specialist connect time (R3) | ≤15 seconds median | Daily |
| First-attempt specialist fill rate (R3) | ≥92% | Daily |
| AI feature uptake (optional) | Measured per tenant; no minimum target — AI is optional | Quarterly |
| Regulatory findings | Zero | Quarterly |

---

## 14. Assumptions, dependencies and constraints

**Assumptions.** Partners are capable of operating infrastructure — a prospect who cannot is a SaaS customer, not a partner, and should be sold accordingly · partners hold their own carrier and AI provider relationships · partners provide tier-1 support to their own customers · we can fund a permanent product and support team beyond launch.

**Dependencies.** Carrier onboarding and number transfer for our own reference deployment, commonly four to eight weeks — begin before development · an emergency-location service provider in each launch jurisdiction · legal review of §15 before go-live · our entitlement service funded and operated as production infrastructure, not an internal tool.

**Constraints.** Video conferencing and messaging are outside the scope of R1 to R3 · open-source components must carry licences compatible with commercial distribution, verified before adoption · **the API is a contract with paying businesses from the day the first partner builds on it, and breaking it breaks their business** · real-time voice defects are audible to customers immediately, so testing is not a line item that can be cut.

---

## 15. Regulatory and compliance obligations

Each item requires qualified legal review in every launch jurisdiction before go-live. Where an obligation falls to the partner, it is written into the partner agreement rather than assumed.

| Obligation | Consequence |
|---|---|
| **Emergency calling** | Blocks go-live. Implement with accurate location, or block and disclose. Requires a location-service provider in most markets. The capability is ours; the jurisdictional obligation is the partner's. |
| **Consent to record** | Varies by country and by state. The platform defaults to the strictest applicable rule where the position is unclear. |
| **Outbound calling rules** | Do-not-call registers, permitted hours, abandonment limits, caller identification. Fines accrue per call. Enforced by the platform, not left to supervisors. |
| **Caller identity presentation** | Anti-spoofing regimes apply in several markets. Only owned numbers may be presented. |
| **Data protection** | Recordings and transcripts are personal data and often sensitive. Lawful basis, processing agreements, deletion on request including backups, breach notification. Applies to audio streamed to AI providers regardless of who holds the account. |
| **Data residency** | Frequently contractual rather than statutory, and binding either way. Not to be sold before it is built. |
| **Payment-card handling** | Applies only if card details are spoken on recorded calls. The design intent is to remain outside that scope. |
| **Open-source licensing** | Some licences oblige publication of derived source if used incorrectly. Every component is verified before adoption, and licence scanning runs in the build pipeline. This is a commercial risk, not a technical footnote. |

---

## 16. Risk register

| # | Risk | Severity | Mitigation | Owner |
|---|---|---|---|---|
| R-01 | Telephone fraud on a partner deployment | Critical | Spend caps with automatic suspension, destination restrictions, anomaly alerting, no anonymous access | CTO |
| R-02 | Emergency calling unresolved at go-live | Critical | §15 and FBR-R1-15 as an explicit release gate | Legal / PO |
| R-03 | Outbound campaign breaches calling rules | Critical | Enforcement in the platform (BR-10), evidenced in reporting, tested before every release | PO |
| R-04 | Entitlement service outage degrades every partner at once | Critical | High availability, status page, warnings from day 2, emergency extension procedure, offline licence option (§10.3) | CTO |
| R-05 | Licence false lockouts from hardware fingerprinting | High | Weighted multi-attribute fingerprint with tolerance, self-service reactivation, warn-and-recheck (§10.3) | CTO |
| R-06 | Breaking API change damages partners' businesses | High | Versioned API from day one, published deprecation policy with 12-month notice, no breaking change within a major version | Architect |
| R-07 | Scope — three product lines at once | High | Release gates; the Product Owner owns the boundary | PO |
| R-08 | Partners expect ready-made billing connectors we only expose APIs for | Medium | State the boundary in the partner agreement and sales material; publish reference implementations, not supported connectors | PO |
| R-09 | Partners route end-user support to us | High | Contractual tier-1 obligation, enforced at the support desk, with professional services priced for those who want us to do it | COO |
| R-10 | Knowledge concentrated in one engineer | High | Pairing, written decisions and recorded walkthroughs from month one | CTO |
| R-11 | The free tier cannibalises small paid deals rather than feeding them | Medium | The cap is deliberately below a viable business — 4 calls, 10 extensions, one tenant (§10.4). Track conversion from Free Community to a paid licence as a named measure (§13); if installs sit unconverted the cap is too generous, and it is ours to tighten | PO |
| R-12 | Module gating is enforced in the console but not in the API, so an entitlement is bypassed by calling the endpoint directly | High | Entitlement is checked in the service that performs the operation, never in the interface. AC-06.14 requires the API to refuse with an entitlement reason, and the console shows the same refusal rather than deciding it | Architect |
| R-13 | Free-tier instance farming — ten free deployments of 4 channels is forty free channels, and the hardware fingerprint does not prevent it because each instance is genuinely a different machine | Medium | The control is **portal-side, not platform-side**: free keys are issued per verified organisation with a published limit, and the portal holds every instance ID it has ever issued against, so concentration is visible. Enforcing it in the platform is impossible by construction — each instance is legitimately licensed. Accept that some farming occurs; §10.3's position is that enforcement stops casual over-use, and a partner assembling ten instances to avoid one licence is a sales conversation, not a technical control | PO |
| R-14 | The development signing key reaches a production build, making an unlimited licence mintable by anyone holding it | High | The key is injected at build time and empty by default, so the strict binary is what a forgotten flag produces. A test asserts a default build trusts exactly one key and rejects a development-signed token (D-54) | CTO |
| R-11 | "Zero-cost open-source AI" claim fails on first GPU invoice | Medium | The claim is stated accurately in §4: no fee to us, not free to run | CEO |
| R-12 | Channel conflict if we sell direct against a partner | High | Direct sales policy decided and written into the partner agreement before the first partner signs | CEO |
| R-13 | Specialist supply too thin for a 15-second target (R3) | High | Validate supply before building; configurable wave size and tiering; coordinator fallback | PO |
| R-14 | Under-funding the 24–30 month runway | Critical | Staged funding against release gates; revenue expectations stated honestly in §12.3 | CEO / CFO |
| R-15 | Partner or customer expects AI to be always available and blames the platform when it is not | Medium | Clear statement in documentation and the partner agreement: AI is optional, best-effort and third-party dependent. Core telephony is unaffected by AI outages. | PO |

---

## 17. Release acceptance gates

A release ships only when every gate passes. A gate is not a target to be negotiated at the end of a sprint.

**R1.0** — a partner engineer installs a working deployment unaided in under 60 minutes · calls placed and received from a browser on office, home and mobile networks · two carriers configured with least-cost routing and failover demonstrated · tenant isolation verified by test · an administrator builds an IVR and routes a live call unaided · complete immutable records with quality measurement on every call · recording, retention and deletion demonstrated end to end · audio streamed to two different AI providers, one self-hosted and one commercial, switchable by configuration with no measurable quality loss · **every function demonstrated through the public API with published documentation, and the administration console proven to use only those same endpoints** · licence activation, capacity rejection, offline grace and emergency-call exemption all verified · independent security assessment passed · load tested at 1.5× target capacity.

**R1.1** — emergency calling resolved and signed off by legal in each launch jurisdiction · mobile applications published · fax verified against real-world destinations · migration completed from a real incumbent system.

**R2** — a ten-agent campaign sustains 45 or more minutes of talk time per agent-hour within the abandonment limit · compliance suite passes with zero exceptions · answering-machine detection meets accuracy and false-machine targets · two CRM connectors live with real customers · the call-flow builder used unaided by a non-engineer.

**R3** — median specialist connect time under 15 seconds across 100 live calls · zero double-acceptance across 1,000 simulated races · ledger reconciles to call records to the cent over a full month · residency and budget caps demonstrated.

---

## 18. Decisions required from the launch board

| # | Decision | Recommendation |
|---|---|---|
| 1 | Do we sell direct in competition with partners? | Direct only above a defined deal size, with a referral margin to the partner. **Decide before the partner agreement is drafted — channel conflict discovered later destroys partner trust permanently.** |
| 2 | Launch jurisdictions | Determines emergency-calling obligations, recording-consent rules and residency claims. Needed before R1.1. |
| 3 | How wide does our own SaaS open at launch? | One region, capped capacity, waiting list. It is a proving ground and lead source, not a second business. |
| 4 | Subscription only, or perpetual by exception? | Subscription only on the price list; perpetual by exception at 3.5× annual plus 20% maintenance, for buyers who cannot purchase operating expenditure. |
| 5 | Fax: build or integrate a provider? | Compare build cost against per-page cost before committing R1.1 effort. |
| 6 | Optional rating and invoicing module — build in R3 or partner with an existing billing vendor? | Assess partner demand at 20 partners before committing engineering. |

---

## Appendix A — Glossary

| Term | Meaning |
|---|---|
| **Platform provider** | A software vendor that licenses its product to operators, rather than operating a service itself |
| **BYOT** | Bring Your Own Trunk — the partner chooses their own carrier; we add no per-minute markup |
| **Concurrent channel** | One call in progress. The unit of capacity and of pricing. |
| **Least-cost routing** | Automatically sending each call by the cheapest suitable carrier |
| **Multi-tenancy** | Many customer organisations on one deployment, completely isolated |
| **API-first** | Every capability available as a documented endpoint before it appears in any interface |
| **Webhook** | A notification the platform sends to a partner's system when something happens |
| **Predictive dialling** | The system places calls and connects only answered ones to an agent |
| **Answering-machine detection** | Automatically recognising voicemail so agents are not connected to it |
| **Broadcast dispatch** | Contacting many qualified specialists at once; the first to accept wins |
| **Per-second ledger** | Revenue and cost calculated to the second, recorded immutably |
| **Hardware fingerprint** | A signature derived from a machine's identifiers, binding a licence to an installation |
| **Offline grace** | The period a deployment continues working without reaching our entitlement service |

## Appendix B — Approval

| Role | Name | Signature | Date |
|---|---|---|---|
| Chief Executive Officer | | | |
| Chief Financial Officer | | | |
| Chief Technology Officer | | | |
| Head of Product | | | |
| Head of Sales / Channel | | | |
| Compliance / Legal | | | |
