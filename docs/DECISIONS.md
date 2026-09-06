# AtsaPBX — Decision Log

Decisions already taken, with the reasoning. **Read this before proposing an
alternative** — most obvious suggestions have already been considered and
rejected for reasons that are not visible in the documents themselves.

Append-only. Add new entries at the bottom with a date.

---

## Commercial

**D-01 · We are a software vendor, not an operator.**
Partners deploy on their own infrastructure, bring their own carriers and their
own AI provider keys, and bill their own customers. Keeps our costs fixed while
partner revenue is variable, removes us from carrier and AI billing entirely,
and lets a partner in a market we have never operated in adopt us without
waiting for us to sign anything local.

**D-02 · Pricing is per concurrent channel, with unlimited extensions.**
Per-seat pricing is what we position against. Unlimited extensions is a
deliberate difference from the closest comparable product, whose extension caps
are a recurring complaint. One-line sales message: you pay for how many calls
happen at once, never for how many people you employ.

**D-03 · Two pricing shapes, for two buyers.**
Capacity tiers for direct sales; a flat pooled-channel Operator licence for
partners. A partner running fifty tenants will not accept re-sizing their
licence every time one client hires.

**D-04 · Language Services is priced as a capacity licence, not revenue share.**
In a self-hosted model we cannot see the minutes, so any per-minute share
depends on partner self-reporting — unenforceable in practice, and it poisons
the relationship the day you audit it. Usage-based pricing belongs in our own
SaaS, where the meter is ours.

**D-05 · Partners supply their own AI credentials.**
Removes per-minute inference cost from our margin, satisfies data-residency
requirements, and proves the no-lock-in positioning. We sell the pipeline and
orchestration, not the model.

**D-06 · "Zero-cost open-source AI" is not a claim we make.**
Self-hosted models are free of licence cost, not free of cost — they need GPUs
and someone to run them. The accurate and still-strong claim: no per-minute fee
to us, and no obligation to use any particular provider.

---

## Requirements that were corrected

**D-07 · Availability is 99.95%, not 99.999%.**
Five nines is 26 seconds of downtime a month end to end, including carriers,
cloud and our own deploys. Nobody at this size achieves it, and the number
becomes a contractual liability the day a customer's lawyer reads it.

**D-08 · 50,000 concurrent channels is per region, not per node, and is a
year-three scale point.**
One media node handles hundreds to low thousands depending on workload. 50,000
concurrent is 150–250 nodes and a six-figure monthly infrastructure bill. The
architecture must not preclude it; nothing should be over-built for it now.

**D-09 · Answering-machine detection targets ≤3.5 seconds, not 1.2.**
Detection needs 2–4 seconds of audio to be reliable. Forcing a faster decision
drives false positives, which means hanging up on real people — a
customer-experience failure and a regulatory exposure. The metric that matters
is the false-machine rate, not speed.

**D-10 · Specialist dispatch contacts candidates simultaneously.**
One ring cycle is 15–20 seconds, so sequential contact with tier roll-over
cannot meet a 15-second target under any circumstances. Simultaneous contact
with first-accept-wins is the mechanism; roll-over is the escalation path only.

**D-11 · Capacity rejection uses a resource-exhaustion response, not a
busy-destination response.**
A busy signal tells the carrier the *destination* is busy, which corrupts the
partner's answer-seizure statistics and hides the real cause when they
investigate. Resource exhaustion also lets the carrier reroute.

**D-12 · Licence enforcement degrades, never disables.**
A phone system that stops working over a billing state is a liability no
revenue justifies. Emergency calls connect in every licence state — valid,
expired, degraded, over-capacity or tampered — and that path cannot be gated.

**D-13 · Hardware fingerprinting must be weighted and tolerant.**
A rebuilt cloud instance changes its network address; a migrated virtual
machine changes its host identifier. A strict fingerprint locks out a working
phone system for a reason the partner cannot diagnose. False lockouts are the
largest single support-cost driver in licensed software. Three of five
attributes matching, self-service reactivation, warn-and-recheck rather than
hard fail.

**D-14 · A short offline grace makes our entitlement service critical
infrastructure.**
If it is unreachable for longer than the grace period — our outage, a
certificate expiry, a DNS fault — every partner degrades simultaneously. That
is a self-inflicted mass outage and a worse business event than some licence
leakage. Requires high availability, a status page, warnings from day two, an
emergency extension procedure, and a manually issued long-term offline licence
for air-gapped deployments.

---

## Architecture

