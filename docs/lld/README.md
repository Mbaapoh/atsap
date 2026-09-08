# Low-Level Design (LLD) Index

LLDs are the implementation-facing layer below the HLD (`docs/hld/`): exact
package paths, function signatures, SQL, and test plans for one bounded
context at a time. Where the HLD says *what shape* a context has (ports,
aggregates, events), the LLD says *exactly how to build it*.

Per [[../DECISIONS.md]] D-29, this stays in `docs/` as a cross-cutting
document, not an OpenSpec spec. **Relationship to OpenSpec:** each LLD below
is written to become the `design.md` (and a source for the delta `specs/`)
of one OpenSpec change under `openspec/changes/<name>/` when it's time to
implement it. `openspec/config.yaml`'s `context:` field already points
Claude Code and OpenCode at `docs/hld/`, `docs/TRD.md` and `docs/DECISIONS.md`,
so `/opsx:propose` for an LLD's change picks this up automatically — an LLD
doc is written, then proposed, not invented fresh by the agent at propose
time.

**The `#` column is a stable identifier, not a build order.** LLD-08
(licensing) is Phase A work that must land before LLD-04 onwards; it took
the next free number when it was split out of LLD-02 on 2026-09-08,
because renumbering would have broken citations inside archived,
immutable changes. Read the order from the dependency graph and the
delivery-phase table below, never from the number.

