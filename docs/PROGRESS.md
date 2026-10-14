# Progress

Where AtsaPBX stands against its own delivery plan. This file is a
**tracker**, not a design document. The plan is
[DECISIONS.md D-46](DECISIONS.md) (three phases, each ending with a console
slice), the flows are [HLD-18](hld/18-phase-a-user-flows.md), and the
behaviour lives in `openspec/specs/`. When this file and those disagree,
they win and this file is wrong.

**Last updated:** 2026-10-13, after `phase-a-hardening` was completed:
phone-dialled calls recorded and presented truthfully, a taken-over Stasis
application noticed, no engine endpoint in `GetCall`, nothing provisioned
before a licence, and no false error after every hang-up.

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
| **A** | install it, licence it, make and receive real calls, configured entirely in the UI | **Done**: 14 changes archived, every console slice A endpoint exists | **A1–A5 screens done** (`console-foundation`, `console-admin-screens`) | In progress |
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
| A1 First run: install to first login, licence | Licence RPCs ✅ (`console-apis-phase-a`). Console session, sign-in by email, tenant choice, throttling, password change and recovery ✅ (`identity-accounts-and-sessions`) | U I W E | ✅ Sign in, Licence status and activation, Account (change or reset password) (`console-foundation`; browser, rig, UAT) |
| A2 Add a user and give them a role | Add by email (one person, many tenants), invite, disable, grant, list, revoke, administrator reset ✅ | U I W E | ✅ Users, roles, permissions (`console-admin-screens`; browser, rig, UAT) |
| A3 Add an extension a phone can register to | ✅ | U I W E A | ✅ Extensions with registration status, credentials shown once (`console-admin-screens`; UAT: a real phone registers with them) |
| A4 Connect a SIP trunk and route outbound calls | ✅ (plus DIDs and emergency profile) | U I W E A | ✅ Trunks and routes with health, inbound numbers, emergency (`console-admin-screens`; browser, rig, UAT) |
| A5 The first real call (out, in, desk phone, emergency) | Calls ✅. Call history list ✅ (`console-apis-phase-a`) | U I W E A | ✅ Call history, read-only, filters in the address (`console-admin-screens`; UAT: a real call appears) |

### Phase A — next steps, in order

1. **Console design input** (owner): the console design document, from
   the shared design input form. The console changes start from it.
2. ~~`console-foundation`~~ **archived** 2026-10-14: React + TypeScript ([D-36](DECISIONS.md)),
   TypeScript clients generated from the same protos, session handling
   (in-memory token, silent refresh, tenant picker), layout, Sign in and
   Licence screens; browser e2e (Playwright) walking A1.
   - ~~`licensing-status-conditions`~~ **archived** (D-70): `GetLicenseStatus`
     reports the PRD condition, tier and capacity level, so the licence
     screen (task 2.3) is unblocked.
3. ~~`console-admin-screens`~~ **archived** 2026-10-13: Users, Extensions,
   Trunks & routes (with inbound numbers and emergency), Call history;
   browser tests on the real API and UAT with real phones walking A2–A5.
4. **`phase-a-hardening`** done 2026-10-13 (ready to archive):
   - a call dialled on a phone is recorded with its real direction (INTERNAL or OUTBOUND) and from the dialling extension's number, and the ringing phone is shown who is calling;
   - a Stasis application taken over by another process is logged and fails readiness (D-73);
   - `GetCall` returns no engine endpoint;
   - nothing is provisioned before a licence is applied (D-72);
   - no false ERROR after every hang-up.

   It is proven by unit, integration, e2e (including a live takeover on the rig) and real-phone UAT, which now also covers a phone-to-phone call.

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
   extension and person. (`GetCall` already exposes no engine endpoint:
   `phase-a-hardening`.)
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
| 2026-10-13 | `pbx-sip-registrar-settings` | 5/5 | U I W E (A as regression) |
| 2026-10-13 | `console-admin-screens` | 20/20 | Console: component, browser (mocked), browser on the real API (×3, no residue), UAT with real phones |
| 2026-10-14 | `console-foundation` | 11/11 | Console: component, browser (mocked), browser on the real API, A1 UAT |
| 2026-10-14 | `identity-refresh-grace` | 7/7 | U I W E (D-62 amendment: 30-second grace for the previous refresh secret) |

## Active changes

| Change | State | Tasks |
|---|---|---|
| `quality-and-supply-chain-gates` | Go 1.26.9, expanded linters (0 issues), coverage floors on every tier, OSV-Scanner, gitleaks, Trivy and SBOM, Jenkins stages; console linter and Asterisk image scan pending | 11/13 |

## Open gaps

Carried from each change's `coverage.md` (G-numbers are per change). Close
one by linking the change that fixes it. **When:** *hardening* = in
`phase-a-hardening`; *pilot* = before the first pilot; *B* = Phase B by
design; *accepted* = no fix planned.

