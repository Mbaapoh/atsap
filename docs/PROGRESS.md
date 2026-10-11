# Progress

Where AtsaPBX stands against its own delivery plan. This file is a
**tracker**, not a design document. The plan is
[DECISIONS.md D-46](DECISIONS.md) (three phases, each ending with a console
slice), the flows are [HLD-18](hld/18-phase-a-user-flows.md), and the
behaviour lives in `openspec/specs/`. When this file and those disagree,
they win and this file is wrong.

**Last updated:** 2026-10-11, after `pbx-inbound-and-emergency` was
archived and `console-apis-phase-a` was proposed.

## Keeping it current

Update this file **in the same commit that archives a change** (the
`/opsx:archive` step):

1. Move the change from *Active* to *Done* with its date and proof tiers.
2. Tick any flow or console screen it completes.
3. Add its open gaps to *Open gaps*, and remove any it closed.
4. Bump *Last updated*.

A row is ticked only when its proof ran green. "Code merged" is not proof.

---

## At a glance

| Phase | A partner can… | Backend | Console | Status |
|---|---|---|---|---|
| **A** | install it, licence it, make and receive real calls, configured entirely in the UI | ~90%: 11 changes archived; console APIs proposed | **Not started** | In progress |
| **B** | run it as a business phone system (IVR, queues, call history, recording) | — | — | Not started |
| **C** | run an outbound operation (compliance first, then the power dialler) | — | — | Not started |

**Proof tiers:** U unit · I integration (real Postgres) · W wire · E e2e
(running licensed app, live Asterisk) · A UAT (real baresip phones, RTP
measured both ways).

## Phase A — flows (HLD-18)

Phase A is done when a partner walks A1 → A5 **in the console**, without a
configuration file, and without being told what Asterisk is.

| Flow | Backend | Proven | Console screen |
|---|---|---|---|
| A1 First run: install to first login, licence | Login ✅. Licence applied via env only ❌, so `ApplyLicenseKey`/`GetLicenseStatus` are pending in `console-apis-phase-a` | E (login) | ☐ Sign in · ☐ Licence status and activation |
| A2 Add a user and give them a role | Create, disable, grant ✅. List users and revoke role ❌, pending in `console-apis-phase-a` | U I W E | ☐ Users, roles, permissions |
| A3 Add an extension a phone can register to | ✅ | U I W E A | ☐ Extensions with registration status |
| A4 Connect a SIP trunk and route outbound calls | ✅ (plus DIDs and emergency profile) | U I W E A | ☐ Trunks and routes with health |
| A5 The first real call (out, in, desk phone, emergency) | Calls ✅. Call history list ❌, pending in `console-apis-phase-a` | U I W E A | ☐ Call history (read-only) |

### Phase A — next steps, in order

1. **`console-apis-phase-a`** (proposed, 0/13): `GetLicenseStatus`,
   `ApplyLicenseKey`, `ListPrincipals`, `RevokeRole`, `ListCalls`.
2. **Console slice A** (to propose): React + TypeScript
   ([D-36](DECISIONS.md)), TypeScript clients generated from the same protos,
   the six screens, and browser e2e (Playwright) walking A1 → A5.
3. **Shared carrier address** (to propose; correctness before selling):
   two trunks on one carrier IP cannot be told apart for inbound calls.

## Bounded contexts (LLD status)

| LLD | Context | Status |
|---|---|---|
| [LLD-01](lld/LLD-01-telephony-core-walking-skeleton.md) | telephony-core | Implemented: originate/bridge/hangup, arriving calls, either-party hang-up ends a two-party call |
| [LLD-02](lld/LLD-02-identity.md) | identity | Implemented; list/revoke pending in `console-apis-phase-a` |
| [LLD-03](lld/LLD-03-pbx-core.md) | pbx-core | Phase A implemented (all four §12 slices); Phase B (IVR, queues) not started |
| [LLD-08](lld/LLD-08-licensing.md) | licensing | Implemented except the licence RPCs (pending), fingerprinting and the CLI |
| [LLD-11](lld/LLD-11-entitlement.md) | entitlement (module gating) | Designed; after Phase A |
| LLD-04, 05, 06, 09, 10 | compliance, webhooks, dialer, reporting, AI | Not started (Phases B/C, R2) |

## Done — archived changes

| Date | Change | Tasks | Proof |
|---|---|---|---|
| 2026-09-07 | `telephony-core-originate-bridge-hangup` | 31/31 | U I E |
| 2026-09-07 | `identity-auth-rbac` | 27/27 | U I |
| 2026-09-07 | `identity-api` | 22/22 | U I W |
| 2026-09-08 | `auth-cutover-connectrpc` | 15/15 | U I W E |
| 2026-09-08 | `pbx-extensions-projection` | 41/41 | U I W E |
| 2026-09-08 | `pbx-e2e-projection` | 12/12 | E |
| 2026-10-11 | `licensing-capacity-grace` | 41/41 | U I E |
| 2026-10-11 | `license-provisioning-enforcement` | 9/9 | U I W E |
| 2026-10-11 | `pbx-trunks-routes` | 17/17 | U I W E |
| 2026-10-11 | `pbx-call-placement` | 25/25 | U I W E A |
| 2026-10-11 | `pbx-inbound-and-emergency` | 24/24 | U I W E A |

## Active changes

| Change | State | Tasks |
|---|---|---|
| `console-apis-phase-a` | Proposed, not applied | 0/13 |

## Open gaps

Carried from each change's `coverage.md` (G-numbers are per change). Close
one by linking the change that fixes it.

| Gap | Where it came from | Severity |
|---|---|---|
| Two trunks on one carrier address are indistinguishable for inbound calls | inbound G3 | **High**: wrong-tenant attribution once two tenants share a carrier |
| Before activation (Setup), tenant and extension caps read 0 = unlimited | license-provisioning-enforcement | Medium: provisioning is unbounded until a licence is applied |
| `ListCalls`/`GetCall` have no role check (any user of the tenant can read its calls) | console-apis-phase-a D5 | Medium: arrives with LLD-09 |
| No `Retry-After` on the 503 for capacity refusals (Asterisk cannot add it) | inbound G1 | Low |
| Emergency catalogue is CM and US only (owner decision); other countries are tenant-entered | inbound G2 | Accepted |
| Toll-fraud outbound classes not enforced | call-placement G4 | Medium: tenant country now exists to build it on |
| No `Degraded` trunk health from latency; unanswered calls not attributed (no SIP cause) | call-placement G1, G2 | Low |
| Both legs of an API-placed call ring at once | call-placement G6 | Low |
| ERROR log "participant-left handler failed: call not found" after every hang-up | call-placement G3, inbound G6 | Low: noise |
| Static fixtures 1000/1001 cannot dial (no tenant) | inbound G5 | Accepted |
| e2e/UAT need the host firewall to admit the Docker bridge on UDP 5070–5071 and 18000–18039 | call-placement G7 | Environment |
| No live test of a full trunk being passed over, or of a trunk failing to originate | inbound G4, call-placement G8 | Low: proven at integration |

## How to see it working

```bash
make dev-licence      # once: licence the dev app (nothing committed)
make rig-restore      # between test tiers
make uat-auto         # walking skeleton, real phones
make uat-placement    # PlaceCall over a trunk, real phones
make uat-inbound      # carrier → DID → phone, and phone → trunk, real phones
```