**D-15 · We do not build telephony from first principles.**
We orchestrate a mature open-source media engine through its programmable
interface. Removes protocol engineering risk, years of time to market, and
gives instant carrier compatibility. Our IP is the control plane, the AI media
pipeline, the dispatch engine, the API layer and the licensing system.

**D-16 · All call logic lives in application code; telephony configuration
holds entry points only.**
Logic in configuration files is untestable, unversionable and unreviewable.

**D-17 · A Participant is not a Channel.** *(historically named "Leg";
renamed by D-32.)*
A call is a meeting; a participant is one party's participation in it. One
participant may traverse several engine channels — a transfer replaces
channels while the participant's involvement and billable duration continue
unbroken. Billing by channel would split one 8-minute participation into two
shorter calls. The channel-swapping mess lives entirely in the
anti-corruption layer.

**D-18 · A Call is an aggregate of N participants, never "caller and
agent".** *(historically "N legs"; renamed by D-32.)*
Two participants today, three when a supervisor joins, three or more for
interpreting. Modelling it as two named parties means new columns for every
future case, and "how many minutes did we buy from this interpreter" has
nowhere to look.

**D-19 · Supply our own identifier when originating.**
The engine accepts a caller-supplied channel identifier. Using it makes
correlation exact instead of a guess under concurrency — matching on dialled
number and timestamp is a guess, and at 30 calls per second it is a wrong one.

**D-20 · Modular monolith, ports and adapters, not microservices.**
Roughly a third less infrastructure and integration work, with no scaling
penalty below the target concurrency. Modules extract later if justified.

**D-21 · Compliance is pure functions, not an aggregate.**
No state, no I/O, everything passed in, a verdict returned. Exhaustively
testable in milliseconds, independently auditable, impossible to bypass — which
is what lets us answer a regulator with evidence.

**D-22 · IVR and call flows are data the domain interprets.**
Not dialplan, not procedural code. A node graph; execution is a pure fold from
current node plus event to next node plus commands. This gives the visual
builder, the API, versioning and rollback later as near-free additions, and
makes the most logic-heavy part of the system testable with no telephony.

**D-23 · AI subscribes to copied audio and is never in the call path.**
An AI failure can degrade a feature. It can never degrade a call.

**D-24 · Twelve extensibility seams, four of which cost months to retrofit.**
`tenant_id` everywhere from commit one · Call as N participants · API-first
from the first commit · per-participant per-second usage records emitted
even though nothing consumes them yet. Historical usage data you never wrote
cannot be recreated.

---

## Process

**D-25 · The walking-skeleton spike comes before any specification.**
Specifications written before a real call has been through the system are
guesses, and an agent implements guesses faithfully. Ten days, local rig first
(no carrier needed), carrier when it arrives, then delete the code and keep
the findings.

Development SIP setup (local rig):
- Asterisk configured with local SIP peers (extensions 1001-1999)
- No SIP trunks configured
- WebRTC works locally via WSS
- Test SIP endpoint (pure Go, `sipgo`) simulates external caller

Carrier integration happens AFTER the core call path works. It uses the same
architecture — just add trunk configuration. No code changes required.

**D-26 · Dependency-first ordering within a release, risk-first across it.**
Tenancy is the correct first dependency and the wrong first build. Prove the
media path before building on assumptions about it.

**D-27 · Power dialling before predictive.**
Power dialling proves the whole loop. Predictive is a pacing algorithm on top of
a loop that already works. Doing both at once means debugging pacing mathematics
and telephony plumbing simultaneously, with no way to tell which is broken.

**D-28 · Machine gates over human review.**
The builder is also the reviewer and the author of the prompt that generated the
artefact — self-review catches little. Push everything checkable into CI and
reserve human judgement for "is this the right shape" and "does this match the
business intent". No change merges the day it was generated.

**D-29 · OpenSpec for per-change artefacts; TRD documents stay separate.**
Proposal, delta specs, design and tasks live in change folders and archive into
living specs. The domain model, HLD, data model, API contract and test strategy
are cross-cutting and stay in `docs/`. BRD and PRD are never converted into
OpenSpec specs — different audiences, different lifecycles.

**D-30 · No bulk-installed agent skills from marketplaces.**
Skills are instructions steering an agent that writes and commits code. Bulk
installation of unreviewed instruction files is a supply-chain risk that no
dependency scanner catches, because the payload is prose. Write our own few.

**D-31 · Scaled-down DDD.**
Keep: ubiquitous language, bounded contexts, aggregates with invariants, domain
events, ports and adapters, one anti-corruption layer for the media engine.
Drop: event sourcing, CQRS as an architecture, a repository per entity, value
objects for everything, factories, domain services by default.

