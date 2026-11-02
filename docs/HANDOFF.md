# Handoff — where AtsaPBX stands and how to continue

**Written:** 2026-11-01, at the end of a working week, so that anyone — a
person or a different AI model — can pick up exactly here. Read this
first, then [PROGRESS.md](PROGRESS.md). When this file and PROGRESS,
DECISIONS or `openspec/specs/` disagree, **they** win; fix this file.

---

## 1. The next work, in order (owner decision, 2026-11-01)

The owner closes **all of these in the current phase** (Phase A, before a
pilot), in this order:

| # | What | State | How to start |
|---|---|---|---|
| **7a** | **Call session UX** (owner request, 2026-11-02): a persistent call bar with live state and one-click Hang up/Cancel, a global dialer (searchable extension, keypad, recents), live Call history, and `GetCall` timestamps/reason | **Done and archived 2026-11-02** (`console-call-session`, D-79); proven by full Go e2e, all three phone UATs, `make console-check` and the console tiers | — |
| **7** | **Click-to-call**: ring the caller's phone first, then the destination; a "Place a call" action and a "Hang up" action on Call history | **Done and archived 2026-11-02** (`console-click-to-call`, D-78); proven by e2e, all three phone UATs and `make console-check` | — |
| **5** | **Trunk health exactly as HLD T-6** ([hld/11-decisions.md](hld/11-decisions.md)): check every 10 s; *Degraded* after 3 consecutive failed checks **or** > 20 % call-setup failures over 60 s (min 5 calls); recover only after 5 min of healthy checks | Not planned yet. The owner chose **implement T-6 as written**, not amend it | `/opsx:propose` a change (suggested name `pbx-trunk-health-thresholds`) |
| **6** | **BR-08 carrier-failure alert**: the operator is told when a carrier fails | Not planned. Pairs naturally with #5: alert on the thresholded state change, never on one failed check | Same change as #5, or the next one |
| **8** | **Browser calling** (BRD FBR-R1-01, PRD EPIC-03): a softphone in the console over WebRTC | Not planned. The owner placed it in this phase | `/opsx:propose`. It needs owner decisions first (see §5) |

The numbering (#5 to #8) is the refinement list proposed on 2026-11-01. #1 to #3 are done: `pbx-carrier-addresses` and `pbx-carrier-ingress`; **#7 is done and archived 2026-11-02** (`console-click-to-call`, D-78); **#7a is done and archived the same day** (`console-call-session`, D-79). **#5 is next.** PROGRESS's own "Before a pilot" list continues after these with toll-fraud classes and Sign in with Google.

**Deferred by the owner, do not start unasked:**
- `quality-and-supply-chain-gates` task 5.3 (the Asterisk image scan: a multi-stage image, and `--ignore-unfixed`).

---

## 2. What was done this week

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
- **The product docs** are BRD, then PRD, then HLD (`docs/hld/`), then LLD (`docs/lld/`), then OpenSpec specs (`openspec/specs/`). **Never re-litigate a decision** in [DECISIONS.md](DECISIONS.md) (append-only, D-1 to D-77); say so explicitly if one must change.
- **Every piece of work is an OpenSpec change:**
  1. `/opsx:propose`: proposal, delta specs, design and tasks.
  2. `/opsx:apply`: one task at a time.
  3. `/opsx:archive`: syncs the delta specs into `openspec/specs/` and moves the change to `openspec/changes/archive/`.
- **Per task** (owner rule, "auto mode"):
  1. Implement.
  2. Run every relevant tier (§4).
  3. Tick the task in `tasks.md` with a short note of what was proven.
  4. Make **one commit per task**, then push to `develop`.

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

## 5. Decisions the owner must take before #8 (browser calling)

- **Which SIP-over-WebSocket library** for the console softphone, for example SIP.js or JsSIP. It is a new runtime dependency, so it needs a D-68 amendment with its licence checked against D-35.
- **The engine's WSS transport.** `core/conf` has no WSS transport yet: `pbx/acl/asterisk/projector.go` warns that projecting a WEBRTC extension points at a transport that does not exist. TLS certificates for the dev rig and for production need settling too.
- **Who may use the browser phone in Phase A.** Users have no extension yet (D-65; extension ownership is Phase B). Either the softphone registers as an extension the administrator picks, or #8 pulls `pbx-extension-assignment` forward.
- **NAT and media:** TURN/STUN for browsers behind NAT (coturn as a new component, or ICE without TURN in Phase A).

#5 and #6 need fewer decisions:
- The alert channel for BR-08: **email through the existing mailer** (Mailpit on the rig) is the simplest. Webhooks are LLD-05, later.
- Who receives the alert: tenant administrators, the platform operator, or both.

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
