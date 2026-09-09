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

**D-37 · Third-party agent skills allowed upon explicit owner approval (2026-09-06).**

Amends D-30. D-30's default stands: bulk installation of unreviewed
marketplace skills stays prohibited — prose that steers an agent with commit
access is a supply-chain risk no dependency scanner catches. What changes: a
third-party skill MAY be installed once the owner has explicitly approved that
specific skill, in a separate message, after its contents (SKILL.md plus any
scripts or commands it ships) have been reviewed. Approval is per skill, never
bundled — `--all`-style bulk installs remain prohibited even under this
amendment. Each approval and the skill's provenance (source repository and
commit) are recorded alongside the installed skill. This amendment satisfies
the "explicit human architectural authorization" clause of TOOLSET.md §5, so
no TOOLSET.md change is required.

**D-38 · Structured `slog` logging with mandatory redaction is the platform logging standard (2026-09-06).**

**Decision:** All application logs are emitted through Go's standard-library
`log/slog` as JSON, through a single shared handler in `internal/logging`. The
handler applies the mandatory field envelope (D-39) and a redaction layer that
masks telephone numbers and replaces any field whose name is `password`,
`secret`, `api_key`, `token`, or `credential` with `[REDACTED]`. Call audio and
transcript content are barred from log lines.

**Context:** The platform's observability design (HLD `07-observability.md`) and
its credential-isolation obligations (PRD INV-11, BRD BR-15) both require that
logs be machine-parseable, correlatable, and free of secrets and PII. Without a
single enforced standard, each bounded context invents its own format and its
own idea of what is safe to log — and a credential leaked once into logs,
diagnostics or error paths is unrecoverable exposure (BR-15).

**Alternatives:**
- Third-party logging packages (`logrus`, `zap`, `zerolog`): rejected — redundant
  given `log/slog`, and forbidden by TOOLSET.md §1.4 (standard library first).
- Unstructured text logs: rejected — PII/secret scrubbing is impractical, and
  collection/alerting tools lose the structured fields D-39 requires.
- Redaction left to each call site: rejected — a caller forgetting to redact is
  exactly how a credential reaches a log. Central redaction at the handler
  boundary is the only place every caller is covered by default.

**Consequences:**
- All services and the eventual portal backend log through `internal/logging`.
- A test gate asserts the D-39 field set and the redaction behaviour; a log line
  containing a known secret pattern fails CI.
- Scrubbers in diagnostic bundles and exports reuse the same redaction rules.

**Related Decisions:** D-39 (mandatory fields), D-35 (approved toolset)
**Traceability:** PRD INV-11; BRD BR-15; HLD `07-observability.md` §3.1

**D-39 · Mandatory structured log fields (2026-09-06).**

**Decision:** Every log record MUST carry the fields below; what MUST NOT be
logged is equally normative.

| Field | Description | Required |
|---|---|---|
| `timestamp` | ISO 8601 with timezone | ✅ |
| `level` | Log level (debug, info, warn, error, fatal) | ✅ |
| `call_id` | Correlation ID for call (if applicable) | ✅ |
| `tenant_id` | Tenant identifier | ✅ |
| `trace_id` | OpenTelemetry trace ID | ✅ |
| `msg` | Human-readable message | ✅ |
| `module` | Source module | ✅ |

**What MUST NOT be logged:** secrets, API keys, passwords, tokens, call
audio/transcripts (unless explicitly enabled for a supported feature), and PII
(unless required for debugging and logged with explicit consent).

**Context:** Named by HLD `07-observability.md` §3.2 and by the in-house agent
skills as "the D-39 fields" before this entry existed — this entry makes the
decision the citations point at.

**Alternatives:** A looser "log what is useful" policy: rejected — a support
engineer resolving a dispute in under 10 minutes (BRD §11) needs every record
correlatable by `call_id`/`tenant_id`/`trace_id`; an auditor needs to *prove*
secrets were never logged (INV-11).

**Consequences:** Implemented by the shared handler in `internal/logging`
(D-38); the field set is part of the CI log-inspection gate.

**Related Decisions:** D-38 (logging standard)
**Traceability:** HLD `07-observability.md` §3.2; PRD INV-11; BRD BR-15

**D-40 · Go Coding Standards Adoption (2026-09-06).**

**Decision:** All Go code — human-written or agent-generated — follows the
house Go coding standards in `docs/hld/13-coding-standards.md`, which are
normative and reference the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
and the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
instead of copying them.

**Context:** AI agents write most of this project's Go code. Without
enforceable standards, agent output drifts in quality and reintroduces
classic Go failure modes (leaked goroutines, copied mutexes, swallowed
errors, untestable nesting). The audience includes Go beginners, so the
rules must be few, simple, and machine-checkable.

**Key Principles:**
1. Spec-Driven Development: specifications are law — implement only what
   OpenSpec delta specs, Protobuf contracts, and migration SQL describe.
2. Error handling: always handle, wrap (`%w`), and return; blank-identifier
   ignores follow the scoped rule in HLD 13 §2 (never on
   behavior-affecting I/O).
3. Context propagation: `context.Context` is the first argument of all I/O
   operations.
4. Concurrency safety: mutexes by pointer, every goroutine has a documented
   exit strategy, channel ownership is explicit.
5. Line of sight: happy path left-aligned, errors return early.

**Alternatives:**
- No standards: rejected — inconsistent agent-generated code quality.
- Full Uber guide adopted verbatim: rejected — too verbose for agents and
  drifts with upstream; we link it instead of copying it.
- Custom from-scratch standards: rejected — not industry proven.

**Consequences:**
- All new code must pass `gofmt`, `go vet`, and `golangci-lint` (including
  the `depguard` ACL rule and `goimports` grouping); CI fails otherwise.
- Agents are instructed via the role prompt in HLD 13, Appendix A.
- Existing code is refactored incrementally, not all at once.

**Related Decisions:** D-24 (API-first), D-28 (machine gates), D-33
(ConnectRPC), D-35 (toolset)
**Traceability:** TRD Tech stack; HLD `13-coding-standards.md`; TOOLSET.md

**D-41 · Media engine stance: Asterisk retained; portability via contract (2026-09-07).**

**Decision:** Keep Asterisk 22.x LTS as the sole media engine. Future
engines are made pluggable through the `MediaGateway` capability
contract plus the `mediatest` conformance suite — not through
multi-engine support, and never by reshaping the ports to fit a
candidate.

**Context:** Four candidates evaluated against the port surface.
FreeSWITCH fits the shape (ESL control, UUIDs + channel variables,
bridge mixers) but has no business driver — paying its adapter cost now
buys nothing. LiveKit is a different media architecture (rooms/tracks,
no PSTN originate or channel correlation). Diago-as-engine inverts D-15
(protocol risk moves into our process). VoiceBlender fails D-15 on
maturity (months old) and D-05/D-23 on provider-coupled, in-path AI.

**Alternatives:**
- Adopt FreeSWITCH now: rejected — real adapter + container + config +
  CDR work with zero product benefit today; the option stays open via
  the contract.
- Adopt LiveKit, Diago, or VoiceBlender as engine: rejected — role
  mismatch (LiveKit), locked-decision conflict (Diago vs D-15),
  maturity + AI-posture failure (VoiceBlender).
- Do nothing beyond the interfaces: rejected — portability would stay
  folk knowledge instead of an executable gate.

**Consequences:**
- A future engine proves fit by passing `mediatest`; unsuitable
  candidates die at design review, never mid-build.
- Exactly one engine stays wired (Asterisk) until a D-logged revisit
  with a business driver.
- Watch, don't adopt: VoiceBlender revisited in 18–24 months if it
  hardens; LiveKit only if video/meetings ever enters scope (out
  through R3).

**Related Decisions:** D-15 (orchestrate, don't build), D-20 (modular
monolith), D-24 (API-first)
**Traceability:** BRD FBR-R1-01/FBR-R1-03; PRD EPIC-02/EPIC-03; HLD
`01-architecture.md` §2, `04-bounded-contexts.md` §1

---

**D-42 · `identity` stays in-process; the port seam preserves the split
option (2026-09-07).**

**Decision:** `identity` is a bounded context inside the modular
monolith, not a separate service — now or at R1. The ports-and-adapters
seam keeps extraction possible later; nothing is built now to enable it.
If a context is ever extracted first, it will not be this one.

**Context:** Asked directly whether `identity` is "the user service" and
whether it could become a microservice. It is narrower than a user
service (tenants, principals, credentials, RBAC, audit — not extensions,
agents or queues; a principal signs into the portal, an extension
registers a phone, and conflating them is a modelling error the schema
already avoids) and the split question is worth answering once rather
than repeatedly.

The code is genuinely standalone today: `identity` imports zero other
bounded contexts, everything crosses a port interface, and `tenant_id`
plus tenant-partitioned NATS subjects mean distributed tenancy needs no
retrofit. The coupling that would actually bite is not in the code:

- **The shared database.** `identity` and `telephony-core` share
  `atsapbx` and both rely on `SET LOCAL app.tenant_id`. Splitting means
  splitting the schema, and the moment `calls.tenant_id` cannot be a
  foreign key to `tenants.id`, a database-enforced invariant becomes an
  eventually-consistent one. Port discipline does not avoid this.
