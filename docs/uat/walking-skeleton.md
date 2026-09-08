# UAT — Telephony-Core Walking Skeleton (human pass)

User Acceptance Testing for OpenSpec change
`telephony-core-originate-bridge-hangup` (LLD-01 §8 Definition of Done).
Automation proves records, events, and timing; only humans can prove
ringing, answering, and real two-way audio. This document is both the
reusable procedure and the executed record.

Traceability: PRD EPIC-03 (browser calling & PBX core — calling only, no
IVR), PRD §11.1 (call lifecycle), delta spec
`openspec/changes/telephony-core-originate-bridge-hangup/specs/telephony-core/call-lifecycle/spec.md`,
`docs/TESTING.md` (conventions).

## 1. Environment (as executed)

| Item | Detail |
|---|---|
| Date | 2026-09-07 |
| Asterisk | 22.8.2 (`atsap-core:dev`, pinned + checksum-verified `core/Dockerfile`) |
| App | `atsap-api:dev` (tasks 1–9 wired: Service, ACL, outbox worker, GetCall) |
| Backing services | Postgres 16 (`atsapbx` DB + roles), NATS 2.10 JetStream |
| Fixtures | PJSIP endpoints `1000`/`1001`, userpass auth, `ulaw,alaw` only, `max_contacts=1` |
| Softphones | baresip 4.6.0 ×2 (`~/.baresip-1000`, `~/.baresip-1001`), PipeWire audio, codecs PCMU/PCMA only |
| Softphone network | Fixed SIP ports `5070`/`5071`, RTP `18000–18039`; host ufw allows UDP for these **from `172.18.0.0/16` only** (compose net). Without this, Asterisk→host INVITEs are dropped and legs die at the 20 s timeout — misdiagnosable as an app bug |
| Sidecar | `sipua` stopped for any baresip run, human or automated (`max_contacts=1`); restarted afterwards |
| Auto variant | Versioned templates in `scripts/uat/baresip-{1000,1001}-auto/` (manual config plus `ctrl_tcp` on `127.0.0.1:5550`/`5551` and `rtp_stats yes`). Answering is driven per-run via ctrl_tcp `answermode auto` (reply verified) — NOT the `call_accept` config key, which parses but does not auto-answer INVITEs (proven 2026-09-07). Media evidence comes from each phone's end-of-call RTP summary (`packets` TX/RX) |

## 2. Entry / exit criteria

- Entry: automated e2e (`go test -tags e2e`) green with the sidecar; both
  human endpoints show `Avail` in `pjsip show contacts`.
- Exit: every case below PASS with evidence; rig left green (sidecar back,
  automated e2e re-run passing).

## 3. Test cases

### UC-01 — Answered two-party call with real audio
Objective: a human answers both legs; audio flows both ways; platform
records everything correctly.
Preconditions: §2 environment; both phones registered, idle.
Steps:
1. Run `cd api && go test -tags e2e ./internal/telephony/e2e/ -v -count=1`.
2. Answer both ringing phones within ~10 s (`a` + Enter in each baresip).
3. Talk in both directions for a few seconds.
4. Let the test hang up via ARI — or hang up one phone (`b`) to also
   exercise the SIP-BYE path.
Expected: both ring; answer connects audio both ways; test asserts
`Active` → `Terminated`, continuous non-duplicated `usage_seconds` for
both participants, NATS `initiated → active → terminated`, leak-free
`GetCall` (`Terminated`).

### UC-02 — Unanswered call times out with zero billing
Objective: the alerting timeout path terminates cleanly and bills nothing.
Steps: as UC-01 but **do not answer**.
Expected: test fails at the Active wait (expected — the point); the call
reaches `Terminated` with reason `call setup did not complete` ~20 s
after originate; `usage_seconds` empty; outbox/NATS shows `initiated →
terminated` with **no** `active`; phones see `Cancel Q.850 cause 19`.

### UC-03 — Record inspection through the public API
Objective: a partner-equivalent client can read the finished call.
Steps: obtain a token first — `GetCall` requires one since
`auth-cutover-connectrpc`, and an unauthenticated call now returns `401`:

```bash
TOKEN=$(curl -sS -X POST http://127.0.0.1:8080/atsapbx.v1.IdentityService/AuthenticateUser \
  -H 'Content-Type: application/json' \
  -d '{"tenantId":"<tenant>","username":"<operator>","password":"<password>"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')

curl -sS -X POST http://127.0.0.1:8080/atsapbx.v1.TelephonyService/GetCall \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"tenantId":"<tenant>","callId":"<call>"}'
```