| Gap | Where it came from | Severity |
|---|---|---|
| The Asterisk image is single-stage and ships its build toolchain: Trivy finds 20 fixable HIGH (Python 3.11) and 440 HIGH/CRITICAL that Debian has not fixed yet, so the image gate cannot pass | quality-and-supply-chain-gates 5.3 | **High** — pilot: multi-stage runtime image; owner decision on `--ignore-unfixed` |
| Two trunks on one carrier address are indistinguishable for inbound calls | inbound G3 | **High** — pilot: wrong-tenant attribution once two tenants share a carrier |
| ~~Before activation (Setup), tenant and extension caps read 0 = unlimited~~ | license-provisioning-enforcement | Closed: `phase-a-hardening` task 3.1 (D-72) |
| `ListCalls`/`GetCall` have no role check (any user of the tenant can read its calls) | console-apis-phase-a D5, G2 | Medium — B (LLD-09): arrives with LLD-09 |
| ~~`GetCall` returns `Participant.endpoint_uri` (`PJSIP/e_…`), an engine identifier~~ | console-apis-phase-a G3 | Closed: `phase-a-hardening` task 2.1 (deprecated, never filled); person-aware participant fields come with `pbx-extension-assignment` (D-65) |
| No external sign-in yet; providers can be configured only in the database | D-63, D-67, identity G1 | Planned: `identity-oidc-google` (Gmail and Workspace) before a pilot; company-domain SSO later |
| No RPC for a person to change their own email or display name | identity G2 | Low: with the console's account screen |
| The per-address login throttle is shared by everyone behind one address | identity G4 | Accepted (D-64): runs reset after 15 quiet minutes |
| No person ↔ extension link: call history shows numbers and extension names, not people; no per-agent view | D-65 | Phase B by design: `pbx-extension-assignment` |
| No formal CDR (immutable, per-second usage, export); calls and participants are recorded and listable | HLD 04 §5, LLD-09 | Phase B: reporting |
| No MFA for password users | D-63, D-66 | Planned: `identity-mfa` (TOTP and passkeys; required for administrators) in **Phase B, before GA**; Google sign-ins get Google's 2-step verification |
| No SCIM provisioning (leavers removed in the customer's IdP keep their membership) | D-66 | Planned: `identity-scim` after MFA |
| Call history has no extension column: `CallSummary` carries none (a phone-dialled call's source is now its extension number) | console-admin-screens G3 | Low — B: with `pbx-extension-assignment`, which records the call's extension |
| No `ListRoles` RPC: the console's role catalogue mirrors identity's table (a contract test fails on drift) | console-admin-screens G1 | Accepted until custom roles |
| No `Retry-After` on the 503 for capacity refusals (Asterisk cannot add it) | inbound G1 | Low |
| Emergency catalogue is CM and US only (owner decision); other countries are tenant-entered | inbound G2 | Accepted |
| Toll-fraud outbound classes not enforced | call-placement G4 | Medium — pilot: tenant country now exists to build it on |
| No `Degraded` trunk health from latency; unanswered calls not attributed (no SIP cause) | call-placement G1, G2 | Low |
| Both legs of an API-placed call ring at once | call-placement G6 | Low |
| ~~ERROR log "participant-left handler failed: call not found" after every hang-up~~ | call-placement G3, inbound G6 | Closed: `phase-a-hardening` task 5.1 |
| A trunk host that does not resolve (a typo such as `locslhodt`) makes Asterisk block about 5 s on DNS while identifying SIP requests, stalling call setup for **every** tenant; the API accepts any host | phase-a-hardening (found on the dev rig, 2026-10-13) | **High** — pilot: refuse or flag an unresolvable host at creation, and keep DNS off the identify path |
| Outbound calls present no caller ID to the carrier: a tenant has no presentation number yet | phase-a-hardening (design non-goal) | Medium — pilot |
| A Stasis application taken over by another process is not taken back: the instance stays not-ready until restarted | phase-a-hardening D5 | Accepted (D-73) |
| The Go integration tier defaults to the rig's live database and migrates it down; run it with `DATABASE_URL`/`ENGINE_DATABASE_URL` on `atsapbx_test` to keep the rig's data | phase-a-hardening (found while testing) | Low — tooling: make the scratch database the default |
| Static fixtures 1000/1001 cannot dial (no tenant) | inbound G5 | Accepted |
| e2e/UAT need the host firewall to admit the Docker bridge on UDP 5070–5071 and 18000–18039 | call-placement G7 | Environment |
| No live test of a full trunk being passed over, or of a trunk failing to originate | inbound G4, call-placement G8 | Low: proven at integration |

## How to see it working

```bash
make tilt-up          # the stack, live-updating; console on http://localhost:5173
make dev-user         # a console login: dev@example.test, two Tenants
make dev-licence      # once: licence the dev app (nothing committed)
make rig-restore      # between test tiers
make uat-auto         # walking skeleton, real phones
make uat-placement    # PlaceCall over a trunk, real phones
make uat-inbound      # carrier → DID → phone, and phone → trunk, real phones
```
