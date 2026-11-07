# Handoff — where AtsaPBX stands and how to continue

**Written:** 2026-11-01; **revised 2026-11-07** after an architecture
review, so that anyone (a person, Claude, DeepSeek or another model) can
pick up exactly here. Read this
first, then [PROGRESS.md](PROGRESS.md). When this file and PROGRESS,
DECISIONS or `openspec/specs/` disagree, **t## 1. The next work, in order (revised 2026-11-07)

Phase A is **done** and pilot-ready (every A1–A5 flow and every
before-pilot item is closed; see PROGRESS). Phase B is in progress.

| # | What | State | How to start |
|---|---|---|---|
| 0 | **`console-review-fixes`**: Call flows republish wipes the IVR menu (data loss), dialog errors hidden behind the modal, then the missing browser/real-API specs and the Medium findings (PROGRESS R5–R7) | Found 2026-11-07; not proposed yet | `/opsx:propose console-review-fixes`; run it with the D-93 subagent loop |
| 1 | **`pbx-ivr-flow-graph`**: typed destinations, multi-level menus as a node graph, weekly schedules in an IANA time zone with a manual override, re-prompt, direct dial, and an extension delete that checks flows | **Proposed** (D-91); planning artifacts in `openspec/changes/pbx-ivr-flow-graph/` | `/opsx:apply pbx-ivr-flow-graph`, one task and one commit at a time |
| 2 | **`pbx-queues`**: ACD queues, agents and state, hot-desk (LLD-03 §3.3) | Designed | Propose after #1; it adds the `queue` destination kind |
| 3 | **`pbx-voicemail`**, then recording governance (LLD-03 §3.4, LLD-09) | Designed | After #2 |
| 4 | **`reporting-cdr-ledger`**: an event-fed, INSERT-only CDR replaces the read over telephony tables | Flagged (D-92) | Before the R1.0 gate |
| 5 | **Call control**: hold, transfer, park, conference (FBR-R1-01); `HoldParticipant` and `TransferParticipant` are stubs | Not ordered by the owner yet | Ask the owner where it goes |
| 6 | **`ListCalls`/`GetCall` role scoping**, as Reports does | Open gap (Medium) | Small change, any time |
| 7 | **MFA and company-domain SSO** (LLD-02 revision 4) | Designed | Before GA |

The full list of what was kept and what was flagged is in
[PROGRESS.md § Architecture review 2026-11-07](PROGRESS.md).

**AI:** the BRD makes AI optional and provider-neutral (VP-5, D-23). No AI
code exists yet; LLD-10 is the design. The R1.0 gate requires call audio
streamed to two providers, so an audio-tap spike (snoop or external
media over ARI) should be scheduled before R1.0. Ask the owner when.

---

 and `--ignore-unfixed`).

---

## 2. What was done this week

### 2026-11-07 — architecture review and documentation reconciliation

**Who did the 2026-11-02 to 2026-11-05 work.** The owner reports that
DeepSeek made it through the agent tooling. Every commit carries the
standard co-author line, so read authorship from this note, not the
trailer.

**What it covered.** The review checked every change from D-79 to D-90
against the BRD, PRD, HLD and vendor practice.
- **Most of it is aligned and kept.** The list is in PROGRESS.
- **Two designs are flagged for refactor.** The IVR model is D-91 and is
  next; reporting's cross-context read is D-92.

**Documents fixed.**
- `DECISIONS.md`: D-81 to D-90 had been inserted in reverse order and
  are now in number order, with content unchanged.
- `PROGRESS.md`: the stale "Active changes" row, the Phase B and LLD
  statuses, and the open gaps that were already closed.
- This file.
- `LLD-03` §3.2.1 (shipped versus the target IVR).
- `HLD 04` §5 (reporting implementation status).
- `PRIOR-ART` (IVR sources, and the CDR claim corrected).
- **OpenSpec repair.** The main specs `pbx-core/call-flows` and
  `reporting/call-records` had no `## Purpose`/`## Requirements` sections,
  so OpenSpec could not parse their requirements and would have refused
  the next archive. Both are repaired, and `openspec validate --specs
  --strict` passes 22/22. **Run it before every archive.**