**D-32 · Participant replaces Leg as the domain term (2026-09-05).**
Same concept D-17/D-18 defined — one party's participation in a Call,
distinct from an Asterisk Channel — renamed for clarity before any code or
OpenSpec spec shipped using the old name. `CallParticipant` is the explicit
entity name where precision matters (e.g. the Call aggregate's child
collection); `Participant` is the short form used elsewhere. No behavioural
change — terminology only.

**D-33 · ConnectRPC as the sole API ingress standard (2026-09-05).**
Protobuf schema-first contracts generate Go handlers, web TypeScript clients,
and CLI tools with compile-time backward-compatibility checks (`buf breaking`).
ConnectRPC serves standard JSON over HTTP/1.1 for browsers, webhooks, and cURL,
alongside high-throughput binary gRPC over HTTP/2, eliminating the operational
and memory overhead of an external Envoy reverse proxy in partner on-premise
deployments. First-class HTTP/2 server streaming supports real-time RTCP-XR
telemetry and presence feeds with unified authentication interceptors.
Secondary REST frameworks (Gin, Fiber, Echo) are prohibited to eliminate schema
drift and redundant router middleware.

**D-34 · Native pgx/v5 with parameterized SQL over heavy ORMs (2026-09-05).**
Multi-tenancy mandates PostgreSQL 16 Row-Level Security (`SET LOCAL app.tenant_id`)
scoped strictly per transaction. ORMs (GORM, Ent) obscure connection pooling and
transaction lifecycle, creating unacceptable cross-tenant data leak risks.
High-frequency call metering requires native binary stream ingestion
(`pgx.CopyFrom`) to sustain 50,000+ inserts/sec for 1-second usage ticks and RTCP
metrics without garbage collection spikes. Parameterized SQL provides native
support for `SELECT ... FOR UPDATE SKIP LOCKED` (transactional outbox and dialer
queues) and eliminates unpredictable N+1 query plans during sub-2ms call routing.

**D-35 · Permissive FOSS and continuous supply-chain scanning (2026-09-05).**
Only permissive open-source licenses (Apache 2.0, MIT, BSD 2/3-Clause, ISC) are
approved. Copyleft (GPL, AGPL) and proprietary licenses are strictly prohibited to
protect partner appliance distributions from legal liability. All dependencies
must be pinned in `go.mod` and `.mise.toml`. Pull requests and CI builds are
strictly gated on `govulncheck` and Trivy container vulnerability scanning.

**D-36 · React + TypeScript for Portal (2026-09-06).**

**Decision:** The AtsaPBX portal (Admin UI, Agent UI, Supervisor Dashboard,
Partner Portal) will be built with React + TypeScript.

**Context:** Need to decide frontend technology for the portal. The portal is a
consumer of the ConnectRPC API, just like partners.

**Alternatives:**
- HTMX: Simpler, server-rendered. Rejected because agent workspaces, supervisor
  dashboards, and wallboards require rich, stateful UI.
- Vue.js: Lighter than React. Rejected because ConnectRPC TypeScript support
  less mature.
- Svelte: Emerging. Rejected because smaller ecosystem and ConnectRPC support
  less mature.
- Vanilla JS: No build step. Rejected because no type safety and harder to
  maintain at scale.
- Angular: Full framework. Rejected because too heavy for solo engineer.

**Rationale:**
- ConnectRPC generates TypeScript clients from Protobuf (type safety end-to-end)
- Rich, stateful UI required (agent workspaces, live wallboards, real-time
  presence)
- Largest ecosystem, best AI agent support for React + TypeScript
- Solo engineer friendly - most documented stack
- Same stack partners will use for their own portals

**Consequences:**
- Portal consumes same ConnectRPC API as partners (no privileged path - D-24)
- Portal serves as reference implementation for partners
- Partners can build their own portals using the same patterns
- Portal deployment: static SPA served from CDN or nginx
- Frontend package management: npm/yarn

**Related Decisions:** D-33 (ConnectRPC sole API ingress), D-24 (API-first)
**Traceability:** BRD §2.4, PRD EPIC-10

---

## Known and accepted limitations

- A call already in progress on a failed carrier route cannot be moved. External
  limitation, stated openly in service commitments, never presented as something
  the platform can do.
- Active calls on a lost media node end, unless call preservation is separately
  funded.
- The platform terminates media encryption when translating between browser and
  telephone network — that is what makes recording, compliance and quality
  measurement possible. **We are not end-to-end encrypted from browser to phone
  number and must never be sold as such.**
- Managed cloud competitors genuinely win on ease of operation. That is the
  trade a partner makes in exchange for owning the platform and the margin.
