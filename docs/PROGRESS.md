# Progress

Where AtsaPBX stands against its own delivery plan. This file is a
**tracker**, not a design document. The plan is
[DECISIONS.md D-46](DECISIONS.md) (three phases, each ending with a console
slice), the flows are [HLD-18](hld/18-phase-a-user-flows.md), and the
behaviour lives in `openspec/specs/`. When this file and those disagree,
they win and this file is wrong.

**Last updated:** 2026-10-12, after `licensing-status-conditions` was archived
(D-70) and the `console-foundation` scaffold landed (D-68, D-69).

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
| **A** | install it, licence it, make and receive real calls, configured entirely in the UI | **Done**: 13 changes archived, every console slice A endpoint exists | **Not started** | In progress |
| **B** | run it as a business phone system: users ↔ extensions, IVR, queues and agents, CDR and reporting, voicemail, recording | Designed: D-65, LLD-03 §3.1 | — | Not started |
| **C** | run an outbound operation (compliance first, then the power dialler) | — | — | Not started |

**Proof tiers:** U unit · I integration (real Postgres) · W wire · E e2e
(running licensed app, live Asterisk) · A UAT (real baresip phones, RTP
measured both ways).

## Phase A — flows (HLD-18)

Phase A is done when a partner walks A1 → A5 **in the console**, without a
configuration file, and without being told what Asterisk is.

| Flow | Backend | Proven | Console screen |
|---|---|---|---|
| A1 First run: install to first login, licence | Licence RPCs ✅ (`console-apis-phase-a`). Console session, sign-in by email, tenant choice, throttling, password change and recovery ✅ (`identity-accounts-and-sessions`) | U I W E | ☐ Sign in · ☐ Licence status and activation · ☐ Account (change or reset password) |
| A2 Add a user and give them a role | Add by email (one person, many tenants), invite, disable, grant, list, revoke, administrator reset ✅ | U I W E | ☐ Users, roles, permissions |
| A3 Add an extension a phone can register to | ✅ | U I W E A | ☐ Extensions with registration status |
| A4 Connect a SIP trunk and route outbound calls | ✅ (plus DIDs and emergency profile) | U I W E A | ☐ Trunks and routes with health |
| A5 The first real call (out, in, desk phone, emergency) | Calls ✅. Call history list ✅ (`console-apis-phase-a`) | U I W E A | ☐ Call history (read-only) |

### Phase A — next steps, in order

1. **Console design input** (owner): the console design document, from
   the shared design input form. The console changes start from it.
2. **`console-foundation`** (scaffold done, 0.1): React + TypeScript ([D-36](DECISIONS.md)),
   TypeScript clients generated from the same protos, session handling
   (in-memory token, silent refresh, tenant picker), layout, Sign in and
   Licence screens; browser e2e (Playwright) walking A1.
   - ~~`licensing-status-conditions`~~ **archived** (D-70): `GetLicenseStatus`
     reports the PRD condition, tier and capacity level, so the licence
     screen (task 2.3) is unblocked.
3. **`console-admin-screens`**: Users, Extensions, Trunks and routes, Call
   history; browser e2e walking A2–A5. **Phase A is done when this passes.**
4. **`phase-a-hardening`** (small): `GetCall` stops exposing the engine
   endpoint; no provisioning before a licence is applied; the hang-up log
   noise.

### Before a pilot

5. **Shared carrier address**: two trunks on one carrier IP cannot be told
   apart for inbound calls.
6. **Toll-fraud protection**: enforce outbound destination classes.
7. **`identity-oidc-google`**: Sign in with Google, Gmail and Workspace
   ([D-67](DECISIONS.md)).

MFA (`identity-mfa`) is Phase B, before GA (D-66 as amended).

### Phase A scope boundary (D-65)

Phase A's console is a working **administration** console: sign in,
licence, users and roles, extensions, trunks and routes, and real calls
with history (HLD 18 §7.1). It does **not** link people to extensions.
Users have no extension and extensions have no owner, and call history
shows extension numbers and names, not people. Nothing in A1–A5 needs
that link.

### Phase B — order (designed, not started)

1. `pbx-extension-assignment`: who owns an extension; calls record the
   extension and person; `GetCall` stops exposing engine endpoints.