**Process drift to stop.** From 2026-11-02 each change landed as one
squashed commit, and two commits bundle two changes each. From
`pbx-ivr-flow-graph` on, go back to one commit per task (§3.1).


### 2026-11-05 — IVR menu (D-90)

`pbx-ivr-menu` adds a prompt, up to ten DTMF branches, a timeout and
no-input/invalid destinations to an immutable flow revision. Telephony
answers, plays over ARI, collects a digit and routes; closed/holiday never
enters the menu; publish validation refuses a missing destination or a
prompt-less menu. The console Call flows editor exposes all fields. Unit,
integration, e2e and race tiers pass; a real handset pressing digits is
the documented UAT (`docs/uat/ivr-menu.md`).

### 2026-11-04 — missing Phase B/C LLDs written

The design gaps are closed on paper: `LLD-04` compliance (DNC, local-time
windows, opt-out, abandonment), `LLD-05` webhook delivery, `LLD-06` dialer
(preview/power R1, predictive R2), `LLD-09` full reporting (CDR, usage,
quality, recording metadata/retention/access audit) and `LLD-10` AI
pipeline. `LLD-02` gained MFA/company-SSO revision 4; `LLD-03` gained
IVR-menu, queues/agents/hot-desk and voicemail designs. Vendor research
(3CX, VitalPBX, Vodia, FusionPBX, Asterisk) and the deliberate differences
are in `docs/lld/PRIOR-ART.md`. All are **designs**, not implemented
except the slices already delivered.

### 2026-11-04 — IVR routing (D-89)

`pbx-ivr-routing` attaches a DID to a versioned flow with open/closed
destinations, UTC business hours and a holiday calendar. Inbound routing
checks flows before direct DIDs; a holiday or closed hours rings the
closed destination. Publish appends an immutable revision and rollback
moves the active pointer; both are audited. The console gains Call flows
and a holiday calendar. The DTMF menu/prompt is the next slice.

### 2026-11-04 — call-record reporting (D-88)

`pbx-reporting` adds participant attribution at call creation, a
read-only ReportingService over terminated calls, agent-scoped reads, an
audited CSV export and the console Reports page. The operational Call
history remains separate. Quality scores, recordings and retention are
the remaining LLD-09/recording slices.

### 2026-11-04 — image scan closed and Phase B opened (D-86, D-87)

The Asterisk image is now a multi-stage runtime (~0.8 GB) with no build
toolchain or Python. `make image-scan` passes: the API image is strict,
the Asterisk runtime fails on fixable HIGH/CRITICAL and ignores unfixed
Debian base CVEs, with the full SARIF retained; both SBOMs are produced.
`pbx-extension-ownership` opens Phase B: an extension has at most one
active owner, the console shows "Assigned to" and assigns/unassigns, the
change is audited, and deleting an extension removes the link. The full
integration, e2e on the hardened image, race, console-check and phone
UATs pass. Participant attribution and "my calls" follow with LLD-09.

### 2026-11-04 — TURN and the fraud guard (D-84, D-85)

`pbx-turn-media` adds installation-configured STUN/TURN through
`GetSipRegistrar`, the browser phone's peer connection and a dev coturn
service; the two-browser call still passes. `pbx-fraud-guard` adds rated
daily/monthly spend caps from route cost and answered duration, blocked
prefixes, an anomaly window that suspends outbound and emails the
administrators, and an explicit resume in Trunks & routes → Outbound
protection. Emergency and calls in progress are never affected. The
full integration, e2e, race, console-check and phone UATs pass.

### 2026-11-04 — Google sign-in (D-83)

`identity-oidc-google` archived. One installation-wide Google OIDC
client (config in the deployment secret store) is discoverable before an
email is typed. Sign-in finds a person by `(issuer, subject)` only and
never creates one; an invitation links only when Google's verified email
equals the invited email, and the account screen links after re-entering
the current password. Authorization requests use one-time state, PKCE
and a nonce; consumer mailbox domains stay unclaimable. Proven against a
local OIDC issuer, application and Postgres tests, RPC tests, the
console tiers, full integration, full e2e and SCA. Live Google is a
manual release check (`docs/uat/google-sign-in.md`).

