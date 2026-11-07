# Progress

Where AtsaPBX stands against its own delivery plan. This file is a
**tracker**, not a design document. The plan is
[DECISIONS.md D-46](DECISIONS.md) (three phases, each ending with a console
slice), the flows are [HLD-18](hld/18-phase-a-user-flows.md), and the
behaviour lives in `openspec/specs/`. When this file and those disagree,
they win and this file is wrong.

**Continuing next week? Read [HANDOFF.md](HANDOFF.md) first.**

**Last updated:** 2026-11-07, after an architecture review of everything
delivered since 2026-11-02 (11 changes, D-79 to D-90). The tracker,
handoff and decision log were reconciled with the code. Two designs were
judged below the industry standard and flagged for refactor. The IVR model
(D-91) is the next change, `pbx-ivr-flow-graph`. Reporting's direct read
of telephony tables (D-92) is a temporary deviation that `reporting-cdr-ledger`
removes before the R1.0 gate. The full list is in
*Architecture review 2026-11-07* below; the per-change history is in the
*Done* table.

**Next work:** `pbx-ivr-flow-graph` (proposed, D-91), then `pbx-queues`
(ACD, agents, hot-desk), then voicemail and recording, then
`reporting-cdr-ledger` (D-92) and the remaining LLD-09 quality and
retention work. MFA and company-domain SSO remain before GA. Hold,
transfer, park and conference (FBR-R1-01) are not scheduled yet and must
be before R1.0.

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
| **A** | install it, licence it, make and receive real calls, configured entirely in the UI | **Done**: every console slice A endpoint exists, and every before-pilot item is closed | **A1–A5 screens done** (`console-foundation`, `console-admin-screens`), plus click-to-call, the live call session, browser calling with TURN, T-6 health with alerts, outbound classes, Google sign-in, the fraud guard, a hardened scanned Asterisk image | **Done** 2026-11-04 (pilot-ready; MFA before GA) |
| **B** | run it as a business phone system: users ↔ extensions, IVR, queues and agents, CDR and reporting, voicemail, recording | **In progress**: ownership (D-87), call-record reporting read model (D-88), IVR routing and single-level menu (D-89, D-90). Refactor flagged: IVR model (D-91), CDR ledger (D-92) | Call flows, Reports, "Assigned to" delivered; IVR flow graph, then queues and recording | In progress |
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
| A5 The first real call (out, in, desk phone, emergency) | Calls ✅. Call history list ✅ (`console-apis-phase-a`). Click-to-call and the live session ✅ (`console-click-to-call`, `console-call-session`; D-78/D-79) | U I W E A | ✅ Call history with filters, New call dialer, and a persistent live call bar with Cancel/Hang up (`console-admin-screens`, `console-click-to-call`, `console-call-session`; UAT: a real call appears, is observed, and is ended) |

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
4. ~~`phase-a-hardening`~~ **archived** 2026-10-14:
   - a call dialled on a phone is recorded with its real direction (INTERNAL or OUTBOUND) and from the dialling extension's number, and the ringing phone is shown who is calling;
   - a Stasis application taken over by another process is logged and fails readiness (D-73);
   - `GetCall` returns no engine endpoint;
   - nothing is provisioned before a licence is applied (D-72);
   - no false ERROR after every hang-up.

   It is proven by unit, integration, e2e (including a live takeover on the rig) and real-phone UAT, which now also covers a phone-to-phone call.

### Before a pilot

5. ~~Unresolvable trunk host stalls everyone~~ **closed** 2026-11-01
   (`pbx-trunk-host-resolution`, D-75).
6. ~~Carriers with published signalling ranges, and non-carrier hosts~~
   **done** 2026-11-01 (`pbx-carrier-addresses`, D-76).
7. ~~Shared carrier address~~ **done** 2026-11-01
   (`pbx-carrier-ingress`, D-77).

**Owner decision, 2026-11-01:** the following close **in this phase**,
in this order (details and open questions: [HANDOFF.md](HANDOFF.md) §1
and §5):

- ~~**Click-to-call**~~ **done 2026-11-02** (`console-click-to-call`):
  the console rings the administrator's extension first, dials the
  destination only on the answer, and offers Hang up; 30 s caller timeout.
- ~~**Call session UX**~~ **done 2026-11-02** (`console-call-session`,
  D-79): a persistent live bar, New call dialer, live history and
  `GetCall` outcome fields.
