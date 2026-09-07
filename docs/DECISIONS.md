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
  capacity counter is per-process today (LLD-02 §8) — the first genuine
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

**Consequences:**
- Mechanism stays internal by construction: `ValidateToken`,
  `AuthorizeAction`, and `AuthenticateAPIKey` are what the interceptor
  does *for* a caller, never something a caller invokes. Exposing
  `ValidateToken` would hand out a token-validity oracle.
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