Build order is fixed by the dependency graph in
[`../hld/04-bounded-contexts.md` §10](../hld/04-bounded-contexts.md#10-bounded-context-build--dependency-graph).
LLDs are written and implemented **one at a time, in that order** — writing
LLD-03 before LLD-01 is built is exactly the scope creep the graph exists
to prevent.

**One LLD covers one bounded context.** LLD-02 briefly covered two
(`identity` and `licensing`) on the reasoning that they shared a cutover;
delivery disproved it, and it was split on 2026-09-08. Two contexts in
one document hide the fact that one half is finished while the other has
not started — which is exactly what happened.

**Every LLD states its API surface, and every change that changes that
surface updates [`../API.md`](../API.md) in the same commit.** Per D-43, a
capability gets an RPC in the change that builds it when it has a named
R1.0 consumer (the portal, a partner developer per US-05.1, or a test
harness); otherwise it stays a Go port and the LLD records why. An LLD
whose ConnectRPC section says only "ports for now" without naming the
consumer test has not made the decision, it has postponed it — which is
how LLD-02 nearly shipped a JWT cutover with no way to obtain a JWT.
`scripts/check-docs.sh` fails if a proto defines an RPC that `API.md`
does not list, so the inventory cannot rot quietly.

## Index

| # | LLD | Bounded context | Status | OpenSpec change (when proposed) |
|---|---|---|---|---|
| 01 | [Telephony Core Walking Skeleton](LLD-01-telephony-core-walking-skeleton.md) | `telephony-core` | Implemented & archived | Living spec [`telephony-core/call-lifecycle`](../../openspec/specs/telephony-core/call-lifecycle/spec.md) |
| 02 | [Identity](LLD-02-identity.md) | `identity` | Implemented & archived | Living specs [`identity/authentication`](../../openspec/specs/identity/authentication/spec.md), [`access-control`](../../openspec/specs/identity/access-control/spec.md), [`tenant-provisioning`](../../openspec/specs/identity/tenant-provisioning/spec.md), [`audit-log`](../../openspec/specs/identity/audit-log/spec.md), [`public-api`](../../openspec/specs/identity/public-api/spec.md) |
| 08 | [Licensing](LLD-08-licensing.md) | `licensing` | Draft — nothing built; **Phase A work** despite the number | — |
| 03 | [PBX Core](LLD-03-pbx-core.md) | `pbx-core` | In progress — extensions implemented & archived; trunks, routes, call placement and inbound remain | Living specs [`pbx-core/extension-management`](../../openspec/specs/pbx-core/extension-management/spec.md), [`pbx-core/endpoint-projection`](../../openspec/specs/pbx-core/endpoint-projection/spec.md) |
| 04 | Compliance & Reporting | `compliance`, `reporting` | Not started — `compliance` is a **release gate** for LLD-06's dialling (D-45) | — |
| 05 | Webhook Delivery & AI Pipeline | `webhook-delivery`, `ai-pipeline` | Not started | — |
| 06 | Dialer (power dial in R1.0; predictive in R2) | `dialer` | Not started — **R1.0 scope since D-45**; needs `compliance` (LLD-04) as a release gate | — |
| 07 | Console — administration and partner portal (`portal/`) | — (frontend; consumes the public API only) | **Not one LLD.** Delivered as a slice per phase (D-46): A with `pbx-core` basics, B with IVR and reporting, C with campaigns | — |

### Delivery phases (D-46)

R1.0 is delivered in three phases, each ending in something a partner can
be shown, each continuing from what is already implemented:

| Phase | A partner can… | Backend | Console slice |
|---|---|---|---|
| **A** | install, licence, and make and receive real calls, configured entirely in the UI | finish LLD-02 licensing; LLD-03 minimal — extensions, a trunk, basic routing | login, users and roles, extensions, trunk, licence status |
| **B** | run it as a business phone system | LLD-03 completion — IVR, auto-attendant, business hours, queues; LLD-04 `reporting`; recording governance | visual IVR builder, queues, call history, recording policy |
| **C** | run an outbound operation | LLD-04 `compliance` **first**, then LLD-06 `dialer` (preview and power) | campaigns, contact lists, compliance configuration |

Predictive pacing is R2 (D-27, D-45).

Phase A is drawn before it is built:
[`../hld/18-phase-a-user-flows.md`](../hld/18-phase-a-user-flows.md) has
the five administrator journeys it must support and the console slice
they imply, and
[`../hld/17-data-model-erd.md`](../hld/17-data-model-erd.md) has the
entity relationships those journeys write, including which tables Phase A
adds. LLD-03 is written against those, not invented alongside them.

**API-first applies per phase, not per release.** A phase's endpoints
land before its console slice, and the slice uses only public endpoints —
so each slice is the first honest test of whether that phase's API can
actually carry an interface. A screen that cannot be built without a
private endpoint means the API is wrong, and it is fixed in that phase.

### The console depends on nearly everything, which is why it is sliced

PRD EPIC-10 covers one web application for both tenant administration and
partner commerce, and PRD principle 4 fixes what it must hide: **no
customer edits an Asterisk file or needs to know Asterisk exists** — the
expectation VitalPBX and 3CX set. Everything is configured in the
console: extensions, call flows, IVR, auto-attendant, queues,
contact-centre setup, trunks, routing, recording, licences.

It sits last in the order because it consumes APIs the earlier LLDs
provide, and by D-43 it may use *only* public endpoints — a console-only
back door is a defect (AC-05.1, AC-10.7):

| Needs | From |
|---|---|
| Login, users, roles, permissions | `identity` (LLD-02) — already landed |
| Extensions, trunks, routing, IVR flows | `pbx-core` (LLD-03) |
| Call detail, usage, quality views | `reporting` (LLD-04) |
| Licence display and activation | `licensing` (LLD-02) |
| Campaign management | `dialer` (LLD-06, R2) |

Two consequences worth stating before anyone plans a date. It cannot be
built in parallel with LLD-03 in any meaningful way, because the
configuration APIs it drives do not exist yet. And it is a substantial
product in its own right — an IVR builder alone carries AC-03.6 ("a
non-engineer builds a two-level IVR unaided in under 30 minutes"), which
is a usability bar, not a screen.

### How configuration reaches Asterisk — settled by D-47

All configuration is performed through the portal, which translates it
into Asterisk state — administration, call flows, IVR, contact-centre
setup, SIP trunks — so no production Asterisk config is hand-edited.
**D-47 settles how**: the ACL projects the domain row into ACL-owned
`ps_*` tables and Asterisk reads them via **PJSIP Realtime**. No file
generation, no reload, no "Apply Config" step. Proven on our own
Asterisk 22.8.2 on 2026-09-07 — a phone completed a real `REGISTER`
against an extension that existed only as a database row, and deleting
the row deprovisioned it just as immediately. See D-47 for the full test
matrix, the rejected alternatives, and the consequences LLD-03 inherits
(globally unique projected ids, no RLS on `ps_*`, least-privilege grants
for the Asterisk database role).

**Only static registration objects are projected** — endpoints, auths,
aors, trunks. **No dialplan is generated**: call routing, IVR,
auto-attendant and queues are interpreted live in Stasis by our own
application. This is where we diverge from FreePBX, which compiles IVRs
into `extensions_additional.conf` and needs its reload because of it.

**What ARI does and does not do, since this is easy to conflate.** ARI is
*call control*: originate, bridge, play, hang up — proven since LLD-01
and unaffected by D-47. ARI's `/endpoints` resource is **read-only**
(`GET`, plus messaging and refer); there is no create, and
`PUT /ari/asterisk/config/dynamic/res_pjsip/endpoint/...` returns
`403 "Cannot create sorcery objects of type 'endpoint'"` on a default
build. So "we use ARI" answers how a call is controlled, not how an
extension a phone can register to comes to exist. That second question is
what D-47 answers, and it lives entirely inside the ACL: the console
calls our API, our API writes our database, and the ACL makes Asterisk
aware. No part of the choice is visible to the console, the API, or a
partner.

`docs/API.md` §3a fixes the contract-side consequence, unchanged by the
outcome: the API models the domain (an `Extension`, a `CarrierTrunk`),
never Asterisk's own objects, and activation is not assumed
instantaneous.

Do not start row *N+1* until row *N* is implemented and its walking-skeleton
or integration test passes — per D-26 (dependency-first within a release,
risk-first across it).

## Granularity: an LLD is neither a feature nor a task

Three distinct grains, not two:

| Level | Grain | Lives in |
|---|---|---|
| **LLD** | One bounded context, built once, evolves over many changes | `docs/lld/LLD-NN-*.md` |
| **OpenSpec change** | One feature-sized slice of that context | `openspec/changes/<name>/` |
| **Task** (`tasks.md` item) | One sitting's worth of work | inside a change folder |

One LLD decomposes into several OpenSpec changes over its lifetime (e.g.
`LLD-01` into "originate/bridge/hangup happy path", then "hold/resume",
then "attended transfer continuity", then "wire Screening to the stub
licensing/compliance ports") — not one change per LLD. Each change is
independently proposed, applied, and archived; the LLD is the stable
design reference they're all cut from, not itself a unit of delivery.

## The propose → apply → archive loop, bit by bit

1. `/opsx:propose "<context>: <one feature slice>"`. The agent reads
   `openspec/config.yaml`'s `context:` — BRD, PRD, DECISIONS, TRD, HLD, and
   the relevant LLD — and drafts `proposal.md` (cites the BRD/PRD
   requirement realised), delta `specs/` (Given/When/Then scenarios),
   `design.md` (from the LLD), and `tasks.md`.
2. **Review before apply.** This is where BRD/PRD stay authoritative: if
   the draft invents behaviour they don't support, or skips an invariant,
   reject it or `/opsx:update` it — don't let it proceed as drafted.
3. `/opsx:apply` — tasks are worked through and checked off, gated by CI
   (D-28: machine gates over human review for anything checkable).
4. Verify against the owning LLD's Definition of Done section — that is
   the acceptance bar, not "tests pass."
5. `/opsx:archive` — delta specs merge into `openspec/specs/<context>/spec.md`,
   which **becomes the new source of truth for what that context currently
   does**. The next change proposed against this context diffs against
   that living spec, not against the LLD doc again — the LLD stays the
   design *rationale*, the archived spec is the current-*behaviour* record.
6. Repeat for the next feature slice in the same context until the owning
   LLD's Definition of Done is fully met. Only then does the next LLD (per
   the dependency-graph build order above) get written and start its own
   cycle.

**Where feedback flows back up, not just down:** if `apply` surfaces a
requirement that's unbuildable as BRD/PRD/TRD describe it, that is not
silently patched. A local mismatch is fixed with `/opsx:update` on the
in-flight change. A real architectural reversal is escalated as a new
`docs/DECISIONS.md` entry (the way D-32 recorded the Leg→Participant
rename) — engineering iteration never silently rewrites BRD/PRD; it
escalates the conflict to whoever owns that document.