- **`ValidateToken` reads the database per request** — deliberately, so
  disabling an account takes effect immediately rather than at token
  expiry. Across a network boundary that becomes an RPC on every call,
  and the caches that fix it reintroduce the window the design closed.

**Alternatives:**
- Extract `identity` as a service now: rejected — pays distributed-system
  cost (schema split, per-request RPC, an auth outage that takes down
  everything rather than one feature) against no current scale need
  (D-08: 50,000 concurrent is a year-three point).
- Merge identity into telephony-core to avoid the seam entirely:
  rejected — the seam is what makes the option cheap to keep, and
  Tier-1 contexts all need tenant context (HLD 04 §10.1).
- Design for eventual extraction now (dual-write, service registry,
  etc.): rejected — over-building for year-three scale, D-08.

**Consequences:**
- Identity is the *last* context to extract, not the first: everything
  depends on it, so making it a network hop converts a local failure into
  a platform-wide one. The likelier first candidates are `ai-pipeline`
  (different scaling profile, external providers, fails safe by INV-04)
  or `dialer` (bursty, CPU-bound pacing).
- Known ceilings, recorded rather than discovered later: Argon2id cost is
  bounded by login *rate*, not user count (that is the defence working);
  `ValidateToken` throughput is bounded by Postgres reads; and licensing's
  capacity counter is per-process today (LLD-08 §7 — split out of LLD-02 on 2026-09-08) — the first genuine
  multi-node blocker, and a licensing concern rather than an identity one.
- Revisit when a real driver appears: independent scaling need, a
  separate compliance boundary, or a second product sharing identity.

**Related Decisions:** D-08 (year-three scale, do not over-build), D-20
(modular monolith), D-24 (tenant_id everywhere, API-first), D-26
(dependency-first ordering), D-31 (scaled-down DDD)
**Traceability:** PRD EPIC-01, INV-10; HLD `01-architecture.md` §1.2,
`04-bounded-contexts.md` §§7/10.1; LLD-02 §§8/10

---

**D-43 · A capability gets an RPC when it has a named R1.0 consumer
(2026-09-07).**

**Decision:** A capability is exposed over the wire in the same change
that builds it if a named R1.0 consumer needs it — the partner portal
(EPIC-10), a partner developer (US-05.1), or a test harness. Otherwise
it stays a Go port and the change records why. "Reachable in principle"
is not API-first.

**Context:** After two implemented LLDs, 17 capabilities existed and
exactly one — `GetCall` — was reachable over the wire. LLD-02 §6 had
argued API-first was "satisfied either way" because every capability was
reachable; that reasoning treated in-process Go calls as an API, which
they are not for a portal, a partner, or a harness.

Reviewing it surfaced a concrete defect rather than a stylistic one:
LLD-02's sequence ended with `auth-cutover-connectrpc` requiring a JWT on
every RPC, while the only way to mint one (`AuthenticateUser`) was a Go
method, and the dev-token path is `//go:build dev` and refuses to run
outside `ATSAPBX_ENV=development`. The cutover would have locked the API
with the key inside the building. A rule stated once prevents that class
of error; per-change judgement did not.

**Alternatives:**
- Expose every capability as it is built: rejected — freezes contracts
  before their consumers exist. `InitiateCall` is the example: `pbx-core`
  (LLD-03) will reshape what origination means once routing and
  extensions are real, and D-25 warns against specifying unproven
  territory.