### 2026-11-04 — outbound classes (D-82)

`pbx-outbound-classes` archived. Every extension holds INTERNAL,
NATIONAL or INTERNATIONAL, a new one is NATIONAL, and a trunk-bound
destination with a leading `+` is INTERNATIONAL. Placement refuses a
class the extension does not hold before reserving a channel or touching
the engine; emergency is exempt. The console shows and edits the class.
Proven by domain, RPC, integration and full e2e, console-check and the
phone UAT. AC-08.2 detection remains `compliance` work.

### 2026-11-03 — browser phone (D-81)

`console-browser-phone` archived. A WEBRTC extension is projected with a
WebRTC transport, DTLS, ICE, AVPF and RTCP mux; the console registers it
with SIP.js over WebSocket (WS on the rig, WSS in production) and offers
connect, dial, incoming answer/reject, mute and hang up. The SIP password
is entered per session and never stored. The two-browser rig spec proves
a real call over the rig's WS transport; the full Go e2e, console-check
and the three phone UATs pass. No TURN yet: a symmetric-NAT browser needs
a relay before a production pilot (recorded gap). The engine image has no
codec_opus, so the browser uses PCMU/PCMA.

### 2026-11-03 — trunk health T-6 and the BR-08 alert (D-80)

`pbx-trunk-health-thresholds` archived. Checks run every 10 s; a carrier
is Degraded after 3 consecutive failures or a call-setup failure rate
above 20 % over a rolling 60 s window (min 5); recovery needs 5 continuous
healthy minutes. Configuration failures stay Unhealthy at once. The
observations are in `trunk_health_samples` and `healthy_since`; a failure
transition emails the tenant administrators and
`ATSAPBX_CARRIER_ALERT_EMAILS` once, with no alert on recovery. Migration
0017, the pure decision, the prober and the call window are tested at
unit, integration and e2e (the alert is verified in Mailpit). The Asterisk
image scan was then owner-deferred; it closed on 2026-11-04 (D-86).

### 2026-11-02 — live call session and dialer (D-79)

`console-call-session` archived. A placed call is now a shell-level session:
the bar shows *ringing your extension*, *calling the destination*,
*connected with a duration*, or *ended with the reason*, with **Cancel call**
before the answer and **Hang up** after — one click, no confirmation. A
global **New call** dialer offers a searchable registered-extension picker, a
dial pad and recent numbers. Call history refreshes every second while a call
is in flight, ticks the duration, and gives live rows a direct Hang up.
`GetCall` now carries `started_at`, `answered_at`, `ended_at` and
`termination_reason`. The PRD/HLD/CONSOLE-DESIGN/API docs and D-79 record it;
the phone UATs, full Go e2e and `make console-check` pass.

### 2026-11-02 — click-to-call (D-78)

`console-click-to-call` archived. `InitiateCall` has an ordered mode: the
caller's extension rings first, the destination only on its answer, and a
30 s `ATSAPBX_PLACEMENT_CALLER_TIMEOUT` ends an unanswered caller with
`caller_no_answer` without ringing anyone else. The caller leg's engine
timeout is 5 s longer, so the platform owns the reason. The console's Call
history now has **Place a call** (registered extensions only) and **Hang
up** for administrators; refusals are console words. Proven by unit,
race, integration, live e2e and all three phone UATs, plus
`make console-check`; the docs and the UAT procedure are updated and the
floors ratcheted. Two live defects were fixed: the answer could race the
timer's arming, and the engine timeout preempted the placement timer.

### 2026-11-01 — the week's carrier work

There were 39 commits on `develop`, all pushed. Each change below was specified, implemented one commit per task, tested on every tier, documented, and archived with its specs synced.