- ~~**Trunk health per HLD T-6 and the BR-08 alert**~~ **done 2026-11-03**
  (`pbx-trunk-health-thresholds`, D-80): 10 s checks, 3 failures or a
  20 % call-setup window, 5-minute recovery, and one email per failure
  transition.
- ~~**Browser calling**~~ **done 2026-11-03** (`console-browser-phone`,
  D-81): SIP.js over WS on the rig / WSS in production, a WEBRTC
  extension, browser dial/answer/mute/hang up.
8. ~~**Toll-fraud protection**~~ **done 2026-11-04**
   (`pbx-outbound-classes`, D-82): INTERNAL/NATIONAL/INTERNATIONAL, the
   NATIONAL default, and refusal before the engine; emergency exempt.
9. ~~**`identity-oidc-google`**~~ **done 2026-11-04** (D-83): one
   installation-wide Google client, linked-only sign-in, invitation and
   account-screen linking.

MFA (`identity-mfa`) is Phase B, before GA (D-66 as amended).

### Phase A scope boundary (D-65)

Phase A's console is a working **administration** console: sign in,
licence, users and roles, extensions, trunks and routes, and real calls
with history (HLD 18 §7.1). It does **not** link people to extensions.
Users have no extension and extensions have no owner, and call history
shows extension numbers and names, not people. Nothing in A1–A5 needs
that link.

### Phase B — order (in progress)

1. ~~`pbx-extension-ownership`~~ **done 2026-11-04** (D-87).
2. ~~`pbx-ivr-routing`~~ **done 2026-11-04** (D-89);
   ~~`pbx-ivr-menu`~~ **done 2026-11-05** (D-90): prompt/DTMF, fallbacks.
3. **`pbx-ivr-flow-graph`** (proposed 2026-11-07, D-91): typed
   destinations, multi-level menus as a node graph, weekly schedules in a
   time zone, override, re-prompt, direct dial. It must come before queues,
   because a queue is a new destination kind.
4. `pbx-queues`: ACD queues, agents and state, hot-desking (US-03.4).
5. LLD-09 `reporting`: ~~read model, attribution, agent-scoped history,
   audited export~~ **done 2026-11-04** (D-88); `reporting-cdr-ledger`
   (event-fed, append-only CDR, D-92) and quality remain.
6. `pbx-voicemail`, then recording and quality.
7. Call control (FBR-R1-01): hold, transfer, park, conference.
   `HoldParticipant`/`TransferParticipant` return `ErrNotImplemented`
   today. Not yet ordered by the owner.

## Bounded contexts (LLD status)