2. `pbx-ivr-flows`: auto-attendant, business hours, holidays.
3. `pbx-queues`: ACD queues, agents and state, hot-desking (US-03.4).
4. LLD-09 `reporting`: immutable CDR, per-second usage, export,
   per-person and agent-scoped history.
5. `pbx-voicemail`, then recording and quality.

## Bounded contexts (LLD status)

| LLD | Context | Status |
|---|---|---|
| [LLD-01](lld/LLD-01-telephony-core-walking-skeleton.md) | telephony-core | Implemented: originate/bridge/hangup, arriving calls, either-party hang-up ends a two-party call |
| [LLD-02](lld/LLD-02-identity.md) | identity | Revision 2 implemented: users and memberships, sessions, recovery, throttling, SSO-ready model. Next: `identity-oidc`, `identity-mfa` (before GA), `identity-scim` |
| [LLD-03](lld/LLD-03-pbx-core.md) | pbx-core | Phase A implemented (all four §12 slices); Phase B designed (§3.1 extension ownership and agents, D-65; IVR; queues), not started |
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
| 2026-10-11 | `console-apis-phase-a` | 13/13 | U I W E (A as regression) |
| 2026-10-11 | `identity-accounts-and-sessions` | 26/26 | U I W E (A as regression) |
| 2026-10-12 | `licensing-status-conditions` | 7/7 | U W E |

## Active changes

| Change | State | Tasks |
|---|---|---|
| `console-foundation` | Scaffold milestone done (0.1); screens next | 1/11 |

## Open gaps

Carried from each change's `coverage.md` (G-numbers are per change). Close
one by linking the change that fixes it. **When:** *hardening* = in
`phase-a-hardening`; *pilot* = before the first pilot; *B* = Phase B by
design; *accepted* = no fix planned.

| Gap | Where it came from | Severity |
|---|---|---|
| Two trunks on one carrier address are indistinguishable for inbound calls | inbound G3 | **High** — pilot: wrong-tenant attribution once two tenants share a carrier |
| Before activation (Setup), tenant and extension caps read 0 = unlimited | license-provisioning-enforcement | Medium — hardening: provisioning is unbounded until a licence is applied |
| `ListCalls`/`GetCall` have no role check (any user of the tenant can read its calls) | console-apis-phase-a D5, G2 | Medium — B (LLD-09): arrives with LLD-09 |
| `GetCall` returns `Participant.endpoint_uri` (`PJSIP/e_…`), an engine identifier | console-apis-phase-a G3 | Medium — hardening: `GetCall` stops exposing it in `phase-a-hardening`; the person-aware participant fields come with `pbx-extension-assignment` (D-65) |
| No external sign-in yet; providers can be configured only in the database | D-63, D-67, identity G1 | Planned: `identity-oidc-google` (Gmail and Workspace) before a pilot; company-domain SSO later |
| No RPC for a person to change their own email or display name | identity G2 | Low: with the console's account screen |
| The per-address login throttle is shared by everyone behind one address | identity G4 | Accepted (D-64): runs reset after 15 quiet minutes |
| No person ↔ extension link: call history shows numbers and extension names, not people; no per-agent view | D-65 | Phase B by design: `pbx-extension-assignment` |
| No formal CDR (immutable, per-second usage, export); calls and participants are recorded and listable | HLD 04 §5, LLD-09 | Phase B: reporting |
| No MFA for password users | D-63, D-66 | Planned: `identity-mfa` (TOTP and passkeys; required for administrators) in **Phase B, before GA**; Google sign-ins get Google's 2-step verification |
| No SCIM provisioning (leavers removed in the customer's IdP keep their membership) | D-66 | Planned: `identity-scim` after MFA |
| No `Retry-After` on the 503 for capacity refusals (Asterisk cannot add it) | inbound G1 | Low |
| Emergency catalogue is CM and US only (owner decision); other countries are tenant-entered | inbound G2 | Accepted |
| Toll-fraud outbound classes not enforced | call-placement G4 | Medium — pilot: tenant country now exists to build it on |
| No `Degraded` trunk health from latency; unanswered calls not attributed (no SIP cause) | call-placement G1, G2 | Low |
| Both legs of an API-placed call ring at once | call-placement G6 | Low |
| ERROR log "participant-left handler failed: call not found" after every hang-up | call-placement G3, inbound G6 | Low — hardening: noise |
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