| Change | Decision | What it gives |
|---|---|---|
| `pbx-trunk-host-resolution` | D-75 | One tenant's unresolvable trunk host (`locslhodt`) no longer stalls carrier calls for every tenant. The platform resolves names; the engine sees addresses only |
| `pbx-carrier-addresses` | D-76 | A trunk takes up to 20 published carrier signalling ranges. `localhost`, loopback, link-local and multicast hosts are refused. A name resolving only to those reads "Unhealthy · Address not allowed" |
| `pbx-carrier-ingress` | D-77 | Carrier calls enter through one engine endpoint, `carrier-ingress`, and the platform attributes each by transport source address plus dialled number. Two tenants on one carrier each receive their own numbers; an ambiguous number is refused 404 and logged |

**Fixes made along the way:**
- **Test budgets:** console tests had budgets that failed on a busy machine.
- **Dependency advisories:** Go 1.26.10, `x/crypto` 0.56.0, `klauspost/compress` 1.18.7, vitest 4.1.12, undici 7.30.1, and a dated OSV exception, `GO-2026-5932`, until 2026-12-01.
- **The registrar address:** it was refused by the carrier rule and the app would not start.
- **The identify column:** it was `VARCHAR(80)` (now TEXT, migration 0015).
- **An unreachable destination** was left ringing; it is now refused 480.
- **Refused calls held a licence channel**; every refusal now releases it.
- **E2e cleanups orphaned engine rows.**

**Also documented:**
- **Gaps recorded** against the BRD and HLD: T-6 not implemented, and BR-08 not alerted. Both are now #5 and #6.
- **UAT:** the browser procedure, with its executed record, is in [uat/trunk-host-resolution.md](uat/trunk-host-resolution.md).

---

## 3. How the project is run (read before changing anything)

### 3.1 Workflow
- **The product docs** are BRD, then PRD, then HLD (`docs/hld/`), then LLD (`docs/lld/`), then OpenSpec specs (`openspec/specs/`). **Never re-litigate a decision** in [DECISIONS.md](DECISIONS.md) (append-only, D-1 to D-93, in number order); say so explicitly if one must change.
- **Every piece of work is an OpenSpec change:**
  1. `/opsx:propose`: proposal, delta specs, design and tasks.
  2. `/opsx:apply`: one task at a time.
  3. `/opsx:archive`: syncs the delta specs into `openspec/specs/` and moves the change to `openspec/changes/archive/`.
- **Per task** (owner rule, "auto mode", shift-left since D-93; full
  cadence in [TESTING.md §8](TESTING.md)):
  1. `qa-engineer` subagent: write the task's tests and show them
     failing.
  2. `implementer` subagent: make them pass, then run the fast tiers
     (lint, unit, the affected integration package on `atsapbx_test`,
     console vitest).
  3. `code-reviewer` subagent: a read-only review of the diff, which must
     end `VERDICT: approve`.
  4. Main session: re-run the gates, tick the task in `tasks.md` with the
     proof, make **one commit per task** and push to `develop`.
  5. At the end of a slice, `qa-engineer` runs full integration, the
     slice's e2e and the browser specs. At the end of a change it runs
     full e2e, console-check, UAT and quality; `docs-steward` then
     updates the docs and runs `openspec validate --specs --strict`
     before `/opsx:archive`.

  Never batch tasks. Never skip tiers. Ratchet the coverage floors after green.
- **Stop and ask the owner** for any new dependency, security trade-off, or scope change.
- **New decisions** are recorded as the next D-number with alternatives, context and traceability to the BRD and PRD.
- **On archive, update PROGRESS.md** (Done row, gaps, *Last updated*).
- **Commits:** end every commit message with the co-author line the tooling asks for. Work on `develop`; `main` is the PR base.

### 3.2 Tests are proof (owner rule)
A task is done only when its **proof ran green**: unit, integration, e2e and UAT for the backend, and the same four tiers for the console (component, browser e2e, rig integration, UAT). "Code merged" is not proof. Real defects keep turning up in the live tiers (this week: silent ringing, the licence leak, the registrar regression), so never skip them.

---

## 4. Commands

Run from the repo root; `mise` pins the toolchain (`.mise.toml`, `console/mise.toml`).