- Defer all wire surface to a portal slice: rejected — this is what
  produced the lockout above, and it back-loads US-05.1 ("every action
  available in the console through a documented API") into the release
  where it is least affordable.
- Expose read paths only: rejected — provisioning and suspension are
  console actions in EPIC-01; a read-only API cannot run a portal.

**Amended 2026-09-07 — the console is a named consumer.** BRD §16's R1.0
gate requires "every function demonstrated through the public API with
published documentation, and the administration console proven to use
only those same endpoints", and all configuration — administration, call
flows, IVR, contact-centre setup, SIP trunks — is performed through the
portal, which translates it into Asterisk state. The console therefore
*is* a named R1.0 consumer for essentially every capability in R1.0
scope. The consumer test still holds and still refuses speculative
surface, but the default for an R1.0 capability is now **on the wire**,
and a port-only decision is the exception that must argue for itself.
Cross-cutting shape (naming, errors, pagination, mutation semantics,
tenancy) is fixed once in `docs/API.md` §3a so that delivering the API
one LLD at a time still yields one coherent API.

**Consequences:**
- Mechanism stays internal by construction: `ValidateToken`,
  `AuthorizeAction`, and `AuthenticateAPIKey` are what the interceptor
  does *for* a caller, never something a caller invokes. Exposing
  `ValidateToken` would hand out a token-validity oracle.
- The console gets no privileged back door. If it needs an operation, the
  operation is public API with the same authorization as any partner's
  own client would face — which is what makes the R1.0 gate provable
  rather than asserted.
- Each LLD's ConnectRPC section states, per capability, wire or port and
  the consumer that justifies it. A capability with no named consumer is
  a port, and saying so is the record.
- The wire surface grows per context rather than in one late portal
  change, so `buf breaking` has something real to protect from the point
  each contract acquires a consumer.
- Applied immediately: LLD-02 §9 gains an `identity-api` change before
  `auth-cutover-connectrpc`.

**Related Decisions:** D-24 (API-first from commit one), D-25
(walking-skeleton before specification), D-33 (ConnectRPC), D-42
(identity stays in-process)
**Traceability:** PRD EPIC-05 (US-05.1), EPIC-10; HLD
`12-portal-architecture.md`; LLD-02 §§6/9

---

**D-44 · The platform is tenant-abstracted; the one SaaS blocker is
licensing's scope, and it is decidable now (2026-09-07).**

**Decision:** Keep building for the self-hosted per-partner model, which
is what BRD/PRD describe. The tenancy abstractions already in place carry
over to a hosted multi-tenant SaaS unchanged, with one exception —
licensing is installation-scoped — and that exception is settled *before*
licensing is implemented rather than after, because it is the only part
that would cost months to unpick.

**Context:** Asked whether the design extends to multi-tenant SaaS. An
audit of what is built says most of it already does:

| Already tenant-abstracted | Where |
|---|---|
| `tenant_id` on every row, RLS + FORCE, fails closed | migrations, HLD 03 §5 |
| Tenant as an aggregate with reversible suspension | `identity/domain` |
| `residency_zone` per tenant, immutable once set | AC, LLD-02 §10.6 |
| System scope vs tenant scope — platform operator vs customer admin | `domain.IsAuthorizedSystem` |
| Usernames unique **per tenant**, not globally | AC-01.2, `UNIQUE(tenant_id, username)` |
| API keys carry and resolve to their own tenant | `identity-auth-rbac` |
| Audit per tenant; NATS subjects tenant-partitioned | HLD 03 §5, LLD-01 |

What is not, in rough order of cost to retrofit:

1. **Licensing is installation-scoped.** `licensing_state` is keyed by
   `instance_id` with no `tenant_id` (T-1, D-42) — correct for a licence
   installed once per partner deployment, wrong for SaaS where
   entitlement is per tenant. This is the expensive one, and licensing
   is **not yet built**, so the moment to keep the option open is now.
2. **Login needs a tenant UUID.** `AuthenticateUser` takes `tenant_id`,
   but a SaaS user signs in with an email or a subdomain. Needs a
   slug/domain → tenant lookup. Additive; a unique `slug` column on a
   populated table is a migration, not a redesign.
3. **No self-service signup.** Tenant creation requires system scope
   today. A public signup path is purely additive.
4. **Shared connection pool.** Per-tenant quotas and noisy-neighbour
   controls are infrastructure concerns for whoever operates the hosted
   plane, not domain ones.

**Alternatives:**
- Build for SaaS now: rejected — no hosted product is planned, and D-08
  rules out constructing for a scale point that does not exist. The
  seams above already make it reachable.
- Ignore the licensing scope until a SaaS need appears: rejected — that
  is exactly the retrofit that costs months, and it is avoidable for free
  while the code is unwritten.

**Consequences:**
- **Actionable now:** LLD-02's licensing work must not assume
  installation scope any deeper than the `licensing_state` row. The port
  seam already helps: `ValidateCapacity(ctx, tenantID, requestedChannels)`
  carries a tenant, kept during the LLD-02 review for an unrelated reason
  (avoiding a telephony-core edit) and now the thing that lets capacity
  become per-tenant without reshaping callers. Keep `tenantID` in that
  signature even while the implementation ignores it.
- A hosted plane would add rows to `licensing_state` keyed by tenant, a
  tenant slug for login, and a signup path — none of which reshape
  `identity`, `telephony-core`, or the RLS model.
- Revisit when a hosted offering is actually proposed; this decision
  records that it is not blocked, not that it is planned.

**Related Decisions:** D-08 (do not over-build for year-three scale),
D-24 (tenant_id everywhere from commit one), D-42 (identity stays
in-process), D-43 (API-first consumer test)
**Traceability:** PRD INV-10, EPIC-01; HLD `03-domain-model.md` §5,
`11-decisions.md` T-1; LLD-02 §§8/10.6

---

**D-45 · Outbound dialling joins R1.0; predictive stays in R2
(2026-09-07).**

**Decision:** R1.0 now includes outbound dialling in preview and power
modes (EPIC-14) together with outbound compliance enforcement (EPIC-15).
Predictive pacing remains R2. D-27 is unchanged and is the reason for the
split, not a casualty of it.

**Context:** The MVP was stated as VoIP core *and* contact centre with
dialling. The PRD had the dialler and its compliance surface deferred
wholesale to R2 as "a distinct product line", which no longer matches
what the product is for.

Compliance moves with it and is not optional: a dialler without enforced
do-not-call, calling hours and opt-out is not a smaller product, it is
one that places unlawful calls. Principle 6 — compliance is enforced,
never advised — makes EPIC-15 a release gate the moment any campaign can
dial.

**Alternatives:**
- Take predictive into R1.0 as well: rejected — this is exactly what
  D-27 forbids, and nothing about the MVP framing changes the reasoning.
  Power dialling proves the whole loop; predictive is a pacing algorithm
  on top of a loop that already works. Shipping both at once means
  debugging pacing mathematics and telephony plumbing simultaneously with
  no way to tell which is broken.
- Leave the dialler in R2 and sell R1.0 as inbound-only: rejected — it
  does not match the intended product, and BPO and contact-centre
  operators are a named target partner (BRD §3.1).
- Ship the dialler without EPIC-15: rejected outright. Not a scope
  option; a regulatory one.

**Consequences, stated plainly:**
- **R1.0 is now nearly the whole platform.** The dialler sits in Tier 2
  of the dependency graph (HLD 04 §10) and needs `telephony-core`
  (originate), `compliance` (clearance), `pbx-core` (queues, routing) and
  `reporting` (usage). Pulling it in pulls LLD-03, LLD-04 and LLD-06 into
  R1.0 with it, alongside the console (LLD-07) which must now also carry
  campaign management. What remains outside R1.0 is mobile, emergency
  location, fax, migration tooling, supervisor tooling, packaged CRM
  connectors, and the R3 specialist vertical.
- **The release date moves substantially.** This was accepted knowingly
  when the scope was set; it is recorded here so nobody later reads the
  R1.0 date as evidence the plan was optimistic rather than deliberately
  widened.
- Build order is unchanged — the graph already sequences the dialler
  last among Tier 2, and D-26's dependency-first rule still applies.
  Nothing may be started earlier to compensate for the wider scope.
- Predictive arriving in R2 lands on a power loop that has run in
  production, which is the whole point of D-27.

**Related Decisions:** D-26 (dependency-first ordering), D-27 (power
before predictive), D-43 (API-first consumer test)
**Traceability:** BRD FBR-R2-01/02/03, §3.1; PRD §4 release plan, §5.2,
EPIC-14, EPIC-15; HLD `04-bounded-contexts.md` §10

---

**D-46 · R1.0 is delivered in three phases, each demonstrable, each
ending with console work (2026-09-07).**

**Decision:** Keep R1.0's scope as D-45 set it, and deliver it in three
phases that each end in something a partner can be shown. Every phase
includes its own console slice; the console is **not** one large
frontend built at the end. Phases continue from what is already
implemented — nothing is restarted.

**Context:** D-45 widened R1.0 to nearly the whole platform, which is
correct for what the product is but leaves a long stretch with nothing
demonstrable. Separately, the console had been planned as a single
LLD-07 *after* every backend LLD, on the reasoning that it can only
consume APIs that exist. That reasoning is sound per API and wrong per
delivery: it means no user-visible product until the end, no feedback on
whether the API is usable by the interface that must use it, and a large
frontend integration risk concentrated at the worst moment.

**Phases:**

| Phase | Ends with a partner able to… | Work |
|---|---|---|
| **A** | install it, licence it, and make and receive real calls — configured entirely in the UI | finish LLD-02's licensing half (already designed); LLD-03 `pbx-core` minimal — extensions, a SIP trunk, basic routing; console slice A — login, users and roles, extensions, trunk, licence status |
| **B** | run it as a business phone system | LLD-03 completion — IVR, auto-attendant, business hours, queues; LLD-04 `reporting` — call detail, history, quality; recording governance; console slice B — visual IVR builder, queues, call history, recording policy |
| **C** | run an outbound operation on it | LLD-04 `compliance` **first** — DNC, calling hours, opt-out; LLD-06 `dialer` — preview and power; console slice C — campaigns, contact lists, compliance configuration |

Predictive pacing remains R2 (D-27, D-45), landing on a power loop that
has run in production.

**Alternatives:**
- Backend-complete, then one console: rejected — the reasoning above.
  Nothing demonstrable until the end, and the first real test of "can the
  interface actually be built on these endpoints" arrives too late to act
  on.
- Console-first against mocks: rejected — it would freeze contracts
  before the domain that serves them exists, and AC-05.1 requires the
  console to use the same endpoints a partner would, not mocks that later
  diverge.
- Shrink R1.0 instead of phasing it: rejected — D-45 set that scope
  deliberately and for product reasons; phasing addresses the delivery
  concern without reopening it.

**Consequences:**
- **API-first still holds, per phase.** A phase's endpoints land before
  its console slice, and the slice uses only public endpoints — the
  console gets no back door (D-43, AC-10.7). Each phase is a small
  version of the same discipline, not an exception to it.
- Each phase's console slice is the first honest test of that phase's
  API. If a screen cannot be built without a private endpoint, the API
  is wrong and is fixed in that phase rather than worked around.
- The `pbx-core` Asterisk-translation decision (docs/lld/README.md) is
  settled in Phase A, where the surface is smallest and being wrong is
  cheapest to correct.
- Phase A is independently sellable as a PBX. B and C add contact centre
  and outbound. Compliance precedes the first dial in C, always — it is
  a gate, not a phase-ordering preference (D-45).
- LLD-07 is retired as a single unit; console work is tracked as a slice
  per phase.

**Related Decisions:** D-25 (walking skeleton before specification), D-26
(dependency-first ordering), D-27 (power before predictive), D-43
(API-first consumer test), D-45 (outbound in R1.0)
**Traceability:** PRD §4 release plan, EPIC-03, EPIC-10, EPIC-14,
EPIC-15; HLD `04-bounded-contexts.md` §10; `docs/lld/README.md`

---

**D-47 · Configuration reaches Asterisk through PJSIP Realtime, into
ACL-owned projection tables (2026-09-07).**

**Decision:** Console configuration becomes live Asterisk state through
**PJSIP Realtime**. Asterisk reads its own `ps_*` tables directly from
Postgres; there is no file generation, no reload, and no "Apply Config"
step. Those `ps_*` tables are **projection tables owned by the ACL** —
they are not the domain model. `extensions` and `carrier_trunks` (HLD 03
§5) remain the tenant-scoped source of truth, and the ACL projects into
`ps_*` inside the same transaction that writes the domain row.

**Only static registration objects are projected** — endpoints, auths,
aors and trunk configuration. **No dialplan is ever generated.** Call
routing, IVR, auto-attendant and queue behaviour stay in our Go
application, interpreted live in Stasis over ARI, exactly as proven in
LLD-01.

**Context:** `docs/lld/README.md` carried three candidates and no
decision, and D-46 put settling it in Phase A. This was tested against
our own Asterisk 22.8.2 on 2026-09-07 rather than reasoned about, with
`res_config_pgsql` and `res_sorcery_realtime` (both already in our
image) mapped to a minimal `ps_endpoints`/`ps_auths`/`ps_aors` schema:

| Test | Result |
|---|---|
| `INSERT` an endpoint row, issue no reload | `pjsip show endpoint 9001` resolves it, transport/auth/aor/codecs all bound |
| Real SIP `REGISTER` from a UA, digest auth | `401` challenge, then **`200 OK`** — the contact appears under the aor |
| Same `REGISTER` with a wrong password | `401 Unauthorized` — credentials are genuinely enforced from the row |
| `DELETE` the rows, issue no reload | `Unable to find object 9001` — deprovisioning is immediate |
| Existing `pjsip.conf` fixtures 1000/1001 alongside realtime | both keep working and stay registered |

A phone registered to an extension that existed only as a database row.
The spike was torn down afterwards; the dev environment is unchanged.

**Alternatives:**
- **Generated config + reload** (FreePBX's approach: its MySQL tables are
  the truth, `fwconsole reload` regenerates `pjsip.*.conf` and
  `extensions_additional.conf`, then reloads). Rejected. It buys nothing
  we need and costs three things: an activation window the API must then
  model (FreePBX's red "Apply Config" bar is that window made visible);
  file generation as a failure surface on a path that must not fail; and
  a reload whose blast radius is the whole node — in a multi-tenant
  platform (D-08), tenant A editing an extension must not reload tenant
  B's engine. FreePBX needs the reload because it *compiles IVRs into
  dialplan*. We do not: our call flow is interpreted in Stasis, so the
  only thing left to project is a handful of static objects that change
  rarely and have a stable shape. Removing the reload removes the
  activation window entirely.
- **ARI dynamic config.** Rejected on evidence:
  `PUT /ari/asterisk/config/dynamic/res_pjsip/endpoint/...` returns
  **`403 "Cannot create sorcery objects of type 'endpoint'"`** (tested
  2026-09-07), because `res_pjsip`'s default sorcery backend is the
  config file. Making it work means configuring a writable backend —
  which is this decision underneath anyway — and objects created that way
  still need our database to survive a restart. ARI's `/endpoints`
  resource is read-only besides (`GET`, messaging, refer; no create).

**Consequences:**
- **Nothing about this is visible above the ACL.** The console calls our
  API, our API writes our database. Which mechanism the ACL uses is
  invisible to the console, the API, and any partner — PRD principle 4
  and the §1.2 package rule both hold unchanged. `docs/API.md` §3a's
  contract stands: the API models an `Extension`, never a `ps_endpoint`.
- **The mapping question API.md raised is answered:** the ACL owns it.
  Domain tables keep their own columns; `ps_*` is a read model for
  Asterisk, written by the ACL, never read by the domain.
- **Endpoint IDs must be globally unique, not the extension number.**
  `ps_*` is a single flat namespace shared by every tenant, so extension
  1000 in two tenants cannot both be `ps_endpoints.id = '1000'`. The
  projected id is derived from the extension's UUID; the tenant-local
  number the user sees stays tenant-local. LLD-03 fixes the exact form.
- **`ps_*` cannot use tenant RLS.** Asterisk connects as its own database
  role and cannot set a tenant context, so isolation on these tables is
  by construction (globally unique ids) rather than by policy. That role
  gets `SELECT` on `ps_*` only, and no access whatsoever to the domain
  tables — least privilege is the control here, and it is a Phase A task,
  not an afterthought.
- **Driver risk, stated and cheap to reverse.** `res_config_pgsql` is
  Asterisk *extended* support, not core; `res_config_odbc` is core. If
  the native driver disappoints, switching to ODBC needs one Debian
  package in `core/Dockerfile` and a DSN — no schema change, no code
  change, no decision reopened. Phase A uses the native driver.
- ~~**Deferred to the multi-node work (D-08):** registration contacts
  currently land in `astdb`, which is node-local.~~ **Superseded
  2026-09-08 while implementing `pbx-extensions-projection`.** The
  deferral was wrong, for a reason this entry did not anticipate: with
  contacts in `astdb` there is nothing for the platform to read, so the
  registered/not-registered state the console must show is unobtainable.
  Worse, a *partially* specified `ps_contacts` corrupts registration
  silently — Asterisk's INSERT names sixteen columns, a missing one fails
  the write, and the device still receives `200 OK` while no contact
  binds. `ps_contacts` is therefore mapped in Phase A with its full
  column set, which also delivers the multi-node visibility this bullet
  wanted, earlier.

  Two consequences follow, both real. **Registrations become database
  state**: a restore or failover leaves every device unreachable until it
  re-registers, where previously node-local `astdb` was unaffected by
  anything happening to Postgres. And it is the one projection table
  Asterisk *writes*, so the engine role holds
  `SELECT, INSERT, UPDATE, DELETE` there and read-only on the other
  three — the grant is no longer uniform. See
  `openspec/changes/archive/*-pbx-extensions-projection/design.md` D8.

- **Deprovisioning deliberately leaves the contact row** (decided
  2026-09-08, closing finding G3 from `pbx-e2e-projection`). Removing an
  extension deletes its endpoint, auth and aor; the `ps_contacts` row a
  device registered survives until Asterisk prunes it at expiry.

  It is inert: a contact is only ever reached through its aor, which is
  deleted with the endpoint, so an orphaned row cannot route a call or
  authenticate anything. Volume is bounded by registration expiry and
  self-heals.

  *Alternative — delete contacts in `RemoveExtension`.* Rejected. It
  looks tidier and is worse: contacts are the engine's own lifecycle,
  written on registration and pruned on expiry, and Asterisk holds its
  own view of them. Deleting rows underneath a running engine makes the
  platform race the engine's cache to remove something already inert.
  Completeness of a `DELETE` is not worth introducing a race.

  **The known cost:** `atsap-api pbx reconcile` inventories
  `ps_endpoints` only, so orphaned contacts are invisible to the one tool
  whose job is finding engine state that disagrees with the platform.
  That is accepted rather than fixed — reporting every recently deleted
  extension until its expiry would fill a signal tool with predictable
  noise. If contacts ever stop being inert, this is the decision to
  revisit.

  The e2e deletion test asserts the contact **remains**, so a change to
  this behaviour fails a test and reopens this decision rather than
  silently invalidating it.
- HLD `03-domain-model.md` §5 gains the projection tables, and
  `core/conf/` gains `sorcery.conf`, `extconfig.conf` and `res_pgsql.conf`,
  when LLD-03 is written. **`sorcery.conf` must restate the config-file
  wizard alongside the realtime one** — adding a realtime wizard alone
  replaces the default and the file fixtures vanish (observed in the
  spike).

**Related Decisions:** D-08 (scale to a regional cluster), D-24
(multi-tenant seams from the start), D-41 (one engine, behind the ACL),
D-43 (API-first consumer test), D-46 (settle this in Phase A)
**Traceability:** PRD principle 4, EPIC-03, EPIC-10; HLD
`01-architecture.md` §1.2, `03-domain-model.md` §5; `docs/API.md` §3a;
`docs/lld/README.md`

---

**D-48 · One LLD per bounded context; LLD numbers are stable identifiers
(2026-09-08).**

**Decision:** Every bounded context in HLD 04 §10.1 gets exactly one LLD,
and every LLD covers exactly one bounded context. The number is an
identifier, never a build order and never a grouping — once issued it is
never reused, never renumbered, and never retired onto a different
context. Build order is read from the §10.1 dependency graph and the D-46
phase table, nowhere else.

**This is the authoritative context → LLD map.** Where an older entry in
this log or an archived change cites a different number, that citation
was accurate when written and this table supersedes it:

| Bounded context (HLD 04 §10.1) | LLD | Tier |
|---|---|---|
| `telephony-core` | 01 | 0 |
| `identity` | 02 | 0 |
| `pbx-core` | 03 | 1 |
| `compliance` | 04 | 0 |
| `webhook-delivery` | 05 | 1 |
| `dialer` | 06 | 2 |
| `licensing` | 08 | 0 |
| `reporting` | 09 | 1 |
| `ai-pipeline` | 10 | 1 |
| `entitlement` | 11 | 1 |

**07 is permanently vacant.** It was the console, retired as a single
unit by D-46; the console is not a bounded context, so under this
decision it cannot hold an LLD number at all. It is tracked as a slice
per phase.

**Context:** `docs/lld/README.md` has stated "one LLD covers one bounded
context" since it was written, while its own index broke that rule in
three rows: LLD-02 held `identity` + `licensing`, LLD-04 held
`compliance` + `reporting`, LLD-05 held `webhook-delivery` +
`ai-pipeline`, and LLD-07 held a frontend that is not a context.

LLD-02 was split on 2026-09-08 and the split is what exposed the rest.
Its stated justification — the two contexts "share one cutover" — had
already been disproved by delivery: `identity` shipped three archived
changes while `licensing` shipped nothing, hidden behind a single "Draft"
status on one document. That is the failure mode the rule exists to
prevent, and LLD-04 and LLD-05 were positioned to repeat it.

**Why append rather than renumber.** Splitting `reporting` out of LLD-04
could have renumbered everything below it into a tidy sequence. Rejected:
numbers appear in archived, immutable OpenSpec changes and in this log,
and a renumber silently changes what an old citation means. `reporting`
therefore takes 09 and `ai-pipeline` takes 10, leaving 04, 05 and 06
pointing exactly where they always did. The index reads out of order, and
says so.

**Alternatives:**
- *Keep the pairs and rely on section headings inside one document.*
  Rejected — that is what LLD-02 did. Two contexts in one document hide
  divergent progress and produce a status line that is true of neither.
- *Renumber into a clean sequence.* Rejected — see above. Stable
  identifiers are worth more than a tidy column, the same reasoning that
  keeps decision numbers in this log fixed.
- *Give the console LLD-07 as a documented exception.* Rejected. The rule
  is one LLD per bounded context, and the console consumes every context
  and owns none; D-46 already retired it. An exception written into the
  rule's own index is how the rule stops being believed.

**Consequences:**
- LLD-09 and LLD-10 are index entries with no document yet, exactly as
  LLD-04, 05 and 06 are. Nothing is written before its turn in the graph.
- Citations that meant the split-off half were repointed in the same
  commit: `reporting` references from LLD-04 to LLD-09, `ai-pipeline`
  references from LLD-05 to LLD-10, in `docs/` and in the five Go
  comments that named them. Earlier entries in this log were left as
  written — this table is where a stale number is resolved.
- `docs/lld/LLD-02-identity-licensing.md` was deleted rather than kept as
  a redirect. Two archived changes link that path and now link nothing;
  the map above is where that path resolves. An unused file in the spec
  repo is worse than a dead link in an immutable record.
- A tenth context added later takes 11, whatever its tier. *(It did: `entitlement`, D-50, on 2026-09-09.)*

**Related Decisions:** D-29 (LLDs live in `docs/`, not OpenSpec), D-45
(outbound in R1.0), D-46 (three phases; console retired as one LLD)
**Traceability:** HLD `04-bounded-contexts.md` §10.1/§10.2;
`docs/lld/README.md`; `docs/API.md` §3

---

**D-49 · Hybrid licensing: a permanent free floor from 3CX, module
gating from VitalPBX, and neither one's unbundling (2026-09-09).**

**Decision:** The commercial model takes the useful half of each market
benchmark and refuses the rest.

- **From 3CX — a capacity floor.** An installation with no licence runs
  permanently as the **Unregistered tier**: 4 simultaneous calls, 10
  extensions, 1 tenant, ~~no key, no activation, no seeding~~
  (**superseded by D-52, 2026-09-09**: every entitlement including the
  free one is an activated signed token, and a never-activated
  installation is Setup with no call path). It is also the
  floor that expiry, elapsed grace and tampering degrade to, so the
  product has one degraded behaviour rather than one per cause.
- **From VitalPBX — module gating by edition.** Advanced modules check
  entitlement against the edition in the signed payload. Gating is by
  edition; there is no separate module-key mechanism.
- **From neither — unbundling the engine.** Multi-tenancy and SIP
  intrusion protection are native and never sold separately.

Recorded in BRD §10.4 (BR-LIC-01 to BR-LIC-03), §12.2's entitlement
matrix and §12.4's module catalogue; PRD §11.2 gains an `Unregistered`
state and EPIC-06 gains AC-06.11 to AC-06.14.

**Context:** the BRD had no free tier at all — editions started at 16
channels and nothing described an unlicensed install. That was not a
deliberate omission so much as an unasked question, and it surfaced while
proposing `licensing-capacity-grace`: deleting the always-permit stub
makes "what happens with no licence" a behaviour that must exist, and
nothing in BRD, PRD or the decision log answered it.

**Why these two are not arbitrary security exceptions:**

- **Multi-tenancy** is enforced by PostgreSQL row-level security, in the
  database, on every table. A tenant-isolation guarantee that is absent
  unless purchased is not a guarantee. It is also the product's identity
  (BRD §1) and the basis of the Operator pooled-channel licence, which is
  where tenancy *is* monetised.
- **SIP intrusion protection** is the same argument: a platform that can
  be brute-forced unless the customer buys the protection module is not
  secure, it is negotiable.

**Alternatives:**
- *No free tier (the BRD as written).* Rejected — it forces a
  conversation with us before an engineer can prove an install works
  (AC-09.1), and it leaves the degraded floor undefined, which is what
  started this.
- *Free tier with unlimited extensions.* Rejected — many small
  deployments never exceed 4 concurrent calls, so with no extension cap
  the free tier becomes a product rather than an evaluation.
- *Free tier that is multi-tenant.* Rejected. Every licensed edition is
  multi-tenant with no tenant limit, but an unlicensed multi-tenant
  install is a small operator business run for nothing, and it undercuts
  the one licence that monetises tenancy.
- *Standard / Enterprise tiers replacing the four editions.* Rejected —
  it would invalidate §12.2's price table, the Operator licence and the
  Language Services line to gain a naming convention.
- *Fax as a paid add-on.* Rejected — FBR-R1-16 already promises it in
  R1.1. Charging later for a promised feature is the repackaging partners
  remember.

**Consequences:**
- **VP-2's "unlimited extensions" is now qualified** as "on every
  licensed edition", in §4, §6's competitive table and §12.2. The claim
  competes against paid per-user pricing, which is where it is made; the
  free tier is capped precisely so it reads as an evaluation.
- **AI translation augments the R3 specialist business, it does not
  replace it** (BRD §12.4.2). AI bridges the wait before an interpreter
  joins and covers language pairs with no available specialist; the human
  remains the product where accuracy carries liability. The per-second
  margin ledger and availability tiers are unchanged.
- **Degrading never removes a provisioned tenant or extension** — only
  new call capacity is capped (AC-06.13). An Operator with fifty tenants
  that degrades must not lose forty-nine of them.
- The extension and tenant caps are **entitlement data owned by
  `licensing` but enforced at provisioning time** by the contexts that
  create extensions and tenants. `licensing-capacity-grace` lands the
  channel floor and exposes the caps; enforcing them is a separate change
  against `pbx-core` and `identity`.
- New revenue line 5 in §12.1 (sellable modules) and a module catalogue
  in §12.4. Nothing in that catalogue is core telephony, tenancy, or
  security.

**Related Decisions:** D-11 to D-14 (licensing mechanism), D-24
(multi-tenant seams from the start), D-27 (power before predictive), D-45
(outbound in R1.0), D-46 (three delivery phases)
**Traceability:** BRD §4 VP-2, §6, §8 FBR-R1-12/FBR-R1-16, §9, §10.4,
§12.1, §12.2, §12.4; PRD §11.2, EPIC-06 (US-06.0, AC-06.11–06.14);
`docs/lld/LLD-08-licensing.md`

---

**D-50 · Per-tenant module enablement lives in a Tier-1 `entitlement`
context, not in `identity` or `licensing` (2026-09-09).**

**Decision:** "May tenant T use module M?" is answered by one new Tier-1
bounded context, `entitlement`, which depends on `licensing` (what the
installation is entitled to) and `identity` (which tenants have it
switched on) and is the only place the two are combined.

Consumers — `pbx-core`, `ai-pipeline`, `dialer`, `reporting` — ask
`entitlement` a single question. None of them combines the two sources
itself.

**The invariant it exists to hold: enablement can never exceed
entitlement.** Switching a module on for a tenant when the installation
is not entitled to it fails, and it fails in one place rather than in
four.

**Context:** BR-17 has always required per-tenant AI enablement ("one
tenant may use AI while another on the same deployment does not"), and
BRD §12.2 says partners may enable AI for some tenants, all, or none.
D-49's module catalogue generalises that from AI to every module. But the
two facts live in two contexts that **may not call each other**: HLD 04
§10.1 gives both `identity` and `licensing` "may depend on: nothing". So
the question cannot be answered by either of them alone, and there was
nowhere for it to be answered at all — no table, no port, no context.

**Alternatives:**
- *`identity` stores the flags; every consumer ANDs them with
  `licensing`'s entitlement.* Rejected — the "enablement ≤ entitlement"
  rule would be re-implemented in four contexts, and the first one to get
  it wrong grants a module nobody paid for, silently.
- *`licensing` owns both.* Rejected — it would give `licensing` a
  tenant-scoped, RLS-protected table beside `licensing_state`, whose
  defining property is that it is installation-scoped with no `tenant_id`
  and no RLS, asserted by test (LLD-08 DoD 6). One context holding two
  opposite scoping rules is how that assertion eventually gets reworded
  away.
- *Answer it in the composition root.* Rejected — it is domain logic with
  an invariant, not wiring, and the composition root is not testable as a
  unit.

**Consequences:**
- A tenth bounded context, and **LLD-11** (next free number under D-48;
  `entitlement` is Tier 1). HLD 04 §10.1 gains a row: `entitlement` may
  depend on `licensing` and `identity`, and nothing else may depend on
  those two for this purpose.
- HLD 03 §5 gains a **tenant-scoped enablement table with RLS enabled and
  forced**, unlike `licensing_state`. The two scoping rules now sit in two
  contexts, which is the point.
- **Gating is enforced in the service that performs the operation, never
  in the console** (BRD R-12). An entitlement check reachable only through
  the UI is bypassed by calling the endpoint.
- It is Phase A work only to the extent Phase A sells a module. The
  channel floor in `licensing-capacity-grace` does not depend on it.

**Related Decisions:** D-24 (multi-tenant seams), D-43 (API-first
consumer test), D-48 (one LLD per context), D-49 (hybrid licensing),
D-51 (tenant count as an entitlement field)
**Traceability:** BRD §9 BR-17, §12.2, §12.4, §16 R-12; HLD
`04-bounded-contexts.md` §10.1, `03-domain-model.md` §5

---

**D-51 · Tenant count is an entitlement field (`MaxTenants`), not a
purchasable module (2026-09-09).**

**Decision:** The signed licence payload carries **`MaxTenants`**
alongside edition, channel capacity, expiry and instance identity.

| `MaxTenants` | Meaning |
|---|---|
| `1` | Single-tenant installation. Additional-tenant provisioning is refused; the default tenant context is implied |
| `0` | Unlimited — the Operator licence |
| `> 1` | That many tenants |

An unlicensed installation is `MaxTenants = 1` (D-49, BR-LIC-01). Every
licensed edition is unlimited.

**Row-level security is mandatory in every mode, including
single-tenant.** There is one schema, one code path, and no
single-tenant build. `MaxTenants` constrains *provisioning*; it never
changes how isolation works, and it is never a reason to skip a
`tenant_id` or a policy.

**Refusal is `FAILED_PRECONDITION`, not `PERMISSION_DENIED`.** The caller
has the permission; the installation lacks the entitlement. Returning a
permission error for a licensing condition makes the RBAC audit trail
lie — an access-control investigation would find denials that had nothing
to do with access control — and it tells the administrator to check
their roles when they need to check their licence. AC-06.14 requires a
distinct entitlement reason for exactly this reason.

**Only creation is gated.** `CreateTenant` is refused at the cap;
**reads are not.** A single-tenant installation still has one tenant, and
its console must be able to list it. Gating the read would break a
legitimate state to enforce a limit the write already enforces.

**Context:** D-49 settled that the Unregistered tier is single-tenant and
that tenancy is never sold as a module or differentiated between licensed
editions. It did not say how the cap is carried or enforced. Naming a
token field closes that, and it keeps tenancy out of the module
catalogue: what a partner buys is a tenant *count*, in the same payload
as their channel count — not a feature that might be absent.

**Alternatives:**
- *Multi-tenancy as a purchasable module.* Rejected, and this is the
  distinction worth preserving: tenant isolation is enforced by RLS on
  every table. A security property that is absent unless purchased is not
  a property. BRD §10.4 keeps tenancy off the module list; `MaxTenants`
  is a quantity on the licence, which is a different thing.
- *A separate single-tenant build or schema.* Rejected outright — two
  code paths, one of them under-tested, and the under-tested one is the
  one handling isolation.
- *`PERMISSION_DENIED` at the cap.* Rejected — see above.

**Consequences:**
- LLD-08 §3's `LicenseToken` gains `MaxTenants int`, and the entitlement
  it publishes carries the tenant cap alongside the extension cap
  (D-49) — both published by `licensing`, both enforced where the thing
  is created.
- LLD-02's tenant provisioning gains an entitlement precondition. Nothing
  implemented today is wrong: no cap is enforced now, so this is additive.
- The degraded and expired states report `CAPACITY_DEGRADED_EXPIRED` as
  their reason code, distinct from the unlicensed reason (D-49) and from
  an over-capacity refusal on a valid licence.
- BRD §12.2's entitlement matrix states `MaxTenants` per edition.

**Related Decisions:** D-02 (per-channel pricing, unlimited extensions),
D-12/D-14 (degrade, never disable), D-24 (multi-tenant seams from the
start), D-49 (hybrid licensing), D-50 (where enablement is answered)
**Traceability:** BRD §10.4 BR-LIC-01/BR-LIC-03, §12.2; PRD §11.2,
EPIC-06 AC-06.14; `docs/lld/LLD-08-licensing.md` §3,
`docs/lld/LLD-02-identity.md`

---

**D-52 · Every entitlement is an activated signed token; a
never-activated installation is in Setup, which is not a licence state
(2026-09-09). Supersedes D-49's "no key, no activation step".**

**Decision:** There is no unsigned entitlement. Obtaining a token —
**including the free one** — requires registering on the central portal,
which is where lead capture happens.

| State | Reached by | Can place ordinary calls? |
|---|---|---|
| **Setup** | First boot, no token ever applied | **No.** Admin console only: instance ID, fingerprint, and how to register |
| **Free Community** | Applying the free perpetual token | Yes — 4 SC, 10 extensions, `MaxTenants: 1` |
| Licensed edition | Applying a purchased token | Yes — purchased capacity, unlimited extensions and tenants |
| **Degraded** | Expiry, elapsed grace, or tampering on a previously valid licence | Yes — **4 SC**, the Free Community floor as a constant |

**Setup and Degraded are not the same state and must never be merged.**
A never-activated box has no phone system to protect. A degraded box has
one, in production, with calls in progress — and D-12, BR-09 and INV-03
forbid disabling it. Degradation therefore falls back to the 4 SC
constant, **never to Setup**. Collapsing the two would mean an expired
licence silently disabling a working installation, which is the single
outcome this product promises never to produce.

**Context:** D-49 described the free tier as zero-configuration — "no
key, no activation step, and no database seeding" — on the stated
premise that this matched the 3CX benchmark. **That premise was wrong:**
3CX issues its free tier as a key obtained after registration. Correcting
it changes the model rather than a detail, so it is recorded here rather
than edited into D-49.

**What this buys, beyond accuracy:**
- **Every entitlement is signed.** There is no "no row means 4 channels"
  branch, so there is no unsigned path into the capacity decision. The
  domain gets simpler, not more complex.
- **Every deployment is a known contact**, including free ones, which is
  the commercial point of requiring registration at all.
- **The upgrade path is already wired** — the portal holds the instance
  ID, so Free → paid is a new token, not a reinstall.

**Alternatives:**
- *Keep zero-config 4 SC (D-49 as written).* Rejected — it forgoes lead
  capture on every free deployment, and it keeps an unsigned entitlement
  path alive purely for first-run convenience.
- *Time-boxed evaluation, then require a key.* Rejected — it needs a
  first-boot date that survives restarts and cannot be reset by
  reinstalling, which is a tamper-resistance problem bought for a
  convenience.

**Consequences, including the ones that cost us:**
- **AC-06.11 and US-06.0 are rewritten.** "Places calls with no key" is
  no longer true; what must stay true is that obtaining and applying the
  free key is self-service, immediate, and needs no conversation with us.
- **The dev stack and both e2e suites need a signed fixture token**,
  since they place real calls. The fixture is signed by a test key that
  is never the production key (D-39). This is a real cost, and it buys a
  test path that exercises activation rather than bypassing it.
- `licensing-capacity-grace` changes before it is applied: the
  missing-row case becomes Setup (no call path), not a 4-channel floor.
- **Air-gapped deployments are unaffected** — §10.3's manually issued
  offline licence already covers them.
- **R1.0 ships OCI images** (`atsapbx/*`, compose and Helm); the turnkey
  ISO/AMI/OVA appliance is **R1.1**, because an appliance build pipeline,
  an OS patching path and appliance QA are not in §12.3's effort
  estimate. The fingerprint tolerance (3-of-5, D-13) is load-bearing for
  the AMI case, where a rebuild changes MAC and host UUID.

**Related Decisions:** D-12/D-14 (degrade, never disable), D-13 (weighted
fingerprint), D-39 (no secrets in fixtures), D-49 (hybrid licensing —
superseded on this point), D-51 (`MaxTenants`), D-53 (tamper-resistant
storage)
**Traceability:** BRD §10.4 BR-LIC-01, §10.5, §12.2; PRD §11.2, EPIC-06
(US-06.0, AC-06.11); `docs/lld/LLD-08-licensing.md`

---

**D-53 · The stored entitlement is the signed payload, re-verified on
load — not a row of parsed columns (2026-09-09).**

**Decision:** `licensing_state` stores the **signed licence payload and
its signature** as the source of truth. The entitlement in force is
derived by verifying that payload on load and caching the result in
memory. The parsed columns — edition, capacity, `max_tenants`,
`max_extensions`, expiry — are a denormalised convenience for display and
support, and are **never read to make an entitlement decision**.

**Context:** the schema as designed (HLD 03 §5) stored only the parsed
claims. Ed25519 verification happened once, at apply time, and the proof
was then discarded. On a self-hosted product the customer owns the
database, so:

```sql
UPDATE licensing_state SET capacity = 1000, max_tenants = 0;
```

defeated licensing completely — without forging a signature, touching a
binary, or defeating the fingerprint. The cryptography protected the
*transport* of a licence and nothing about its *storage*, which is the
half that matters when the licence lives on someone else's hardware.

**This is proportionate, not an arms race.** BRD §10.3 is explicit that
enforcement should stop casual over-use rather than defeat a determined
attacker. Re-verification stops the partner who edits a row to get twenty
channels — the actual threat. It does not stop someone patching the
binary, and nothing reasonable would.

**Alternatives:**
- *Re-verify on every entitlement read.* Rejected — capacity is consulted
  on every call setup, and AC-06.3 forbids putting licence work in the
  call path. Verify on load and on change; cache in memory.
- *A checksum or HMAC over the parsed columns.* Rejected — the key would
  have to live on the same machine as the data it protects.
- *Accept the exposure and rely on the daily entitlement check.* Rejected
  — the check is unreachable for 7 days by design (D-14), which is
  exactly the window an edited row would be used in.

**Consequences:**
- HLD 03 §5 `licensing_state` gains `signed_payload BYTEA NOT NULL` and
  `signature BYTEA NOT NULL`; the claim columns remain, explicitly as a
  cache.
- **A tampered row fails verification and degrades** to the 4 SC floor
  with the tamper reason (BRD §10.2) — it never disables, and never
  silently grants what the row claimed.
- LLD-08 DoD gains an assertion that editing a claim column changes no
  entitlement decision, which is the test that would have caught this.
- Verification cost is paid on load and on `ApplyLicenseKey`, never
  during call setup.

**Related Decisions:** D-11 (signed licence payloads), D-14 (offline
grace), D-49, D-51, D-52
**Traceability:** BRD §10.2, §10.3; HLD `03-domain-model.md` §5;
`docs/lld/LLD-08-licensing.md` §5, §8

---

**D-54 · Development and test entitlement comes from a separately signed
token, never a bypass flag (2026-09-09).**

**Decision:** Signature verification **always runs**. There is no
environment variable, build tag, or configuration value that skips it,
and no code path in which an entitlement is accepted unverified.

What varies between builds is the **set of trusted public keys**:

| Build | Trusts |
|---|---|
| Production (default) | The production public key, only |
| Development / CI | The production key **and** a development key |

The development public key is injected at build time
(`-ldflags -X`), and is **empty by default**. A build that forgets the
flag is the strict build, so the failure mode is fail-safe rather than
fail-open.

`ATSAP_LICENSE_TOKEN` carries a **signed token**, never a mode. It exists
so a compose stack, a Helm chart, or a CI job can supply an entitlement
without a portal round trip, and it is the same variable in every
environment. A development-signed token pasted into a production binary
fails verification exactly as a forgery does, because that binary has
never heard of the key that signed it.

The development token grants unlimited capacity, unlimited tenants and
extensions, and a far-future expiry — it is committed to the repository,
because against a production binary it is worthless.

**Context:** the requirement was an environment variable giving
development an unlimited licence. Implemented as a mode flag
(`LICENSE_MODE=dev`) that skips verification, it would be a single string
that disables every layer of D-11, D-13, D-14 and D-53 at once — no
forgery, no SQL edit, no patched binary required. It would become the
tampering method, and the mechanism it bypassed would be exercised only
in production, where it is least safe to be first tested.

Separating keys instead of separating code paths keeps development
convenient and keeps exactly one verification path under test.

**Alternatives:**
- *`LICENSE_MODE=dev` skipping verification.* Rejected — see above.
- *A production-trusted "developer" key with unlimited entitlement.*
  Rejected — it is the same bypass with a signature on it. Once the key
  is trusted in production, possessing it is possessing a free unlimited
  licence, and it cannot be revoked without shipping a new binary.
- *Committing a development private key so tests can mint tokens.*
  Rejected (D-39). Unit tests generate an ephemeral Ed25519 keypair
  in-process and inject the public key, so no private key material exists
  in the repository at all. Only the dev stack's pre-signed token is
  committed, and its private key is held offline.

**Consequences:**
- `licensing/domain` verifies against a **key set**, not a single key, so
  adding or rotating a key is data rather than a code change.
- A test asserts that a binary built with no `-ldflags` trusts **exactly
  one** key, and that a development-signed token is rejected by it. That
  test is what stops the dev key drifting into a release.
- The dev stack and both e2e suites gain the committed development token
  through `ATSAP_LICENSE_TOKEN` (D-52's stated cost), rather than a
  fixture that bypasses activation. The suites therefore exercise the
  real activation and verification path.
- The token is also how an air-gapped or automated deployment applies a
  licence without the console (§10.5), so this is not development-only
  machinery.

**Related Decisions:** D-11 (signed payloads), D-13 (fingerprint
tolerance), D-14 (offline grace), D-39 (no secrets in fixtures), D-52
(every entitlement is activated), D-53 (tamper-resistant storage)
**Traceability:** BRD §10.5, §10.6; `docs/lld/LLD-08-licensing.md` §5,
§10; `docs/TESTING.md`

---

**D-55 · PostgreSQL is not a swappable backing service, and that is a
deliberate exception to twelve-factor IV (2026-09-09).**

**Decision:** The platform requires PostgreSQL. It is not abstracted
behind a database-neutral layer, no other engine is supported, and
"support MySQL" is not a configuration change but a redesign of the
tenant-isolation model.

**Why, measured rather than asserted.** The migrations currently declare
**13 tables with `ENABLE` *and* `FORCE ROW LEVEL SECURITY`, 12
`CREATE POLICY` statements**, and policies written against
`current_setting('app.tenant_id')::uuid`. Alongside that: `pgx/v5` (not
`database/sql`), `JSONB`, `gen_random_uuid()`, `SKIP LOCKED` in the
outbox, and Asterisk's `res_config_pgsql` for the PJSIP Realtime
projection (D-47).

**MySQL has no row-level security of any kind.** Porting therefore means
moving tenant isolation out of the database and into application `WHERE`
clauses — exchanging an invariant the database enforces for one that code
review enforces, in a product whose central promise is that one tenant
cannot see another's data (D-24). One forgotten clause is a cross-tenant
leak, and it would be found by a customer rather than by a test. That is
not a swap.

**This is the twelve-factor deviation, stated plainly.** Factor IV treats
backing services as attached resources. We honour it for NATS (D-57) and
for the media engine (D-41), and we break it here on purpose. A factor is
a default, not a law; the cost of following it is the security model.

**Alternatives:**
- *An abstraction layer or ORM for portability.* Rejected twice over —
  TOOLSET §2.2 already rejects ORMs, and a portability layer must target
  the intersection of engines, which excludes RLS. It would deliver the
  weaker model on both engines rather than one.
- *Application-enforced isolation on any SQL engine.* Rejected — see
  above.
- *Keep it undocumented.* Rejected — an undocumented lock-in is
  discovered by whoever proposes MySQL in a partner conversation, and by
  then it sounds like an oversight rather than a decision.

**Consequences:**
- Any partner requirement for a different SQL engine is a **commercial
  no**, answered from this entry rather than re-derived.
- Should it ever become necessary, the entry point is not a driver: it is
  a decision to re-express tenant isolation, and it needs its own
  decision superseding D-24.
- Postgres major-version upgrades stay ordinary work; nothing here
  pins a version.

**Related Decisions:** D-24 (multi-tenant seams enforced by the
database), D-41 (engine portability by contract — the contrasting case),
D-47 (PJSIP Realtime via `res_config_pgsql`), D-57 (NATS *is* swappable)
**Traceability:** `docs/TOOLSET.md` §2.2, §3; HLD `03-domain-model.md`
§5; `api/migrations/`

---

**D-56 · No distributed cache in R1.0; authorization state is never
cached, verified state always is (2026-09-09).**

**Decision:** No Redis, Memcached, or equivalent. R1.0 ships with
PostgreSQL and NATS as its only stateful dependencies.

Where caching does happen, one rule decides it:

| Kind of state | Cached? | Example |
|---|---|---|
| **Authorization** — may this caller do this, right now | **Never** | Token validation reads the database per request (D-42), so disabling an account takes effect immediately |
| **Verified, self-proving** — expensive to derive, cheap to re-check | **Always, in process** | The entitlement is verified once on load and held in memory (D-53), because verifying per call setup would put cryptography in the call path (AC-06.3) |

**Context:** the two existing decisions on this each covered one case and
neither stated the principle, so "why is there no cache?" had no answer
to point at. D-42 rejected caching `ValidateToken` because "the caches
that fix it reintroduce the window the design closed". D-53 requires
caching the verified entitlement for the opposite reason. Both are right,
and the rule that reconciles them is above.

**Why no cache service in R1.0:**
- Deployment is single-node (D-08); an in-process map is faster than a
  network round trip and has no coherence problem to get wrong.
- Every cache adds a staleness window, and the two places pressure would
  push us to add one — authentication and licensing — are exactly where a
  staleness window is a security defect rather than a latency win.
- It is **another service the partner must run, back up, secure and
  patch** on their own hardware. For self-hosted software that cost is
  theirs, not ours, which makes it easy to underestimate.
- Nothing has measured PostgreSQL as the bottleneck. Adding a cache
  before a measurement is guessing with someone else's operational
  budget.

**Where a cache legitimately belongs later**, none of it R1.0: reporting
aggregates, IVR flow definitions, and PJSIP realtime lookups on the
Asterisk side. All read-mostly, none security-critical.

**Alternatives:**
- *Add Redis now for sessions and rate limiting.* Rejected — sessions are
  stateless JWTs validated against the database by design, and rate
  limiting is deferred (LLD-02 §10.5). Neither has a cache-shaped problem
  yet.
- *Cache authorization with a short TTL.* Rejected — "revoked but works
  for another thirty seconds" is the exact behaviour D-42 refused, and a
  TTL only sets the size of the hole.

**Consequences:**
- Adding any cache service is a decision that supersedes this one, and it
  must name what was measured.
- In-process caches remain fine and are used; they die with the process,
  which for verified state is correct rather than a limitation.
- Multi-node work (D-08) revisits this, because an in-process cache of
  verified state is per-node — correct, but no longer shared.

**Related Decisions:** D-08 (nothing built for year-three scale), D-42
(identity in-process; no auth caching), D-53 (verified entitlement is
cached in memory), D-55 (PostgreSQL is the datastore)
**Traceability:** `docs/TOOLSET.md` §4; `docs/hld/08-performance.md`;
`docs/lld/LLD-02-identity.md` §10.5

---

**D-57 · Event publishing goes through a port; the broker is confined to
one package and one wire (2026-09-09).**

**Decision:** Application code publishes domain events through a
`ports.EventPublisher` interface and the transactional outbox. It never
imports a broker package. NATS remains the implementation, behind
`internal/nats`, wired only in `cmd/atsap-api`, and a depguard rule
enforces that.

**Context:** the broker is *already* confined — `internal/nats` is
imported by exactly one file, `cmd/atsap-api/main.go`, and application
code writes to the `outbox` table rather than to NATS. That is the right
architecture and it happened for a good reason: the outbox is the seam.

> **Correction, 2026-09-09.** This entry as first written said "there is
> no interface". That was wrong: `postgres.OutboxPublisher` exists
> (`api/internal/postgres/outbox.go:28`) and `internal/nats.Publisher`
> asserts conformance to it. The accurate statement is narrower — the
> port exists but is **owned by the persistence package rather than a
> `ports` package**, and no gate enforces the boundary. The decision
> below is unchanged; only its premise was overstated.

What is unenforced is the boundary, not the abstraction: nothing fails if
someone imports `internal/nats` from an application package tomorrow, and
the port sits in `internal/postgres`, which is a persistence concern
owning a messaging contract. A property that is true, valuable, and
unenforced is a property with a short life (the same reasoning as D-48's
gate work and the `pbx-acl-boundary` rule).

**Consequences:**
- `OutboxPublisher` moves out of `internal/postgres` into a ports
  package, and a `broker-stays-behind-the-outbox` depguard rule denies
  `atsap-api/internal/nats` from every `internal/*/application`,
  `domain` and `rpc` package. Fault-injected once to prove it rejects.
- **Replacing NATS is then a bounded change**: `internal/nats` plus one
  line in the composition root. Kafka, RabbitMQ or Redis Streams all sit
  behind the same outbox drain, because the durability decision was made
  by the outbox, not by the broker.
- This is deliberately the opposite of D-55. Where PostgreSQL's
  substitution cost is the security model, the broker's is a package —
  so twelve-factor IV is honoured here and knowingly broken there.
- No behaviour changes. This is a boundary made explicit, not a
  redesign.

**Related Decisions:** D-41 (engine portability by contract — the same
pattern for Asterisk), D-48 (gates over stated intentions), D-55
(PostgreSQL, the contrasting case)
**Traceability:** `api/internal/nats/`, `api/cmd/atsap-api/main.go`,
`.golangci.yml`; `docs/TOOLSET.md` §3

---

**D-58 · Idempotency and immutability are named invariants, not
properties that happen to hold (2026-09-09).**

**Decision:** Two properties the design already depends on are stated
here, with where each applies and how each is enforced, because until now
they held in several places for several unrelated reasons and in one
place did not hold at all.

**Idempotency — required wherever an operation can be retried or
redelivered:**

| Site | Mechanism |
|---|---|
| Outbox → NATS | **At-least-once by construction.** The worker publishes and only then marks the row published; a crash between the two republishes it. The outbox row id is sent as the JetStream message id, so a repeat inside the dedupe window is discarded server-side |
| Every event consumer | **Must be idempotent.** Dedupe narrows the window, it does not close it — a republish after the window still arrives |
| `ProjectExtension` (D-47) | Upsert-based; already idempotent |
| ARI redelivery | Guarded by the `CallTerminated` state check |
| `ReleaseCapacity` | **Keyed by call id**, so a repeat release is a no-op rather than a decrement |
| `ApplyLicenseKey` | Applying the same token twice succeeds, changes nothing, and does not refresh `last_confirmed_at` |
| Public creates | Not idempotent by default; a caller-supplied idempotency key is decided per endpoint (`docs/API.md` §3a) |

**Immutability — required wherever a record is evidence:**

| Site | Why |
|---|---|
| `audit_logs` | BR-07. Append-and-read-only, asserted by a tripwire test |
| Usage ticks and call detail | BR-07 — partners bill from this data, so corrections are separate attributable adjustments, never edits |
| Outbox rows | Written once; only `published_at` is set afterwards |
| Residency zone | Fixed at provisioning, asserted (LLD-02) |
| The signed licence payload | D-53 — the mutable copy exists but is never authoritative |

**Context:** the question "does this design need idempotency and
immutability" had no single answer to point at. Both were relied upon in
four or five places each, enforced variously by a database constraint, a
test, an upsert, or nothing. That is how a property survives until the
day someone adds a code path that does not know about it.

**The one place it did not hold.** `ReleaseCapacity` as first designed
was at-most-once by *guard* — correct only while `finalizeTermination`
remains the single termination funnel. The failure direction is what
makes it worth fixing rather than documenting: a double release
under-counts, so the installation permits calls it should refuse. That is
licence leakage which no test notices, because everything continues to
work. Keying release by call id makes the invariant independent of the
guard.

**Alternatives:**
- *Exactly-once delivery.* Not available, and pursuing it is the classic
  distributed-systems error. At-least-once plus idempotent consumers is
  the reachable design; dedupe by message id makes the common case
  cheap.
- *Reverse the outbox ordering — mark published, then publish.* Rejected:
  it trades duplicates for lost events, and a lost usage event is
  unbillable revenue that nothing can reconstruct.
- *Leave both properties implicit.* Rejected — that is the state that
  produced the `ReleaseCapacity` gap.

**Consequences:**
- The JetStream stream carries a five-minute dedupe window, and a test
  fault-injected against it (publishing the same row twice delivers one
  message; removing the message id delivers two).
- `ReleaseCapacity` takes a call id rather than a channel count.
- New event consumers state how they are idempotent, in the change that
  adds them. "The broker handles it" is not an answer past the window.
- Nothing here changes an existing behaviour; it names what was already
  required and closes the one case that was not met.

**Related Decisions:** D-24 (multi-tenant seams), D-47 (upsert-based
projection), D-53 (the signed payload is authoritative), D-57 (the broker
sits behind the outbox)
**Traceability:** BRD §9 BR-07; `docs/API.md` §3a;
`api/internal/nats/publisher.go`, `api/internal/postgres/outbox.go`

---

**D-59 · Context-list drift is a gate, not a discipline (2026-09-09).**

**Decision:** `scripts/check-docs.sh` check 5 asserts that every bounded
context in HLD 04 §10.1's matrix has both a section in that document and
a row in `docs/lld/README.md`'s index table. A context added to the
matrix and nowhere else fails CI.

**Context:** ten documents enumerate the bounded contexts — HLD 04's
ASCII graph, its §10.1 matrix, its per-context sections, its §10.2 build
sequence, HLD 01's package tree, HLD 03's schema, the TRD's list and
mermaid view, `docs/lld/README.md`'s index, and D-48's map. Adding
`entitlement` (D-50) meant touching all ten.

The audit that accompanied it found the predictable result of having no
gate:

| Drift | Fixed |
|---|---|
| `hld/README.md` claimed "all 9 bounded contexts" | → 10 |
| BRD FBR-R1-12 said an unlicensed install "shall run permanently at a capped free floor rather than refusing service" — **contradicting D-52** | Rewritten |
| BRD used "Unregistered tier" in five places after §10.4 renamed the states to Setup / Free Community | Aligned |
| LLD-08 §3 declared `VerifyToken(..., pubKey ed25519.PublicKey)` after D-54 replaced it with a key set | Corrected |
| LLD-08 §3 declared `ReleaseCapacity(ctx, channels int)` after D-58 keyed it by call | Corrected |
| LLD-08 §9's handoff table predated the `licensing-capacity-grace` re-slice | Rewritten |
| LLD-08 §1's "zero `telephony-core` changes" tripwire, after it had already fired | Narrowed and recorded |
| D-49's "no key, no activation" bullet, superseded by D-52 but unmarked | Struck through, with the pointer |

Every one of those was written by someone who had the correct decision in
front of them, days or hours earlier. That is what makes it a gate
problem rather than a care problem.

**What the gate does not claim.** It checks two relationships, not ten.
Prose counts ("all 9 bounded contexts"), narrative descriptions, and the
ASCII and mermaid graphs are not machine-checkable, and check 5 does not
pretend to cover them — a gate believed to cover more than it does is
worse than no gate (D-28). It covers the two an agent actually reads
before proposing a change: is there a section describing this context,
and is there exactly one LLD for it.

**Found by fault injection, and worth recording.** The first version of
check 5 grepped the whole of `docs/lld/README.md`. Deleting a context's
index row still passed, because the delivery-phase prose mentions the
same context. The check now scopes to the index table. A gate's first
test is whether it can fail, and this one could not — for one of its two
branches — until it was injected against.

**Alternatives:**
- *A single generated source of contexts that all ten documents render
  from.* Rejected for now — the documents are prose for different
  audiences, and generating them would flatten the reason each mentions a
  context. Revisit if the count of enumerating documents grows again.
- *Rely on review.* Rejected — that is what produced the table above.

**Related Decisions:** D-28 (machine gates over review for anything
checkable), D-48 (one LLD per context; stable identifiers), D-50 (the
context whose addition prompted this)
**Traceability:** `scripts/check-docs.sh` check 5;
`docs/hld/04-bounded-contexts.md` §10.1; `docs/lld/README.md`

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
