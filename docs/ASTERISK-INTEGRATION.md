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

**The limit that has bitten us hardest.** `http.conf`'s `sessionlimit`
defaults to **100**, and Asterisk services each HTTP session on its own
thread. Both ARI REST calls and ARI WebSockets are HTTP sessions.

**How it fails:** at the limit, Asterisk **accepts the TCP connection and
then never answers**. It does not refuse, does not log a rejection, and
does not close. Clients see a read timeout. Ours saw:

```
timed out waiting for Stasis app voip-app-e2e to register
ari: dial event stream: read tcp …:8088: i/o timeout
```

Neither names Asterisk, HTTP, or a limit. `ari show apps` shows nothing
registered, which reads like an application bug.

**How to confirm it** — the thread count sitting exactly at the limit is
the tell:

```bash
docker exec deploy-asterisk-1 sh -c 'ls /proc/$(pgrep -o asterisk)/task | wc -l'
```

**What we did:** `core/conf/http.conf` sets `sessionlimit = 500`, plus
`session_inactivity = 10000` and `session_keep_alive = 5000` so idle
sessions are reclaimed in ten seconds rather than thirty. Raised, not
removed: unbounded trades a clear failure for memory exhaustion.

**This is not a container artefact.** The default is sized for a handful
of long-lived HTTP clients; ARI is neither. Bare metal behaves
identically. The container only surfaced it sooner because everything is
short-lived and restarted often.

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
holds a registration. Two tests in one binary is fine; three began
failing before §2.1 was fixed. Prefer sharing one engine connection
across related assertions — the same discipline as sharing one database.

**Do not** try to solve this with a unique app name per test. Measured
2026-09-09: it does not help, because the constraint is on sessions, not
names.

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