| Tier | Command | Notes |
|---|---|---|
| Lint | `mise exec -- make lint` | golangci-lint, must be **0 issues** (gocognit ≤ 30, goimports) |
| Go unit | `mise exec -- make test` | Plain `make test` may pick up a Go without `covdata` |
| Go integration | `cd api && DATABASE_URL=postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx_test?sslmode=disable ENGINE_DATABASE_URL=postgres://asterisk_engine:devpassword123@localhost:15432/atsapbx_test?sslmode=disable mise exec -- go test -tags integration -count=1 -p 1 ./...` | **Always on `atsapbx_test`**. Without these variables it migrates the rig's live database down and wipes the owner's data |
| Go e2e | `mise exec -- make rig-restore` then `cd api && mise exec -- go test -tags e2e -count=1 -p 1 ./...` | Against the running rig app. Some tests need UDP 5070/5071 (see §6) |
| Phone UATs | `make uat-auto`, `make uat-placement`, `make uat-inbound` (each after `make rig-restore`) | Real baresip phones, media both ways |
| Console, all | `mise exec -- make console-check` | Component (vitest), browser e2e (Playwright), rig integration, UAT, audit |
| Quality gate | `mise exec -- make quality` | lint, unit coverage floors, console tests, SCA (OSV and govulncheck), gitleaks, npm audit |
| Docs | `mise exec -- make docs` | Link and decision checks; run before every docs commit |
| Floors | `mise exec -- make coverage-ratchet` | Raises `quality/coverage-floors.json`. **Never lower a floor**; add real tests instead |

---

## 5. Browser calling and the carrier alert — decisions taken (D-80, D-81, D-84)

- **Library:** SIP.js 0.21.2 (MIT; D-35/68 allowed).
- **Transport:** WS on the dev rig via Asterisk's HTTP server, WSS in production through the reverse proxy or a mounted `transport-wss` certificate; `SIP_WEBRTC_URL` selects it.
- **Who may use it:** an administrator chooses a WEBRTC extension and enters its one-time SIP password; ownership waits for Phase B (D-65).
- **NAT:** STUN/TURN is installation-configured and passed to the browser (`pbx-turn-media`, D-84); the rig runs a dev coturn. TURN credentials are static in Phase A (accepted gap).
- **BR-08 alert (D-80):** email through the existing mailer to tenant administrators and `ATSAPBX_CARRIER_ALERT_EMAILS`; webhooks wait for LLD-05.

---

## 6. The dev rig and its traps

- **Stack:** Docker Compose in `deploy/`, with containers `deploy-app-1`, `deploy-asterisk-1`, `deploy-postgres-1` (host port 15432), `deploy-sipua-1`, `deploy-mailpit-1` and `deploy-nats-1`. The owner normally develops under **Tilt** (`make tilt-up`), which live-updates the app. This week the app ran on the compose image instead.
  - **Rebuild only the app** with `docker compose --env-file .env -f deploy/docker-compose.yml build app`, then `docker compose --env-file .env -f deploy/docker-compose.yml up -d --no-deps --no-build app`.
  - **Do not use `make dev-app`** without Tilt: it also recompiles the Asterisk image (more than 30 minutes).
  - **Never run `make dev-app` while Tilt runs.**
- **The app binary** is `/out/app` under Tilt but `/usr/local/bin/atsap-api` in the compose image. Scripts and tests now read it from the container; do not hard-code either.
- **Reconcile:** `docker exec deploy-app-1 <binary> pbx reconcile [--fix]` should say "No divergence". The engine projection has one platform row, `carrier-ingress` (D-77); per-trunk identify rows must not exist.
- **The console** is at http://localhost:5173 (`make tilt-up`, or `cd console && npm run dev`). The login is `dev@example.test` / `e2e-fixture-operator-password`, tenant **Dev Acme** (`make dev-user` resets it). Mail lands in Mailpit at http://localhost:8025.
- **The owner's own phones** run as `baresip -f ~/atsap-phones/1001` and `.../1002` (extensions 1001 Reception and 1002 in Dev Acme), **on UDP 5070 and 5071, the same ports the e2e inbound rig and the phone UATs use**.
  - Before those tests, ask the owner, then stop the phones.
  - Afterwards restart them with `setsid nohup baresip -f /home/mbaapoh/atsap-phones/1001 >/dev/null 2>&1 &` (and the same for 1002), and check they re-register (`select endpoint from ps_contacts`).
  - Their configs share `ctrl_tcp_listen 4444`, which is harmless but worth separating.
