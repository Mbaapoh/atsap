# Asterisk Integration — Interfaces, Limits and How They Fail

Everything the platform touches on the engine, what its documentation
says, what it does not say, and what we have observed. Every "observed"
entry below cost real debugging time; they are recorded so it is spent
once.

**Why this file exists.** Each of these interfaces fails in a way that
names something other than itself. A session limit presents as a test
timeout. A missing sorcery wizard presents as a broken test in an
unrelated bounded context. A schema teardown presents as an ARI
`500 Allocation failed`. The pattern is constant: **the symptom points
away from the cause**, so the cause is written down here.

Related: [`DECISIONS.md`](DECISIONS.md) D-41 (one engine, behind the
ACL), D-47 (PJSIP Realtime projection); [`WORKFLOW.md`](WORKFLOW.md) §4
(rig discipline); [`TOOLSET.md`](TOOLSET.md) (approved libraries).

---

## 1. The four interfaces, and which is authoritative for what

| Interface | We use it for | We must NOT use it for |
|---|---|---|
| **ARI** (REST + WebSocket) | Call control: originate, bridge, answer, playback, hangup | Creating configuration objects — it cannot (§2.3) |
| **Stasis** (the ARI application) | Receiving call events and driving the call in our own code | Dialplan logic. There is no generated dialplan (D-47) |
| **AMI** | Coarse system events we do not get from ARI | Call control. Anything ARI can do, ARI does |
| **Sorcery / PJSIP Realtime** | Making endpoints, auths, aors and contacts exist for the engine | Domain state. `ps_*` is a projection, never the source of truth |

**Upstream documentation:**

- ARI — <https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/>
- ARI REST reference — <https://docs.asterisk.org/Asterisk_22_Documentation/API_Documentation/Asterisk_REST_Interface/>
- AMI — <https://docs.asterisk.org/Configuration/Interfaces/Asterisk-Manager-Interface-AMI/>
- Sorcery — <https://docs.asterisk.org/Development/Reference-Information/Asterisk-Framework-and-API-Examples/Sorcery-API/>
- PJSIP Realtime — <https://docs.asterisk.org/Configuration/Interfaces/Realtime-Database-Configuration/>
- `http.conf` — <https://docs.asterisk.org/Configuration/Core-Configuration/Asterisk-Builtin-mini-HTTP-Server/>

Version in use: **Asterisk 22.8.2 LTS**. Behaviour below is verified
against that; a major upgrade re-verifies this file.

---

## 2. ARI

### 2.1 Sessions are threads, and there are 100 of them by default

`http.conf`'s `sessionlimit` defaults to **100**, and Asterisk services
each HTTP session on its own thread. Both ARI REST calls and ARI
WebSockets are HTTP sessions.

`core/conf/http.conf` raises it to 500 with a ten-second idle reclaim.
Raised, not removed: unbounded trades a clear failure for memory
exhaustion. Verified applied — thread counts now climb past 100, which
they could not before.

**Read the next section before concluding this explains a failure.**

### 2.1.1 RESOLVED: an upstream websocket session leak (Asterisk < 22.11.0)

**Correction 2026-10-07:** the leak fix below holds, but "the wedge is
fixed" did not. The same test-side symptom came back on 22.11.0 from a
second, independent mechanism — an ARI teardown deadlock, upstream
#2170 — and is fixed on our side; see §2.1.2. The old note further down
that suspected "two mechanisms" was right.

**Status 2026-09-09: root cause identified and fixed by upgrading the
engine to 22.11.0.** The fix is the version pin in `core/Dockerfile`;
there is no code change on our side. Do not downgrade below 22.11.0.

**Cause.** Asterisk issue #2020, *"Possible leak of inbound websocket
session"*: two error paths in `res_http_websocket` allocated an
`ast_websocket` session and returned without dropping the reference. One
is reachable when the ARI pre-callback fails. Every e2e run leaked
sessions until the engine stopped serving new ARI event streams. Fix
commit: `res_http_websocket: Unref session when it fails to establish`,
released in 22.11.0.

**The controlled A/B that established it.** Same test code, same
`mise run rig:restore` procedure, same six-run loop, only the
`ASTERISK_VERSION` build arg changed. Six consecutive runs on one rig
with **no** restart between them:

| Run | 22.8.2 threads / result | 22.11.0 threads / result |
|---|---|---|
| 1 | 82 — PASS | 84 — PASS |
| 2 | 92 — FAIL | 84 — PASS |
| 3 | 96 — FAIL | 84 — PASS |
| 4 | 100 — FAIL | 84 — PASS |
| 5 | 104 — FAIL | 84 — PASS |
| 6 | 108 — FAIL | 84 — PASS |

Thread count is the leak signature and the reason this is attribution
rather than correlation: on 22.8.2 it climbs monotonically by ~4 per run
and lingering `stasis/m:ari:application` taskprocessors go 1 → 3 and
stay; on 22.11.0 both are flat across all six. Test-side changes cannot
hold an engine's thread count flat.

**Why it took so long to find.** The first run always passed, on every
version. Any loop shorter than two runs reports success, which is why
the six-run loop — not a single green run — is the acceptance test.
The script is `scripts/wedge-loop.sh`; keep using it after any
engine or ARI-client change.

**Everything below is the investigation that preceded the fix.** It is
kept because each entry cost real time and re-running any of it would
waste that time again.

**Symptom.** A client on the host, connecting through the published port
`127.0.0.1:8088`, sends the WebSocket upgrade and receives nothing.
Asterisk accepts the TCP connection and never answers. Tests report:

```
timed out waiting for Stasis app <name> to register
ari: dial event stream: read tcp 127.0.0.1:…->127.0.0.1:8088: i/o timeout
```

Plain HTTP through the same port answers `200` normally, so the engine
looks healthy and its healthcheck passes.

**What has been ruled out, with evidence:**

| Suspected | Evidence against |
|---|---|
| Session limit (§2.1) | Raised to 500 and confirmed live; threads reached 111, well under the cap, and every run still failed |
| Stasis app-name collision | Giving each test a unique app name changed nothing |
| Abrupt WebSocket close | Adding the RFC 6455 handshake made it **worse** (§2.5) |
| Goroutine or socket leak in our client | Fixed and asserted; failures persist |
| File-descriptor exhaustion | Container limit is 65536 |
| Stasis apps accumulating | `ari show apps` is empty when failing |
| Missing schema / deregistered devices | Schema present (17 tables), `pjsip show contacts` shows 2 |

**A lead that was pursued and died — kept so it is not re-run.** While host-side upgrades
hang, the **app container's** ARI stream — established over the Docker
network rather than the published port — logs zero reconnects. That
points at the host→container port-publishing path for long-lived
upgrades rather than at Asterisk. It is *not* proven: an already-
established stream surviving says nothing about whether a NEW in-network
upgrade would succeed, and that comparison has not yet been made
cleanly.

**Update 2026-09-09 (e2e-wedge troubleshooting):** the port-publishing
half of the lead is ruled out, and the picture is narrower and stranger.
Measured on a wedged engine, each item fresh and independent:
- A WS upgrade aimed directly at the container IP (no NAT involved)
  hangs identically: 8s, zero bytes.