The tenant in the body must match the token's, so use the operator's own
tenant. Then inspect `usage_seconds` and `outbox` rows directly.
Expected: `Terminated` + final billable seconds (UC-01) / no usage
(UC-02); every outbox row `published`; no `atsa-part-`/channel-shaped
content in any API response or event payload.

### UC-04 — Automated regression on the same real phones
Objective: re-prove UC-01 unattended on the identical endpoints, so every
future change re-runs the human cases without a human. Manual UAT stays
for first-proof, audio-quality judgment, and exploratory work; UC-04 is
the regression net.
Steps: `mise run uat:auto` (i.e. `scripts/uat-auto.sh`), which stops the
sidecar, installs the auto configs, starts both phones, waits for both
registrations, sets `answermode auto` over ctrl_tcp (verified reply),
runs the unchanged Go e2e, then asserts endpoint-measured media and
restores the rig (kills auto phones, restarts sidecar) even on failure.
Expected: e2e PASS (Active → Terminated, continuous usage, ordered NATS,
leak-free GetCall) **and** both phones report RTP `packets` TX>0 and RX>0
with zero errors — bidirectional media measured at the endpoints, not
inferred from signaling.

## 4. Execution record

| Case | Result | Evidence |
|---|---|---|
| UC-01 | **PASS** 2026-09-07 ~09:44 UTC | Call `963848b1`: originated 09:44:31, answered 09:44:38 (~7 s human answer time), ended 09:44:41, `normal clearing`. baresip logs both sides: `Incoming call` → `Call established`, PCMU encoder+decoder, PipeWire capture+playback started, 3–4 s audio. `usage_seconds`: 7 continuous non-duplicated rows / 2 participants. NATS: `call.initiated, call.active, call.terminated` in order. `GetCall`: `Terminated`, leak-free. Test: `PASS ok 10.46s` |
| UC-02 | **PASS** 2026-09-07 ~09:49 UTC | Call `02fd8a7c`: 09:49:19→09:49:39 (exactly the 20 s alerting timeout), never answered, `call setup did not complete`. `usage_seconds`: 0 rows. Outbox: `call.initiated → call.terminated`, both published, no `active`. Phones observed `Cancel Q.850 cause=19`. (Test-failure at the Active wait is the expected signal, not a defect.) |
| UC-03 | **PASS** | `GetCall` returned `Terminated` + final billable durations; outbox rows all `published`; no channel-ID content in API/event payloads (mirrors task 8.2's automated scan on live data) |
| UC-04 | **PASS** 2026-09-07 ~11:08 UTC | `mise run uat:auto` first-try green (4.4 s): e2e PASS on auto-answering real phones (no human), rig auto-restored. Media measured at endpoints — phone 1000: TX 152 / RX 151 packets, phone 1001: TX 152 / RX 152, 0 errors, ~64 kbit/s PCMU both ways |

Logs: app log free of ERRORs across both runs; Asterisk log free of errors
except historical `cel_pgsql varchar(32)` noise (schema since widened to
64 — zero occurrences during these runs).

## 5. Known issues (non-blocking)

1. **Noisy log on double-destroy race:** when both legs are torn down
   ~simultaneously, the second `ParticipantLeft` finds the call already
   removed and logs `participant left ... call not found` at ERROR. Harmless
   (expected race, correct final state) but misleads; consider downgrading
   to Warn when the call is already `Terminated` — follow-up, not this change.
2. **Earlier failed attempts are also clean:** calls `5bb32ec9`,
   `20db7253` (registration-race / pre-firewall-fix era) each reached
   `Terminated` with `call setup did not complete` and zero usage — the
   failure paths terminate safely too.
3. The automated e2e's first run after any endpoint restart can race
   re-registration (stale contacts, `max_contacts=1`); a second run goes
   green. Operational quirk of the rig, not product behavior.

## 6. Sign-off

UC-01/02/03/04 PASS with evidence above. The walking skeleton is accepted
for human-affecting behavior (ring/answer/audio/hangup/timeout). Automated
DoD (31/31 tasks, `mise run ci`, e2e) plus this record = archive-ready.
Manual UAT remains the first-proof and audio-quality/exploratory path;
UC-04 is the unattended regression net for every future change.

Sign-off: ______________________ Date: __________