- **The owner's trunks** in Dev Acme are `test` and `trunk2test` (host `localhost`, now "Address not allowed" by design) and `testwithsteriskhost` (host `asterisk`, Healthy).
- **Test residue:** about 330 throwaway e2e tenants remain in the rig database. Their cleanup cannot delete a tenant that has call rows. They are harmless; making the cleanup complete is a small to-do.
- **The machine is unstable under heavy parallel load** (2026-11-01, morning and afternoon). Prebuilt tools (OSV-Scanner, govulncheck, golangci-lint, the Go compiler) and Node and Chrome workers died on internal runtime or V8 faults, and the kernel logged `segfault` and `trap int3`. Every gate passed on rerun or with fewer workers (`npx vitest run --coverage --maxWorkers=2`, `npx playwright test --workers=1`). The owner was advised to run a memory test. **A crash inside a tool is not a test failure: rerun before debugging.**
- **Docker can wedge after an interrupted `docker restart` (2026-11-04):**
  `docker start`/`rm` may hang and the container stays `Exited`. Restart
  the daemon (`sudo systemctl restart docker`), then `docker rm -f` the
  stuck container and `docker compose ... up -d` it again. The app,
  Postgres and coturn come back from their restart policies; re-check
  `pbx reconcile` for orphaned endpoints left by the interrupted run.
- **Owner phones bind UDP 5070/5071 (2026-11-04):** the owner's baresip
  profiles `/home/mbaapoh/atsap-phones/1001` and `/1002` listen on
  `0.0.0.0:5070` and `0.0.0.0:5071`, the same ports the Go e2e and the
  console phone UAT bind. Stop them (`pkill -x baresip`) before those
  tiers and restart them afterwards; otherwise calls fail with "call
  setup did not complete" and UAT refuses to run. Never use
  `pkill -f baresip` from a shell whose own command line contains it:
  it kills that shell too.
- **Host toolchain corruption (2026-11-02):** `/usr/lib/gcc/x86_64-pc-linux-gnu/16/libgcc.a` and `liblto_plugin.so` return `Input/output error` on `stat`, so the host's `go test -race` fails for **every** package with `relocation target __popcountdi2 not defined` — even an empty program. This is the filesystem, not the change. Until it is repaired (fsck / reinstall the toolchain), run the race tier in the local Go image:
  `docker run --rm -v /home/mbaapoh/atsap:/src -v /home/mbaapoh/go/pkg/mod:/gomod:ro -e GOMODCACHE=/gomod -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -e GOFLAGS=-mod=mod -w /src/api golang:1.26-bookworm go test ./... -race -cover -count=1`
  Non-race `go test`, `make lint`, integration and e2e are unaffected. Ask the owner before running fsck on a mounted filesystem.

---

## 7. Where things live

| Need | Look at |
|---|---|
| What is built and what is next | [PROGRESS.md](PROGRESS.md) (tracker, gaps, done changes) |
| Why things are the way they are | [DECISIONS.md](DECISIONS.md) (D-numbers) |
| Behaviour contracts | `openspec/specs/<context>/<capability>/spec.md` |
| Designs per bounded context | `docs/lld/` (LLD-03 `pbx-core` §7.5–7.7 describes carrier recognition) |
| Architecture and failure model | `docs/hld/` (T-6 trunk health is in `11-decisions.md`) |
| Tools and versions | [TOOLSET.md](TOOLSET.md); `.mise.toml` |
| Test conventions | [TESTING.md](TESTING.md); human UAT procedures in `docs/uat/` |
| Archived change records (with proofs) | `openspec/changes/archive/<date>-<name>/tasks.md` and `coverage.md` |