- `/` answers 404 in 9ms and bad credentials answer 401 in 8ms, while
  every **authenticated** `/ari/*` call — REST and WS alike — hangs.
  TCP, HTTP dispatch and auth are fine; the stall is past auth.
  (Refined by the 22.8.2 control run above: authenticated **REST** still
  answered `200` on every failing run while only the WS upgrade timed
  out. Total authenticated hang is a later, deeper stage of the same
  leak, not its first symptom — so "REST answers, therefore the engine
  is fine" is not a safe inference at any stage.)
- Stopping the app container (removing all client retry pressure) does
  not clear it; 150s of idle does not clear it. Only an Asterisk restart
  clears it. No TCP pileup (zero sockets on 8088), no taskprocessor
  backlog, 108 threads against a 500 session limit.
- Stuck registrations are real and distinct from the stall: a previous
  run's app name can remain registered (GET returns 200) 40+ minutes
  after its socket is gone, and unique names per run accumulate one
  lingering `stasis/m:ari:application/<name>` taskprocessor each.
  The stall has also been observed with residue as low as 3, so
  accumulation alone does not explain it — there are either two
  mechanisms or one whose trigger is not the count.

**The candidate that did NOT fit, recorded so it is not re-credited:**
#1979, *"outbound client handshake read has no timeout"* — a
non-responsive peer hangs the calling thread forever *while holding the
channel lock*, stalling the process. The shape matches the symptom
exactly, which makes it tempting, but the direction is wrong: that path
is Asterisk dialling **out** over WebSocket, and nothing in this
deployment makes it do so. It is also fixed in 22.11.0, so an upgrade
alone cannot distinguish the two — only the leak signature (flat vs.
climbing thread count) does.

**The workaround this replaced:** before the upgrade, `mise run
rig:restore` was the only remedy — it restarts Asterisk, after which one
e2e run passes and subsequent ones may not. `rig:restore` is still the
right tool between test tiers for its *other* reason (the integration
tier drops the schema), but it is no longer needed to un-wedge ARI.

### 2.1.2 RESOLVED: ARI teardown deadlock with `subscribeAll` (upstream #2170, open)

**Status 2026-10-07: root cause identified from thread stacks; fixed on
our side by not requesting `subscribeAll`.** No engine change.

**Symptom.** Identical to §2.1.1 at the test: `timed out waiting for
Stasis app … to register` (or `… to release`), `ari: dial event stream:
… i/o timeout`. On the engine: every authenticated `/ari/*` call hangs,
REST included, while `/httpstatus` answers instantly; engine threads
climb ~4–6 per run; a dead app's `stasis/m:ari:application/<name>`
taskprocessor lingers with items queued. Postgres was clean (no blocked
queries, engine session idle), and the engine image was bit-identical to
the one that passed the §2.1.1 A/B (same `res_http_websocket.so`,
`res_ari.so` and config checksums), so neither was the cause.

**The lock cycle** (gdb on the wedged 22.11.0 engine, symbols resolved
with `set sysroot /proc/1/root` from a sidecar sharing its PID
namespace):

1. A websocket closes. `session_cleanup` → `session_reset` takes the
   **ARI session lock**, then `stasis_app_unregister` →
   `unsubscribe_device_state` → `stasis_unsubscribe_and_join` waits for
   the app's device-state subscription to drain.
2. That subscription's in-flight `device_state_cb` → `app_send` →
   `ari_websocket_send_event` blocks on the **same session lock**.
   Neither can proceed.
3. The next run's app registration (`__stasis_app_register` →
   `subscribe_device_state`) holds the global **apps container** and
   blocks on the device-state container the teardown holds — so every
   `/ari/*` request (`find_app_by_name`) queues behind it, one HTTP
   thread each. That is the thread climb.

The trigger is closing a websocket whose app has an **all-device-state**
subscription while a device-state event is in flight. `subscribeAll=true`
creates exactly that subscription, and every e2e test hangs up calls (a
burst of device-state changes) and then disconnects. It is a race, which
is why run 1 after a restart usually passed; residue that adds endpoints
(pbx e2e leaves `e_*` rows) plausibly widens the window — not proven.

**The fix.** The client no longer requests `subscribeAll`
(`internal/telephony/acl/ari/events.go`; `TestClient_EventsURL` now
forbids it). The event loop consumes only `StasisStart`,
`ChannelDestroyed` and `ChannelHangupRequest` for channels the app
originates, which ARI subscribes it to regardless, so nothing used was
lost. If a future feature needs device or endpoint state, subscribe to
those specific sources (`POST /applications/{app}/subscription`) — never
re-enable `subscribeAll` while #2170 is open.

**Evidence, same rig and procedure, one variable:**

| Loop (no restart between runs) | `subscribeAll=true` (HEAD and working tree) | without |
|---|---|---|
| `scripts/wedge-loop.sh 6` (telephony) | 0/6, threads 85 → 120 | 6/6, threads 84–85 flat |
| then telephony + pbx suites ×6, same engine | — | 6/6, threads 85 flat |