| LLD | Context | Status |
|---|---|---|
| [LLD-01](lld/LLD-01-telephony-core-walking-skeleton.md) | telephony-core | Implemented: originate/bridge/hangup, arriving calls, either-party hang-up ends a two-party call |
| [LLD-02](lld/LLD-02-identity.md) | identity | Revision 2 implemented, plus Google sign-in over generic OIDC (D-83). Designed (revision 4): `identity-mfa` (before GA), company-domain OIDC, `identity-scim` |
| [LLD-03](lld/LLD-03-pbx-core.md) | pbx-core | Phase A implemented, plus trunk health, carrier ingress, outbound classes, fraud guard and TURN. Phase B: ownership, IVR routing and single-level menu implemented; IVR flow graph (§3.2.1, D-91), queues and voicemail designed |
| [LLD-08](lld/LLD-08-licensing.md) | licensing | Implemented except the licence RPCs (pending), fingerprinting and the CLI |
| [LLD-11](lld/LLD-11-entitlement.md) | entitlement (module gating) | Designed; after Phase A |
| [LLD-09](lld/LLD-09-reporting.md) | reporting | First slice implemented (D-88: read model, attribution, audited export); CDR ledger (D-92), quality and retention designed |
| LLD-04, 05, 06, 10 | compliance, webhooks, dialer, AI | Designed 2026-11-04; not implemented (Phases B/C, R2). No AI code exists yet |

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
| 2026-10-14 | `phase-a-hardening` | 10/10 | U I W E A (live takeover on the rig; phone-to-phone UAT) |
| 2026-10-14 | `pbx-trunk-health-probing` | 7/7 | U I W E, console browser on the real API, A3–A5 UAT against a real carrier (D-74) |
| 2026-11-01 | `pbx-trunk-host-resolution` | 9/9 | U I E, UAT as regression; a container carrier recognised by DNS name (D-75) |
| 2026-11-01 | `pbx-carrier-addresses` | 9/9 | U I E A; a container carrier recognised from its published range; console field, browser and rig; phone UATs as regression (D-76) |
| 2026-11-01 | `pbx-carrier-ingress` | 7/7 | U I E A; spike (one ingress, `remote_addr` through ARI, 500 ranges); two tenants on one container carrier each receive their own numbers (D-77) |
| 2026-11-02 | `console-click-to-call` | 5/5 | U I E A and the console's four tiers; caller-first placement and 30 s timeout (D-78); live e2e + all three phone UATs + `make console-check` |
| 2026-11-02 | `console-call-session` | 32/32 | U I E A and the console's four tiers; live shell call bar, dialer, live history and `GetCall` outcome fields (D-79); full Go e2e, three phone UATs, `make console-check`, ratcheted floors |
| 2026-11-03 | `pbx-trunk-health-thresholds` | 12/12 | U I E A; T-6 thresholds and recovery, `trunk_health_samples`, BR-08 email alert in Mailpit (D-80); full pbx integration, full Go e2e, console-check, three phone UATs |
| 2026-11-03 | `console-browser-phone` | 16/16 | U I E A and the console tiers; WEBRTC projection and migration 0018, SIP.js browser phone, two-browser rig call, full Go e2e, console-check, three phone UATs (D-81) |
| 2026-11-04 | `pbx-ivr-routing` | 5/5 | U I E A; DID flows, immutable publish/rollback, UTC hours and holidays, console Call flows (D-89); full integration, e2e, race, console-check and phone UATs |
| 2026-11-04 | `pbx-reporting` | 5/5 | U I E A; participant attribution, terminated-call CDRs, agent-scoped reads, audited CSV export and Reports page (D-88); full integration, e2e and race, console-check and phone UATs |
| 2026-11-04 | `pbx-extension-ownership` | 5/5 | U I E A; audited extension ownership, active-member validation, "assigned to" in the console, assign/unassign API (D-87); full integration, full e2e, race, console-check and phone UATs |
| 2026-11-04 | `quality-and-supply-chain-gates` | 13/13 | U I E A; multi-stage Asterisk runtime (~0.8 GB), fixable-only image gate with retained SARIF, SBOMs, full quality gate (D-86) |
| 2026-11-04 | `pbx-fraud-guard` | 6/6 | U I E A; rated spend caps, blocked prefixes, anomaly suspension and resume (D-85); full integration, full e2e, race, console-check and phone UATs, docs and SCA |
| 2026-11-04 | `pbx-turn-media` | 4/4 | U I E A; STUN/TURN config through the registrar and browser, dev coturn, two-browser rig call (D-84) |
| 2026-11-04 | `identity-oidc-google` | 10/10 | U I E C; installation-wide Google OIDC, linked-only sign-in, invitation and account linking, single-use state/PKCE/nonce (D-83); full integration, full Go e2e, console-check and phone UATs, docs and SCA. Live Google smoke stays in UAT |
| 2026-11-04 | `pbx-outbound-classes` | 10/10 | U I E A; per-extension INTERNAL/NATIONAL/INTERNATIONAL, NATIONAL default, `+` classification, refusal before the engine, emergency exempt (D-82); full pbx integration, full Go e2e, console-check and UAT, docs and SCA |
| 2026-11-05 | `pbx-ivr-menu` | 5/5 | U I E A; ARI prompt/DTMF execution, timeout and no-input/invalid fallbacks, console menu editor (D-90); full integration, e2e, race, console-check; live handset UAT documented |

## Active changes

| Change | State | Tasks |
|---|---|---|
| `pbx-ivr-flow-graph` | Proposed 2026-11-07 (D-91); not started | 0/— |

## Open gaps

Carried from each change's `coverage.md` (G-numbers are per change). Close
one by linking the change that fixes it. **When:** *hardening* = in
`phase-a-hardening`; *pilot* = before the first pilot; *B* = Phase B by
design; *accepted* = no fix planned.