Upstream: [asterisk/asterisk#2170](https://github.com/asterisk/asterisk/issues/2170).
When it is fixed and the pin moves past it, `subscribeAll` becomes safe
again but remains unnecessary.

### 2.2 The WebSocket client needs bounds the libraries do not give it

`gorilla/websocket`'s `DefaultDialer` has a **45-second**
`HandshakeTimeout`, and Go's `http.DefaultClient` has **none at all**.
Against a stalled engine both turn a caller's intent into a hang: a
helper with its own ten-second deadline cannot enforce it while blocked
inside `Do`.

`internal/telephony/acl/ari` therefore sets, and
`internal/archtest`'s `ScanDefaultHTTPClient` prevents the regression:

| Bound | Value | Catches |
|---|---|---|
| `http.Client.Timeout` | 10s | Anything, as a backstop |
| `Transport.ResponseHeaderTimeout` | 5s | "Connected, never answered" — the §2.1 failure |
| `net.Dialer.Timeout` | 3s | An unreachable engine |
| `Transport.IdleConnTimeout` | 30s | Our own sockets outliving the engine's session pool |
| `websocket.Dialer.HandshakeTimeout` | 10s | A stalled upgrade |

### 2.3 ARI cannot create configuration objects

`PUT /ari/asterisk/config/dynamic/res_pjsip/endpoint/...` returns
**`403 "Cannot create sorcery objects of type 'endpoint'"`** on a default
build. Tested 2026-09-07. This is why D-47 exists: configuration reaches
the engine through PJSIP Realtime, not ARI. `/ari/endpoints` is read-only
(`GET`, plus messaging and refer).

**Never propose ARI dynamic configuration.** It has been tried.

### 2.4 Reconnects must be bounded and jittered

An event stream that drops reconnects forever by design — an engine
restart must leave the platform reconnecting, not deaf. But an unbounded
or unjittered retry loop against §2.1's session pool turns a blip into an
outage that will not clear, because each synchronised wave consumes the
sessions the next wave needs.

Ours: capped exponential backoff (1s → 30s) with **full jitter**, reset
after a connection survives 30 seconds. See `events.go`.

### 2.5 Stasis registrations are per-connection and slow to release

An application exists only while some WebSocket subscribes to it. When
the socket goes, so does the registration — and **Asterisk releases it on
its own schedule, not immediately**.

**Consequence for tests:** every test that composes its own ARI client
holds a registration. Two tests in one binary behaved better than three,
so sharing one engine connection across related assertions is the safer
shape — the same discipline as sharing one database. Whether registration
count is genuinely the constraint is unproven; see §2.1.1.

**Do not** try to solve this with a unique app name per test. Measured
2026-09-09: it changes nothing.

**Do** use one stable name per test plus a release drain. Since 2026-09-09
the e2e helpers derive `e2e-<test>` (no run-unique suffix) and wait for
the previous registration to release (404) before subscribing
(`waitForStasisAppReleased`). Rationale: unique names convert each run's
registration into permanent server-side residue (one lingering Stasis
taskprocessor per run, never reclaimed), while the drain covers exactly
the release lag the suffix used to dodge. Stable names do NOT cure the
§2.1.1 stall — observed with residue as low as 3 — they only stop
feeding it names.

**Do not** add the RFC 6455 closing handshake to "release it faster".
Also measured: it made things **worse**. Teardown slowed enough that the
next registration arrived before the previous was released, taking a
suite that passed three tests down to one. Asterisk logs the abrupt close
as `WebSocket connection forcefully closed due to fatal write error`;
that log line is noise we accept.

---

## 3. AMI

Used for coarse system events only. Call control is ARI's, always
(D-41 keeps engine vocabulary inside the ACL either way).

**Limits worth knowing:**

- **AMI is line-oriented and stateful**, not request/response. A parser
  that assumes one reply per command breaks on asynchronous events
  arriving mid-exchange.
- **Permissions are per-class** (`system,call,log,verbose,command,agent,user,dialplan`
  in `manager.conf`). `command` grants arbitrary CLI execution — it is
  the AMI equivalent of a shell, and production overlays must justify it
  or drop it.
- **`deny`/`permit` default to permissive in our dev config**
  (`0.0.0.0/0`). Production ships a separate overlay; this is called out
  in `manager.conf` itself.
- AMI does not survive an Asterisk restart: reconnect logic is required,
  with the same bounded-and-jittered shape as §2.4.

---

## 4. Sorcery and PJSIP Realtime

### 4.1 A realtime wizard REPLACES the config-file wizard

The single most likely way to break this configuration, and the failure
appears somewhere else entirely.

Adding `endpoint=realtime,ps_endpoints` to `sorcery.conf` **removes** the
default `config,pjsip.conf` wizard unless it is restated. Without the
`config` lines, the dev fixtures 1000 and 1001 vanish from Asterisk, and
telephony-core's walking-skeleton test — which dials them — fails with no
visible connection to this file.

Wizards are consulted **in the order listed**, so static fixtures resolve
first and the database is asked only for what the file does not define.

### 4.2 `requirements=warn`, never `createchar`

`res_pgsql.conf` sets `requirements=warn`. The schema is owned by
`api/migrations`; an engine that can alter it is an engine that can
disagree with the migration defining it. A column mismatch is a warning
to fix in a migration, never a table Asterisk quietly reshapes.

### 4.3 A partially-specified `ps_contacts` corrupts registration silently

Asterisk's contact INSERT names sixteen columns. If one is missing the
write fails — and **the device still receives `200 OK` while no contact
binds**. The phone believes it is registered and is unreachable.

This is why `ps_contacts` is mapped with its full column set (D-47), and
why it is the one projection table the engine may write.

### 4.4 The engine polls, so a missing table is a continuous error

PJSIP Realtime polls `ps_contacts` for expirations roughly every second.
If the schema is absent — which the integration suite causes, since it
migrates down on exit — Asterisk logs a failure per poll:

```
res_config_pgsql.c: PostgreSQL RealTime: Query Failed because:
ERROR: relation "ps_contacts" does not exist
```

Harmless in itself, but it fills the log and buries whatever you are
actually looking for. `mise run rig:restore` fixes it.

### 4.5 Registrations live in the database now

Since `ps_contacts` became realtime-backed, device registrations are
database state. The integration suite's schema reset therefore
**deregisters `cmd/sip-ua`**, and the e2e tier then fails with ARI
`500 "Allocation failed"` on `PJSIP/1000` — an error naming neither
registration nor the database.

Two consequences beyond testing, both real: a restore or failover leaves
every device unreachable until it re-registers, where node-local `astdb`
was unaffected by anything happening to Postgres; and it is the one
projection table Asterisk writes, so the engine role's grants are not
uniform (D-47).

---

## 5. Diagnosing an engine problem

In order. The first two separate "our code" from "the rig" and take
under a minute.

```bash
# 1. Does an e2e test we did NOT touch still pass?
#    If it fails too, the rig is broken, not the change.
go test ./internal/telephony/e2e/ -tags e2e -p 1 -run TestWalkingSkeleton

# 2. Is Asterisk at its session limit? (§2.1)
docker exec deploy-asterisk-1 sh -c 'ls /proc/$(pgrep -o asterisk)/task | wc -l'

# 3. What does the engine itself say?
docker exec deploy-asterisk-1 asterisk -rx "ari show apps"
docker exec deploy-asterisk-1 asterisk -rx "pjsip show contacts"
docker exec deploy-asterisk-1 asterisk -rx "core show channels"
docker logs --since 2m deploy-asterisk-1 | tail -30

# 4. Restore the rig and retry.
mise run rig:restore
```

**Step 1 is the one that gets skipped and should not be.** On 2026-09-09
several cycles went into debugging a new test while the engine was the
problem; running the untouched test first would have shown that
immediately.

---

## 6. What we accept

Stated so they are not rediscovered as bugs:

- **Asterisk is a black box behind the ACL** (D-41). We assert on ARI
  entities, events and CLI-visible state — never on SIP retransmission
  timing or dialplan mechanics, because there is no dialplan.
- **`res_config_pgsql` is Asterisk *extended* support**, not core.
  `res_config_odbc` is core. Switching costs one Debian package and a
  DSN — no schema change, no code change (D-47).
- **An in-progress call cannot be moved** between engines or nodes. It is
  an external limitation, stated openly in service commitments.
- **A partner's Asterisk is theirs.** We ship configuration and a
  container; we do not manage their kernel, their NAT, or their carrier.