| Gap | Where it came from | Severity |
|---|---|---|
| Live Google sign-in is not automated (no client secret/account in CI) | identity-oidc-google G1 | **Medium** — run `docs/uat/google-sign-in.md` before a release |
| ~~AC-08.2 anomaly detection and spend caps~~ | pbx-outbound-classes G1 | Closed: `pbx-fraud-guard` (D-85) adds rated caps, blocked prefixes and anomaly suspension |
| The fraud anomaly rule is blunt: volume of international-form attempts outside 08:00–18:00 UTC, default 20 in 10 minutes | pbx-fraud-guard G1 | Accepted: a false positive needs an explicit resume; thresholds are per tenant |
| Spend is rated from route cost and answered duration; calls longer than six hours are not observed | pbx-fraud-guard G2 | Accepted in Phase A; LLD-04 rating replaces it |
| ~~No TURN relay for browser media~~ | console-browser-phone G1 | Closed: `pbx-turn-media` configures STUN/TURN and dev coturn (D-84); the browser passes it to SIP.js |
| TURN credentials are static in Phase A and readable by signed-in tenants | pbx-turn-media G1 | Accepted: required for browser allocation; rotate from the secret store |
| The browser phone needs the SIP password per session; no user↔extension ownership | console-browser-phone G2 | Accepted: Phase B `pbx-extension-assignment` |
| The engine image has no `codec_opus`; browser endpoints use PCMU/PCMA | console-browser-phone G4 | Accepted; add codec_opus later if desired |
| Plain WS on the dev rig for browser signalling | console-browser-phone G5 | Accepted: localhost only; production must use WSS |
| ~~The Asterisk image is single-stage and cannot pass the Trivy gate~~ | quality-and-supply-chain-gates 5.3 | Closed: multi-stage runtime and fixable-only gate (D-86); unfixed Debian CVEs stay in `quality-reports/trivy-core.sarif` |
| ~~Per-person call attribution waits for LLD-09 reporting~~ | pbx-extension-ownership / D-87 | Closed: `pbx-reporting` fills participant extension/owner and exposes agent-scoped records (D-88) |
| Recording/quality/retention remain later LLD-09 work | pbx-reporting G1 | Planned with recording governance |
| IVR destinations are extensions only; one menu level; one UTC window for every day (weekends read as open); no re-prompt, direct dial or manual override; deleting an extension does not check flows | review 2026-11-07 | **High — refactor flagged (D-91)**: `pbx-ivr-flow-graph`, before queues |
| Hold and transfer return `ErrNotImplemented`; park, conference and presence are not built (FBR-R1-01) | review 2026-11-07 | **High — R1.0** (not yet ordered) |
| Reports cap exports at 10,000 rows and do not include quality scores | pbx-reporting G2 | Accepted for the first reporting slice |
| ~~Trunk health did not follow HLD T-6~~ | Found 2026-11-01 | Closed: `pbx-trunk-health-thresholds` implements T-6 as written (D-80) |
| ~~BR-08: a failing carrier was not alerted~~ | BRD BR-08 | Closed: `pbx-trunk-health-thresholds` emails tenant administrators and `ATSAPBX_CARRIER_ALERT_EMAILS` on the failure transition (D-80) |
| The BR-08 alert is email-only; webhooks are LLD-05 | pbx-trunk-health-thresholds G1 | Accepted |
| ~~Two trunks on one carrier address (or overlapping inbound ranges, D-76) are indistinguishable for inbound calls~~ | inbound G3 | Closed: `pbx-carrier-ingress` (D-77): one ingress, attributed by source address and number; a number configured in two tenants that share a carrier is refused and logged |
| ~~Before activation (Setup), tenant and extension caps read 0 = unlimited~~ | license-provisioning-enforcement | Closed: `phase-a-hardening` task 3.1 (D-72) |
| `ListCalls`/`GetCall` have no role check (any user of the tenant can read its calls). The first LLD-09 slice (D-88) scoped Reports but not these RPCs | console-apis-phase-a D5, G2 | **Medium — still open**: scope them like Reports (agent sees own calls) |
| ~~`GetCall` returns `Participant.endpoint_uri` (`PJSIP/e_…`), an engine identifier~~ | console-apis-phase-a G3 | Closed: `phase-a-hardening` task 2.1 (deprecated, never filled); person-aware participant fields come with `pbx-extension-assignment` (D-65) |
| Google is the only external sign-in; company-domain SSO and provider administration are not built | D-63, D-67 | Planned: `identity-oidc` on the same OIDC adapter |
| No RPC for a person to change their own email or display name | identity G2 | Low: with the console's account screen |
| The per-address login throttle is shared by everyone behind one address | identity G4 | Accepted (D-64): runs reset after 15 quiet minutes |
| ~~No person ↔ extension link; no per-agent view~~ | D-65 | Closed: `pbx-extension-ownership` (D-87) and agent-scoped Reports (D-88) |
| CDRs are a read model over telephony's mutable tables, not an event-fed append-only ledger (BR-07, HLD 04 §5) | D-88, review 2026-11-07 | **Refactor flagged (D-92)**: `reporting-cdr-ledger` before the R1.0 gate |
| No MFA for password users | D-63, D-66 | Planned: `identity-mfa` (TOTP and passkeys; required for administrators) in **Phase B, before GA**; Google sign-ins get Google's 2-step verification |
| No SCIM provisioning (leavers removed in the customer's IdP keep their membership) | D-66 | Planned: `identity-scim` after MFA |
| Call history has no extension column: `CallSummary` carries none (a phone-dialled call's source is now its extension number) | console-admin-screens G3 | Low — B: with `pbx-extension-assignment`, which records the call's extension |
| No `ListRoles` RPC: the console's role catalogue mirrors identity's table (a contract test fails on drift) | console-admin-screens G1 | Accepted until custom roles |
| No `Retry-After` on the 503 for capacity refusals (Asterisk cannot add it) | inbound G1 | Low |
| Emergency catalogue is CM and US only (owner decision); other countries are tenant-entered | inbound G2 | Accepted |
| ~~Toll-fraud outbound classes not enforced~~ | call-placement G4 | Closed: `pbx-outbound-classes` (D-82) and `pbx-fraud-guard` (D-85) |
| ~~Trunk health assumed `Healthy` until a call said otherwise~~ | owner, 2026-10-13 (a trunk to `locslhodt` read Healthy) | Closed: `pbx-trunk-health-probing` (checked by SIP OPTIONS, with reason and time; D-74) |
| Unanswered placed calls are not attributed to the carrier (no SIP cause); a slow OPTIONS response now degrades a trunk, but a slow call answer still needs one | call-placement G1, G2 | Low |
| ~~Both legs of an API-placed call ring at once~~ | call-placement G6 | Closed: `console-click-to-call` rings the caller first and the destination only on the answer (D1) |
| ~~ERROR log "participant-left handler failed: call not found" after every hang-up~~ | call-placement G3, inbound G6 | Closed: `phase-a-hardening` task 5.1 |
| ~~A trunk host that does not resolve (a typo such as `locslhodt`) makes Asterisk block about 5 s on DNS while identifying SIP requests, stalling call setup for **every** tenant~~ | phase-a-hardening (found on the dev rig, 2026-10-13) | Closed: `pbx-trunk-host-resolution` (D-75): the platform resolves carrier names; identify holds addresses only. Unresolvable hosts are accepted and flagged (owner) |
| Outbound calls present no caller ID to the carrier: a tenant has no presentation number yet | phase-a-hardening (design non-goal) | Medium — pilot |
| A Stasis application taken over by another process is not taken back: the instance stays not-ready until restarted | phase-a-hardening D5 | Accepted (D-73) |
| The Go integration tier defaults to the rig's live database and migrates it down; run it with `DATABASE_URL`/`ENGINE_DATABASE_URL` on `atsapbx_test` to keep the rig's data | phase-a-hardening (found while testing) | Low — tooling: make the scratch database the default |
| Static fixtures 1000/1001 cannot dial (no tenant) | inbound G5 | Accepted |
| e2e/UAT need the host firewall to admit the Docker bridge on UDP 5070–5071 and 18000–18039 | call-placement G7 | Environment |
| No live test of a full trunk being passed over, or of a trunk failing to originate | inbound G4, call-placement G8 | Low: proven at integration |

## Architecture review 2026-11-07

All changes delivered from 2026-11-02 to 2026-11-05 (D-79 to D-90) were
reviewed against the BRD, PRD, HLD and current vendor practice (3CX,
VitalPBX, FreePBX, FusionPBX, Vodia, Amazon Connect). The owner reports
that DeepSeek made this batch through the agent tooling; the commits carry
the usual co-author line.

**Aligned: keep as built.**
- Carrier work: one ingress attributed by source and number, trunk health
  T-6 with the BR-08 alert, and published carrier ranges (D-75 to D-77, D-80).
- Calling: caller-first click-to-call, the live call session, and the
  browser phone over SIP.js with STUN/TURN (D-78, D-79, D-81, D-84).
- Toll fraud: outbound classes and the fraud guard (D-82, D-85). Emergency
  stays exempt (BR-09).
- Identity: Google sign-in, linked-only, with PKCE, nonce and one-time
  state (D-83).
- Supply chain: a hardened multi-stage Asterisk image and a fixable-only
  scan gate (D-86).
- Extension ownership with audit (D-87).
- Reporting rules: agent scoping, terminated-only records, audited export
  (D-88).
- IVR mechanics: immutable revisions, rollback, ARI execution, 10 options
  with no-input/invalid fallbacks, holidays (D-89, D-90).

**Flagged for refactor**, in this order:

| # | What | Why | Change | Decision |
|---|---|---|---|---|
| R1 | IVR model: extension-only destinations, single level, a UTC window for every day | Below 3CX/VitalPBX/FreePBX; contradicts D-22 and FBR-R1-06; blocks queues | `pbx-ivr-flow-graph` (proposed) | D-91 |
| R2 | Reporting reads telephony tables; the CDR is not append-only | HLD 04 boundary (D-20/D-31), BR-07 | `reporting-cdr-ledger` | D-92 |
| R3 | `ListCalls`/`GetCall` have no role check | An agent can read every call in the tenant | small change, any time | — |
| R4 | `pbx` keeps growing (IVR, fraud, TURN, health, ownership; `ports.go` 837 lines) | Fraud rules are pure checks that D-21 would place in `compliance` | Revisit when `compliance` (LLD-04) starts | — |

**Console review** (a `code-reviewer` subagent read the console diffs
of all 11 changes; the two High findings were then confirmed in the code):
- **Passes:** tsc and oxlint are clean; only generated API clients are
  used; the OIDC exchange is server-side; the SIP password is in memory
  only.
- **High, a live data-loss bug:** `useFlows` drops the menu fields
  (`console/src/features/flows/adapter.ts:67-70`), so republishing a flow
  from the Call flows dialog silently deletes its IVR menu.
- **High:** errors from creating or publishing a flow render behind the
  open modal (`FlowsPage.tsx:87`), so the user sees nothing.
- **Medium:**
  - mistyped menu digits are dropped silently;
  - the softphone is not cleaned up on sign-out or tenant switch (HLD-19
    §6);
  - the in-call view has no DTMF keypad and there is no ringtone;
  - flow rollback has no confirmation;
  - the holiday form is shown to non-administrators;
  - Outbound protection has no field validation and takes caps in raw
    micros.
- **Low:** six items (labels, raw enums, engine wording, the Google
  callback's `returnTo` and error text).
- **Test gap:** Reports, Call flows, IVR menu, Outbound protection,
  ownership and Google sign-in have component tests only. There is no
  browser e2e and no real-API spec, so under the "every tier green" rule
  those tasks were ticked early.

| # | What | Change | Severity |
|---|---|---|---|
| R5 | Call flows republish wipes the menu; hidden dialog errors | `console-review-fixes` (before `pbx-ivr-flow-graph`) | **High** |
| R6 | Browser e2e / real-API specs for Reports, Call flows, Outbound protection, ownership, Google (stub IdP) | `console-review-fixes` | **High** (proof debt) |
| R7 | The Medium/Low console findings above | `console-review-fixes`; the softphone DTMF/ringtone may need its own change | Medium |

**Spec hygiene:** two main specs created in this batch
(`pbx-core/call-flows`, `reporting/call-records`) were structurally
invalid for OpenSpec. They are repaired; `openspec validate --specs
--strict` now passes 22/22.

**Process drift (not a design issue):** each of the 11 changes landed as
one squashed commit (two commits bundle two changes each), and the
changes have only 4 to 6 coarse tasks. That breaks the "one commit per
task" rule. The work is accepted as is; from `pbx-ivr-flow-graph` on, it
is one commit per task again (HANDOFF §3.1).

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

**In the browser:** sign in at http://localhost:5173 as `dev@example.test`
(`make dev-user` prints the password; mail lands in Mailpit,
http://localhost:8025). Step-by-step human passes with their executed
records live in `docs/uat/`: [walking skeleton](uat/walking-skeleton.md),
[trunk host resolution](uat/trunk-host-resolution.md).
