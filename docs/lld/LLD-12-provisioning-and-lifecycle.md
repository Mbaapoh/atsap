# LLD-12 — Desk-Phone Provisioning and Installation Lifecycle

| | |
|---|---|
| **Bounded context** | **Part A (devices):** `pbx-core` — HLD [04 §2](../hld/04-bounded-contexts.md) already gives `pbx-core` the extension, its registration and the hot-desk device; a provisioned phone is the physical end of that extension, so it lives in a new module `api/internal/pbx/provisioning/`, not a new context. **Part B (install, upgrade, rollback, diagnostics, backup):** not a bounded context — installation tooling (`cmd/atsapbx-ctl`) plus a thin `api/internal/platform/lifecycle` read model. See §0.2 for the D-48 numbering question this raises |
| **Status** | **Draft — design only.** Nothing built. Every flow below is written to the twelve-item Flow Definition of Ready ([README](README.md), D-98); the `OWNER DECISION` items in §0.3 must be settled before the first OpenSpec change is proposed |
| **Traces to** | BRD FBR-R1-02, FBR-R1-13; PRD EPIC-03 (US-03.6, AC-03.5), EPIC-09 (US-09.1–09.6, AC-09.1–09.6), INV-01, INV-03, T-9, T-12; HLD [20](../hld/20-call-flow-catalogue.md) AD-07, AD-09; HLD [05](../hld/05-security.md), [06](../hld/06-deployment.md) §2–§5, [09](../hld/09-failure-model.md) §4, §6; [LLD-03](LLD-03-pbx-core.md) §3.8 (presence/BLF, SPIKE-P1), §3.10 (park), §3.13 (feature codes), §3.14 (hot-desk), §6 (`GetSipRegistrar`), §7 (projection, §7.3 credentials); [LLD-08](LLD-08-licensing.md) (fingerprint, Setup vs Degraded); [LLD-11](LLD-11-entitlement.md); DECISIONS D-12, D-13, D-20, D-39, D-43, D-47, D-48, D-52, D-55, D-56, D-86, D-98 |
| **Why it exists** | HLD-20 §11 scored AD-07 "no design" and AD-09 "needs detail"; §12 assigns both to "a new LLD-12". Both are R1.0 (PRD §15 maps FBR-R1-02 and FBR-R1-13 to R1.0) and both are where a self-hosted product loses partners: a phone that has to be configured by hand, and an upgrade that cannot be undone |

## Document history

| Date | Change |
|---|---|
| 2026-11-07 | Written. AD-07 split into flows A1–A8, AD-09 into B1–B5, each to the D-98 Definition of Ready. Migrations 0070–0074 reserved. Owner decisions OD-1…OD-11 listed in §0.3 |
| 2026-11-07 | §A.3.9 added: key rendering follows LLD-03 §3.8 presence/BLF (opaque key, pattern hint `atsa-blf`, SPIKE-P1 fallback) and §3.13 feature codes (tenant-effective pickup/DND codes) |

---

## 0. Scope, ownership and open decisions

### 0.1 Flows in this document

| Flow | Name | HLD-20 | Release | Owner (code) |
|---|---|---|---|---|
| A1 | Register a phone in the inventory (one or CSV import) | AD-07 | R1.0 | `pbx-core` / `pbx/provisioning` |
| A2 | Zero-touch first boot: phone fetches its configuration over HTTPS | AD-07 | R1.0 | `pbx/provisioning` (HTTP listener inside the monolith, D-20) |
| A3 | Resync and remote reboot (SIP NOTIFY `check-sync`) | AD-07 | R1.0 | `pbx/provisioning` → engine via AMI |
| A4 | Firmware: upload, pin, staged roll-out | AD-07 | R1.0 | `pbx/provisioning` |
| A5 | Phonebook XML | AD-07 | R1.0 | `pbx/provisioning` |
| A6 | Device status dashboard and stale detection | AD-07 | R1.0 | `pbx/provisioning` |
| A7 | Revoke, delete, replace (RMA) | AD-07 | R1.0 | `pbx/provisioning` |
| A8 | Template packs, line/BLF key templates, overrides | AD-07 | R1.0 | `pbx/provisioning/packs` |
| B1 | Install from documented media, with pre-flight | AD-09 | R1.0 | `atsapbx-ctl` |
| B2 | Upgrade: snapshot, expand, drain, switch, verify, commit | AD-09 | R1.0 | `atsapbx-ctl` + app drain gate |
| B3 | Rollback within the AC-09.4 window | AD-09 | R1.0 | `atsapbx-ctl` |
| B4 | Diagnostic bundle with redaction | AD-09 | R1.0 | `atsapbx-ctl` + `InstallationService` |
| B5 | Scheduled backup, offsite copy, restore | AD-09 (US-09.5) | R1.0 (offsite target: OD-6) | `atsapbx-ctl` + app scheduler |

**Out of scope (stated so nobody builds it by accident):** TFTP, FTP and
plain-HTTP provisioning (AC-03.5 requires an encrypted channel); SIP
multicast plug-and-play (`ua-profile` SUBSCRIBE on the LAN, 3CX's PnP —
unauthenticated, LAN-only; R1.1 at the earliest); DECT base stations and
handsets; per-tenant restore from a full backup; bulk migration from an
incumbent (AD-08, R1.1); multi-node upgrade orchestration beyond what
AC-09.2 requires of the single-machine profile.

### 0.2 Ownership and the D-48 question

- **Devices belong to `pbx-core`.** The matrix in HLD 04 §10.1 already
  permits everything Part A needs: `identity` (permissions),
  `telephony-core` (is this extension on a call?) and nothing else.
  Provisioning does **not** consult `entitlement` (recommendation, OD-10):
  FBR-R1-02 is core PBX, and the Free Community cap of 10 extensions
  already bounds the device count.
- **The installation lifecycle is not a bounded context.** It owns no
  domain aggregate; it operates on the deployment. The host CLI
  `atsapbx-ctl` (a second `main` in the same Go module, no new
  dependency) does the work; the app exposes a read model and a drain
  gate.
- **D-48 tension, reported rather than resolved here.** D-48 says one LLD
  per bounded context. HLD-20 §12 issued "LLD-12" for this work. Part A
  is `pbx-core` design held outside LLD-03 for size; Part B is not a
  context at all. Either D-48 gains an exception ("non-context
  operational LLDs, and a context may delegate a module to a companion
  LLD"), or Part A moves into LLD-03 as §3.5 and LLD-12 keeps Part B
  only. The README index row records `pbx-core` so `check-docs.sh` check
  5 keeps passing; no new context is added to HLD 04 §10.1.

### 0.3 OWNER DECISION register

| # | Decision | Options | Recommendation |
|---|---|---|---|
| OD-1 | R1.0 supported-model matrix (§A8.3) | (a) three families below; (b) add Fanvil and Snom; (c) Yealink only | **(a)** Yealink T4U/T5x, Poly VVX/Edge E, Grandstream GRP26xx — three template packs, ~13 models; Fanvil/Snom R1.1 |
| OD-2 | **Security trade-off:** the SIP secret of a device-bound extension is kept sealed (AES-256-GCM) so configs can be re-rendered. LLD-03 §7.3 currently says the plaintext is never written anywhere | (a) seal, device-bound lines only; (b) rotate the SIP secret on every config fetch, store nothing; (c) no provisioning of credentials (engineer types them) | **(a).** (b) locks a phone out whenever a response is lost; (c) fails AC-03.5. 3CX, FreePBX and FusionPBX all keep SIP passwords readable in their database; we keep fewer (only device-bound lines) and encrypted |
| OD-3 | **Security trade-off:** a "claim window" for DHCP-only phones with no factory certificate — first unauthenticated fetch accepted once, from an allowed network, within N minutes of the admin arming it | (a) not offered; (b) offered, off by default, CIDR required; (c) on by default | **(b)**, default off, 30 min, CIDR mandatory |
| OD-4 | Vendor redirect services (Yealink RPS, Poly ZTP, Grandstream GDMS): whose accounts, and automation | (a) partner's own accounts, manual entry assisted by CSV export; (b) API automation with partner's keys stored per installation; (c) our vendor-level accounts | **(a) for R1.0**, (b) R1.1. (c) makes us an operator of partner devices, contrary to D-01 |
| OD-5 | Upgrade started from the console | (a) CLI only; (b) console, app container gets the Docker socket | **(a)**. The Docker socket is root on the host; the console shows status and the exact command |
| OD-6 | Offsite backup target for R1.0 | (a) local path only (an NFS/SMB mount is the offsite path); (b) + S3-compatible (**new dependency**); (c) + SFTP (**new dependency**) | **(b)**; SFTP R1.1 |
| OD-7 | Backup retention ceiling, tied to the stated deletion window (AC-07.3, AC-23.2, T-9) | a number of days | **35 days**: 7 daily + 4 weekly fits inside it |
| OD-8 | Call audio in a diagnostic bundle with opt-in (AC-09.5 permits it) | (a) never; (b) per-call opt-in | **(a)** in R1.0 — support receives audio through the recording export and its own access audit (LLD-09) |
| OD-9 | D-48 numbering (§0.2) | amend D-48 / move Part A to LLD-03 | Amend D-48 with a "companion LLD" clause; keeps LLD-03 readable |
| OD-10 | Provisioning as an entitlement-gated module (LLD-11) | gated / core | **Core**, not gated |
| OD-11 | Vendor firmware redistribution | (a) operator uploads images; (b) we ship images in the release | **(a)** — redistribution rights differ per vendor and images are 30–90 MB each |

### 0.4 Dependencies

| Need | Satisfied by | Approval |
|---|---|---|
| Template rendering | Go `text/template` (stdlib) with a restricted FuncMap | none |
| Argon2id, X25519 box for bundle encryption | `golang.org/x/crypto` (already in `go.mod`) | none |
| AES-GCM sealing, Ed25519 media signatures, X.509 | stdlib | none |
| AMI `PJSIPNotify` | existing `internal/telephony/acl/ami` client, behind a pbx port | none |
| Synthetic SIP OPTIONS in the health gate | `github.com/emiago/sipgo` (already present) | none |
| Contract-phase migrations in a second table | `golang-migrate` `x-migrations-table` (already present) | none |
| S3-compatible offsite backup (OD-6 b) | `github.com/minio/minio-go/v7` **or** `aws-sdk-go-v2/service/s3` | **needs owner approval** |
| SFTP offsite backup (OD-6 c, R1.1) | `github.com/pkg/sftp` | **needs owner approval** |

---

# Part A — Desk-phone auto-provisioning (AD-07)

## A.1 Trigger, actors and permissions

| Flow | Trigger | PRD persona | Permission (scope) |
|---|---|---|---|
| A1 Register | `CreateDevice`, `ImportDevices` RPC | Partner engineer (PLATFORM_ADMIN) or tenant administrator | `device.create` (tenant) |
| A2 First boot | Phone HTTPS `GET` to the provisioning listener | The device — **no principal** | Device credential: factory client certificate (mTLS) or per-device Basic credential |
| A3 Resync / reboot | `ResyncDevice`, `RebootDevice` RPC; automatically after a config-affecting change | Administrator; system worker | `device.resync`, `device.reboot` (tenant); worker uses the system actor |
| A4 Firmware | `RegisterFirmware`/`DeleteFirmware` (installation); `SetFirmwarePin` (tenant) | PLATFORM_ADMIN; tenant administrator | `firmware.manage` (system scope, `RequireSystemScope`); `device.update` (tenant) |
| A5 Phonebook | Phone `GET` every `phonebook_refresh` | Device | Device credential |
| A6 Status | `ListDevices`, `GetDevice`; status sweeper timer | Administrator; system | `device.read` |
| A7 Revoke / delete / replace | `RevokeDevice`, `DeleteDevice`, `ReplaceDevice` | Administrator | `device.revoke`, `device.delete`, `device.update` |
| A8 Key templates, overrides | `CreateKeyTemplate` etc., `UpdateDevice.overrides` | Administrator | `device.update` |

`TENANT_ADMIN` and `PLATFORM_ADMIN` hold all `device.*` through the
wildcard; new permissions are added to the identity action catalogue in
the change that lands them. No employee persona may read a device's
credential or config.

## A.2 Preconditions

| Precondition | Checked by | When it fails |
|---|---|---|
| Installation provisioning enabled: `PROV_PUBLIC_HOST`, server certificate and `ATSAPBX_PROVISIONING_KEY` set | App start; `GetProvisioningSettings` | Listener not started; console "Phone provisioning isn't set up on this system. Your provider sets the provisioning address and certificate." RPCs that create devices return `FailedPrecondition` |
| Licence state not **Setup** (D-52) | `licensing` capacity port at `CreateDevice` | `FailedPrecondition` "Activate this system before adding phones." Degraded does **not** block (D-12) |
| Tenant active (not suspended) | `identity` tenant status, on RPC and on every fetch | RPC `FailedPrecondition` "This organisation is suspended."; fetch `404` |
| Model in the supported catalogue (§A8.3) | `domain.LookupModel` | `InvalidArgument` "This model isn't supported yet." with the list |
| Every bound extension exists, is type `SIP` (not `WEBRTC`), same tenant, not bound to another device | `BindLines` in the create/update tx | `NotFound` / `FailedPrecondition` (wording in §A.7) |
| MAC not present in any tenant | `device_macs` PK | `AlreadyExists` |
| For `auth_mode = mtls`: family supports factory certificates (catalogue flag) | `domain.ValidateAuthMode` | `InvalidArgument` "This phone can't prove its identity with a certificate; use a provisioning password." |
| Reboot / firmware: no active call on any of the device's lines (INV-03) | `CallActivity.ActiveOnExtensions` (telephony-core port) | Action **deferred**, not refused (§A.3.3) |
| Resync / reboot: a live registration exists | `ps_contacts` read | `FailedPrecondition` "This phone isn't registered, so it can't be told to update. It will pick up the change the next time it checks in." |

## A.3 Sequences

### A.3.1 A1 — register a phone

| # | Step | Context |
|---|---|---|
| 1 | Admin enters MAC (any of `AA:BB:..`, `aa-bb-..`, `aabb..`), model, label, line 1 extension, optional key template | console |
| 2 | `ProvisioningService.CreateDevice`; handler authorizes `device.create` | `pbx/provisioning/rpc` |
| 3 | `domain.ParseMAC` → canonical 12 lower-hex; `LookupModel`; `ValidateAuthMode`; `ValidateOverrides`; `PlanKeys` (all pure) | `pbx/provisioning/domain` |
| 4 | `BEGIN`; `SET LOCAL app.tenant_id`; `INSERT device_macs` (PK collision → AlreadyExists); `INSERT devices` (`INVENTORIED`), `device_lines`, `device_keys` | `pbx/provisioning/postgres` |
| 5 | For each bound line: LLD-03 `RotateSecretForDevice(tx, ext)` → new plaintext + HA1; HA1 projected to `ps_auths.md5_cred` in the same tx (LLD-03 §7.2); plaintext sealed into `device_line_secrets` (OD-2) | `pbx-core` (extension) + provisioning |
| 6 | If `auth_mode = basic`: `GeneratePassword` (crypto/rand, 32 chars base32 ≈ 160 bits) → Argon2id hash into `device_credentials` | provisioning |
| 7 | `INSERT audit_logs` (`device.created`), outbox `tenant.<id>.event.pbx.device.created`; `COMMIT` | same tx |
| 8 | Response: device, `provisioning_url`, **one-time** `provisioning_username` (= MAC) and `provisioning_password`, and the per-vendor redirect-service values to paste (OD-4) | rpc |

`ImportDevices` runs steps 3–7 for up to 500 rows in **one** transaction:
every row is validated first; any invalid row aborts the whole import and
the response lists each row's error. The one-time passwords come back as
a CSV in the response body for the RPS/ZTP/GDMS bulk upload, never
written to disk by the server.

### A.3.2 A2 — zero-touch first boot

How the phone learns the URL:

| Path | Phone receives | Credential delivered | Zero-touch (AC-03.5) |
|---|---|---|---|
| DHCP option 66 (Yealink, Grandstream) / 160 (Poly) | `https://<PROV_PUBLIC_HOST>/p/` | none — **mTLS required** | Yes, for mTLS-capable families |
| Vendor redirect service (RPS / ZTP / GDMS), configured by the partner (OD-4) | the same URL | per-device Basic username + password entered in the redirect record | Yes |
| Claim window (OD-3, off by default) | DHCP URL | bootstrap file sets the per-device credential | Yes, trust-on-first-use |
| Engineer types URL + credential in the phone's web UI | — | typed | **No** — allowed, but does not satisfy AC-03.5 and the console says so |

| # | Step (phone → server) | Context |
|---|---|---|
| 1 | TLS to `PROV_PUBLIC_HOST:443`. The edge proxy routes this hostname by SNI **passthrough** to the app's provisioning listener `:8443`, so the client certificate is verified in-process, never trusted from a proxy header. Min TLS 1.2; server requests but does not require a client certificate (`tls.VerifyClientCertIfGiven`) | edge, then `pbx/provisioning` |
| 2 | `GET /p/<vendor filename>`. `domain.ParseRequestPath` maps it: Yealink `y0000000000NN.cfg` (common) / `<mac>.cfg` / `<mac>-phonebook.xml`; Poly `000000000000.cfg` / `<mac>.cfg` / `<mac>-directory.xml`; Grandstream `cfg<mac>.xml` / `<mac>-phonebook.xml`; firmware `fw/<sha256>.<ext>`. Unrecognised → `404` | `pbx/provisioning/domain` (pure) |
| 3 | Rate limiter: per source IP token bucket; per-MAC failure lockout (§A.8) → `429` with `Retry-After` | `pbx/provisioning/application` |
| 4 | Common (MAC-less) files: served from the pack, **contain no secret and no tenant data**, `200` without authentication (phones request them before the MAC file and some log errors on `404`) | provisioning |
| 5 | `prov_resolve_mac(mac)` — a `SECURITY DEFINER` function over `device_macs` (the one cross-tenant read, like LLD-03's carrier-ingress match) → `(tenant_id, device_id)` or nothing → `404` with a constant body | postgres |
| 6 | Authenticate: `mtls` → `MACFromCert(family, leaf)` must equal the path MAC and the chain must verify against the family's vendor CA bundle; `basic` → Argon2id verify of `Authorization: Basic`. Missing → `401 WWW-Authenticate: Basic realm="atsapbx-provisioning"`; wrong → `401`, failure counter++ | provisioning |
| 7 | Device `REVOKED` or tenant suspended → `404` (indistinguishable from unknown); audit `device.fetch_refused` | provisioning |
| 8 | `BEGIN`; `SET LOCAL app.tenant_id`; load device, lines, keys, extensions, firmware pin, registrar settings (LLD-03 `GetSipRegistrar` source: `SIP_PUBLIC_HOST/PORT/TRANSPORT`), tenant phonebook URL; `Open` sealed line secrets | provisioning + `pbx-core` reads |
| 9 | `Render(pack, input)` — pure, deterministic → bytes, `sha256` | `pbx/provisioning/domain` |
| 10 | `UPDATE devices` (last fetch time/IP/UA, served hash and pack version, `Transition(..., EvConfigServed)`), `INSERT device_fetch_log`, outbox event if the state changed; `COMMIT` | postgres |
| 11 | `200`, `Cache-Control: no-store`, vendor content type; body never logged | provisioning |
| 12 | Phone applies, then `REGISTER`s to the engine over `SIP_PUBLIC_TRANSPORT` (TLS 5061 recommended) with the line's `AuthUsername` (= endpoint id, LLD-03 §7.1) and secret; Asterisk realtime writes `ps_contacts` | engine (D-47) |
| 13 | Status sweeper (every `status_sweep_interval`) reads `ps_contacts` (`endpoint`, `expiration_time`, `user_agent`, `via_addr`) for bound endpoints; `Transition(..., EvRegistered)`; `UAMac(user_agent)` ≠ device MAC → `ua_mac_mismatch = true` and audit `device.identity_mismatch` | provisioning |

The registrar the config points at is **exactly** what `GetSipRegistrar`
returns; when that is "not configured", `Render` returns
`ErrRegistrarNotConfigured`, the fetch answers `503` with `Retry-After: 300`,
and the dashboard shows "Phones can't be set up until the SIP address of
this system is configured."

### A.3.3 A3 — resync and reboot

ARI has no NOTIFY resource; the platform uses AMI, through a `pbx` port,
the only engine-control path in this LLD.

| # | Step | Context |
|---|---|---|
| 1 | `ResyncDevice` / `RebootDevice`, or the change debouncer (`auto_resync_on_change`, 30 s debounce) after any write that changes the rendered hash | provisioning |
| 2 | `INSERT device_actions` (`PENDING`, `not_before = now`, `expires_at = now + action_expiry`) | postgres |
| 3 | Action worker (every 10 s, `SELECT … FOR UPDATE SKIP LOCKED`): for `REBOOT`, `FIRMWARE` and families whose resync may reboot (Poly), ask `CallActivity.ActiveOnExtensions`; any active → `DEFERRED_BUSY`, `not_before = now + 60 s` | provisioning → `telephony-core` port |
| 4 | `NotifyEventFor(family, action)` (pure): Yealink resync `check-sync;reboot=false`, reboot `check-sync;reboot=true`; Poly `check-sync` (template sets `voIpProt.SIP.specialEvent.checkSync.alwaysReboot=0`); Grandstream `check-sync` (resync) and `reboot` (verify in spike S-12.2) | domain |
| 5 | AMI `Action: PJSIPNotify`, `Endpoint: e_<hex>`, `Variable: Event=<value>` — sent to every contact of that endpoint | `pbx/acl/asterisk` adapter over `telephony/acl/ami` |
| 6 | AMI `Response: Success` → `SENT`; the phone's subsequent fetch with a matching hash → `DONE`. Error → `attempts++`, retry with 30 s/2 min/10 min backoff, then `FAILED`. `expires_at` passed → `EXPIRED` | provisioning |
| 7 | Audit `device.resync_requested` / `device.reboot_requested` (requester), `device.action_failed` (worker) | postgres |

Templates set the phone-side preconditions for step 5: accept NOTIFY only
from the registrar (`sip.trust_ctrl=1`, `sip.notify_reboot_enable=1` on
Yealink; equivalents per family in the pack).

### A.3.4 A4 — firmware

| # | Step | Context |
|---|---|---|
| 1 | PLATFORM_ADMIN uploads an image through `POST /upload/firmware` (JWT, `firmware.manage`, max `firmware_max_bytes`), stored by SHA-256 in the blob store (`/var/lib/atsapbx/firmware` volume) | provisioning |
| 2 | `RegisterFirmware(upload_id, vendor, family, version, models[])` → `firmware_images` row (installation-scoped) | provisioning |
| 3 | Installation default pin per model (`SetDefaultFirmwarePin`) and optional tenant override (`SetFirmwarePin`) | provisioning |
| 4 | Effective pin ≠ phone's reported firmware (parsed from User-Agent) → render includes the firmware URL `https://<host>/p/fw/<sha256>.<ext>`; a `FIRMWARE` action is queued with `not_before` = next `firmware_window` start in the tenant time zone | provisioning |
| 5 | Inside the window, idle phones get `check-sync`; the phone downloads (same device authentication as A2), flashes and reboots | phone |
| 6 | Staged roll-out: at most `firmware_batch_size` devices per tenant per 10 min; if > 20 % of a batch fail to re-register within 15 min, the roll-out for that model **pauses** and the administrators are emailed | provisioning |

### A.3.5 A5 — phonebook

`GET /p/<mac>-phonebook.xml` (vendor-specific name), authenticated as A2.
Content: the tenant's extensions that are not `hidden_from_directory`
(display name of the assigned owner per D-65, else the extension's
display name, and number), rendered in the family's format (Yealink
`YealinkIPPhoneDirectory`, Poly `directory` XML, Grandstream
`AddressBook`). Generated per request (no cache, D-56: it is
authorization-scoped data). The phone's refresh interval is set by the
template to `phonebook_refresh`.

### A.3.6 A6 — status

The sweeper (step A2.13) and fetch handler maintain `devices`; the
dashboard reads only `devices` (one RLS query, no engine read per row —
this closes LLD-03 §9's "registration status read per page" cost for
phones). Columns shown: label, MAC, model, lines, state, last provisioned,
registered since / last seen, firmware (reported vs pinned), config
"up to date / out of date", identity warning.

### A.3.7 A7 — revoke, delete, replace

| Operation | Steps (one transaction) |
|---|---|
| `RevokeDevice` | state → `REVOKED`; delete `device_credentials`; delete `device_line_secrets`; **rotate each bound line's SIP secret** (LLD-03) so a config already on the phone stops working; unbind lines; cancel pending actions; audit `device.revoked` with reason; event |
| `DeleteDevice` | only from `INVENTORIED` or `REVOKED` (else `FailedPrecondition` "Revoke this phone first."); deletes `devices` (cascade) and `device_macs`; audit `device.deleted` |
| `ReplaceDevice(id, new_mac)` | RMA: old MAC row deleted from `device_macs`, new inserted; credential re-issued (one-time); lines, keys, overrides kept; state → `INVENTORIED`; SIP secrets rotated; audit `device.replaced` with both MACs |

**Known limitation:** rotating the SIP secret makes the next `REGISTER`
and every `INVITE` from the old phone fail at once (Asterisk challenges
both), but an existing contact in `ps_contacts` remains until its expiry
(at most the AOR's `maximum_expiration`), so an **inbound** call could
still ring the revoked phone for that period. Removing a contact needs
an engine action we have not verified on realtime contacts; spike S-12.3.

### A.3.8 A8 — template packs

Packs are embedded in the binary (`embed.FS`) and versioned with the
release; the pack version is `<family>@<semver>`, recorded on every
served config. Each pack holds: `catalogue.json` (models, key counts,
pages, expansion modules, auth modes, filename patterns, NOTIFY events,
vendor CA bundle), `device.tmpl`, `common.tmpl`, `phonebook.tmpl`, and
`managed_keys.json` (the parameters the platform owns and an override may
not touch: account credentials, registrar, transport, provisioning
server, trust settings, NOTIFY policy).

### A.3.9 Line keys, BLF and feature codes (LLD-03 §3.8, §3.13)

Presence and BLF are designed in LLD-03 §3.8: the platform computes
presence and publishes it as custom device state
`Custom:atsa-<extension UUID hex>`; desk-phone subscriptions reach it
through one static pattern-hint context `atsa-blf`, with
`ps_endpoints.subscribe_context = atsa-blf` (SPIKE-P1). This LLD only
renders the keys, so it follows that design exactly:

| Key `kind` | Subscription URI rendered (BLF lamp) | Number dialled on press | Pick-up on press while ringing |
|---|---|---|---|
| `blf` | `<target extension UUID hex>` — the **opaque** key, never the tenant-local number (LLD-03 §3.8, INV-10) | the target's extension number | directed pickup: tenant's `directed_pickup` code (default `**`, §3.13) + number |
| `park` | `atsa-park-<lot hex>-<slot>` (LLD-03 §3.10) | the slot number | — |
| `pickup` | none | group pickup code (default `*8`) | — |
| `dnd` | none (phone-local DND is disabled by the template so DND stays the platform's, LLD-03 §3.13 `*76`) | DND toggle code | — |
| `speed_dial` | none | `value` | — |
| `line` | — | — | — |

Feature codes are read from the tenant's effective set (`feature_codes`
rows over defaults) at render time, so a tenant that changes `**` gets
re-rendered phones (the change debouncer of §A.3.3 fires on a
`feature_codes` write). A disabled code renders the key as `none` with
its label kept, and `GetDevice` reports "Key 4: pickup is switched off
for this organisation".

**Dependency on SPIKE-P1.** Until SPIKE-P1 passes, `blf` keys render as
speed-dial keys with the same label (no lamp) and the dashboard shows
"Busy lamps arrive with a later update"; LLD-03 §3.8's stated fallback
(console-only presence) applies. No other part of Part A waits for it.

Hot-desking (LLD-03 §3.14) does **not** re-provision a phone: the phone
keeps its own line, and hot-desk login changes routing in `pbx-core`
only. A phone used as a hot desk is a normal device whose line 1 is the
hot-desk extension.

## A.4 State machines

### A.4.1 Device lifecycle

| State | Meaning |
|---|---|
| `INVENTORIED` | In the registry, credential issued; never fetched a per-device file |
| `PROVISIONING` | Authenticated, fetched a non-final file (bootstrap, firmware) but not yet its per-device config |
| `PROVISIONED` | Per-device config served; no live registration yet |
| `REGISTERED` | Line 1 has an unexpired contact in `ps_contacts` |
| `STALE` | Was `PROVISIONED`/`REGISTERED`; no live registration for ≥ `device_stale_after`, **or** no fetch for ≥ 3 × `device_resync_interval` |
| `REVOKED` | Terminal for this MAC: credential and sealed secrets destroyed; fetches refused |

| From | Event | Guard | To |
|---|---|---|---|
| `INVENTORIED` | `EvAuthFetchPartial` | auth ok | `PROVISIONING` |
| `INVENTORIED`, `PROVISIONING`, `STALE` | `EvConfigServed` | auth ok, render ok | `PROVISIONED` |
| `PROVISIONED`, `STALE` | `EvRegistered` | contact for line 1, expiry > now | `REGISTERED` |
| `REGISTERED` | `EvConfigServed` | — | `REGISTERED` (hash and time updated; not a regression) |
| `REGISTERED` | `EvUnregisteredFor(d)` | d ≥ `device_stale_after` | `STALE` |
| `PROVISIONED` | `EvUnregisteredFor(d)` | d ≥ `device_stale_after` since served | `STALE` |
| `PROVISIONED`, `REGISTERED` | `EvNoFetchFor(d)` | d ≥ 3 × resync interval | `STALE` |
| any except `REVOKED` | `EvRevoked` | `device.revoke` | `REVOKED` |
| any except `REVOKED` | `EvReplaced` | `device.update` | `INVENTORIED` |
| `REVOKED` | any fetch | — | `REVOKED` (refused, audited) |

`Transition(cur, ev, now) (State, error)` is pure; an event not in the
table returns `ErrInvalidTransition` and the caller logs it, never
panics. Two derived flags are not states: `config_out_of_date`
(`served_config_sha256 ≠` current render hash) and `firmware_mismatch`.

### A.4.2 Device action

| From | Trigger | Guard | To |
|---|---|---|---|
| — | request / debouncer | device not `REVOKED` | `PENDING` |
| `PENDING`, `DEFERRED_BUSY` | worker tick | `not_before ≤ now`; idle (reboot/firmware) | `SENT` |
| `PENDING`, `DEFERRED_BUSY` | worker tick | line on a call | `DEFERRED_BUSY` (+60 s) |
| `SENT` | fetch with expected hash / re-register after reboot | within 15 min | `DONE` |
| `PENDING`, `SENT` | AMI error | attempts < 3 | `PENDING` (backoff) |
| `PENDING`, `SENT` | AMI error | attempts = 3 | `FAILED` |
| any non-terminal | `expires_at` passed | — | `EXPIRED` |
| any non-terminal | device revoked / newer action of same kind | — | `CANCELLED` |

## A.5 Data model (migrations 0070–0073)

All tenant tables: `ENABLE` and `FORCE ROW LEVEL SECURITY`, policy
`USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)`
with the same `WITH CHECK`, and `GRANT SELECT, INSERT, UPDATE, DELETE … TO atsapbx_app`.
**No grant to `asterisk_engine`** on any of them (LLD-03 §5.3 stays true).

```sql
-- 0070_devices.up.sql  (phase: expand)
CREATE TABLE devices (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mac                  CHAR(12) NOT NULL CHECK (mac ~ '^[0-9a-f]{12}$'),
    vendor               TEXT NOT NULL CHECK (vendor IN ('yealink','poly','grandstream')),
    family               TEXT NOT NULL,           -- pack family, validated in Go
    model                TEXT NOT NULL,           -- catalogue model id
    label                VARCHAR(100) NOT NULL DEFAULT '',
    auth_mode            TEXT NOT NULL CHECK (auth_mode IN ('mtls','basic')),
    state                TEXT NOT NULL DEFAULT 'INVENTORIED'
                         CHECK (state IN ('INVENTORIED','PROVISIONING','PROVISIONED','REGISTERED','STALE','REVOKED')),
    state_since          TIMESTAMPTZ NOT NULL DEFAULT now(),
    key_template_id      UUID,
    overrides            JSONB NOT NULL DEFAULT '{}'::jsonb,
    pack_version         TEXT,                    -- NULL = release default
    served_config_sha256 BYTEA,
    served_pack_version  TEXT,
    last_fetch_at        TIMESTAMPTZ,
    last_fetch_ip        INET,
    last_registered_at   TIMESTAMPTZ,
    last_user_agent      VARCHAR(255),
    reported_firmware    VARCHAR(64),
    ua_mac_mismatch      BOOLEAN NOT NULL DEFAULT false,
    revoked_at           TIMESTAMPTZ,
    revoke_reason        VARCHAR(255),
    version              INTEGER NOT NULL DEFAULT 1,     -- optimistic concurrency
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, mac),
    UNIQUE (tenant_id, id)
);
CREATE INDEX devices_tenant_state_idx ON devices (tenant_id, state);

-- Installation-wide MAC uniqueness. NO tenant_id policy: a phone belongs to
-- one tenant on this installation. Read only through prov_resolve_mac().
CREATE TABLE device_macs (
    mac       CHAR(12) PRIMARY KEY CHECK (mac ~ '^[0-9a-f]{12}$'),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    device_id UUID NOT NULL
);
REVOKE ALL ON device_macs FROM PUBLIC;
GRANT INSERT, DELETE ON device_macs TO atsapbx_app;   -- no SELECT
CREATE FUNCTION prov_resolve_mac(p_mac CHAR(12))
  RETURNS TABLE (tenant_id UUID, device_id UUID)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS
  $$ SELECT tenant_id, device_id FROM device_macs WHERE mac = p_mac $$;
GRANT EXECUTE ON FUNCTION prov_resolve_mac(CHAR(12)) TO atsapbx_app;

CREATE TABLE device_lines (
    tenant_id    UUID NOT NULL,
    device_id    UUID NOT NULL,
    line_no      SMALLINT NOT NULL CHECK (line_no BETWEEN 1 AND 16),
    extension_id UUID NOT NULL REFERENCES extensions(id) ON DELETE RESTRICT,
    label        VARCHAR(32) NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, device_id, line_no),
    UNIQUE (tenant_id, extension_id),               -- an extension is on at most one phone
    FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE key_templates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    family     TEXT NOT NULL,
    keys       JSONB NOT NULL,                       -- [{page,position,kind,target,label}]
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE device_keys (
    tenant_id           UUID NOT NULL,
    device_id           UUID NOT NULL,
    page                SMALLINT NOT NULL CHECK (page BETWEEN 0 AND 9),   -- 0 = phone, 1..9 expansion modules
    position            SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 60),
    kind                TEXT NOT NULL CHECK (kind IN ('line','blf','speed_dial','park','pickup','dnd','none')),
    target_extension_id UUID REFERENCES extensions(id) ON DELETE SET NULL,
    value               VARCHAR(64) NOT NULL DEFAULT '',  -- speed-dial number, park slot
    label               VARCHAR(32) NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, device_id, page, position),
    FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE CASCADE
);
-- RLS + grants on devices, device_lines, key_templates, device_keys.
```

```sql
-- 0071_device_credentials.up.sql  (phase: expand)
CREATE TABLE device_credentials (
    tenant_id     UUID NOT NULL,
    device_id     UUID NOT NULL,
    username      CHAR(12) NOT NULL,                 -- = MAC; not a secret
    secret_hash   TEXT NOT NULL,                     -- $argon2id$v=19$m=19456,t=2,p=1$...
    issued_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    failed_count  SMALLINT NOT NULL DEFAULT 0,
    locked_until  TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, device_id),
    FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE CASCADE
);
-- OD-2: the only reversible SIP secret in the schema, and only for
-- extensions bound to a phone. AAD = tenant_id || extension_id.
CREATE TABLE device_line_secrets (
    tenant_id    UUID NOT NULL,
    extension_id UUID NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    key_id       VARCHAR(32) NOT NULL,              -- which ATSAPBX_PROVISIONING_KEY sealed it
    nonce        BYTEA NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext   BYTEA NOT NULL,
    sealed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, extension_id)
);
-- RLS + grants on both.
```

```sql
-- 0072_firmware.up.sql  (phase: expand)
-- Installation-scoped like licensing_state (0005): NO tenant_id, NO RLS.
CREATE TABLE firmware_images (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor      TEXT NOT NULL,
    family      TEXT NOT NULL,
    version     VARCHAR(64) NOT NULL,
    models      TEXT[] NOT NULL,
    filename    VARCHAR(255) NOT NULL,
    sha256      BYTEA NOT NULL UNIQUE CHECK (octet_length(sha256) = 32),
    size_bytes  BIGINT NOT NULL CHECK (size_bytes > 0),
    uploaded_by UUID NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (family, version)
);
CREATE TABLE firmware_default_pins (
    model       TEXT PRIMARY KEY,
    firmware_id UUID NOT NULL REFERENCES firmware_images(id) ON DELETE RESTRICT
);
GRANT SELECT, INSERT, UPDATE, DELETE ON firmware_images, firmware_default_pins TO atsapbx_app;

CREATE TABLE firmware_pins (                         -- tenant override, RLS
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    model       TEXT NOT NULL,
    firmware_id UUID NOT NULL REFERENCES firmware_images(id) ON DELETE RESTRICT,
    PRIMARY KEY (tenant_id, model)
);
```

```sql
-- 0073_device_activity.up.sql  (phase: expand)
CREATE TABLE device_actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    device_id    UUID NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('RESYNC','REBOOT','FIRMWARE')),
    status       TEXT NOT NULL DEFAULT 'PENDING'
                 CHECK (status IN ('PENDING','DEFERRED_BUSY','SENT','DONE','FAILED','EXPIRED','CANCELLED')),
    requested_by UUID NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    not_before   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    attempts     SMALLINT NOT NULL DEFAULT 0,
    last_error   VARCHAR(255),
    FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX device_actions_due_idx ON device_actions (not_before) WHERE status IN ('PENDING','DEFERRED_BUSY');
-- One live action of a kind per device; a newer request cancels the older in the same tx.
CREATE UNIQUE INDEX device_actions_one_live ON device_actions (tenant_id, device_id, kind)
    WHERE status IN ('PENDING','DEFERRED_BUSY','SENT');

CREATE TABLE device_fetch_log (                      -- retention 30 days, purged daily
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    device_id   UUID NOT NULL,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    file_kind   TEXT NOT NULL CHECK (file_kind IN ('config','common','phonebook','firmware','bootstrap')),
    result      TEXT NOT NULL CHECK (result IN ('served','auth_failed','locked','refused_revoked','render_failed','not_configured')),
    status_code SMALLINT NOT NULL,
    source_ip   INET,
    user_agent  VARCHAR(255),
    config_sha256 BYTEA
);
CREATE INDEX device_fetch_log_device_at_idx ON device_fetch_log (tenant_id, device_id, at DESC);
-- RLS + grants on both. The worker's cross-tenant scan of due actions uses the
-- existing BYPASSRLS outbox-worker role pattern, reading ids only, then sets
-- app.tenant_id per action.
```

Unknown-MAC requests are **not** logged per row (that would let a
scanner fill the table); they increment
`atsapbx_prov_requests_total{result="unknown_device"}`.

## A.6 Ports and domain functions

```go
package domain // api/internal/pbx/provisioning/domain — no I/O anywhere in this package

type MAC string           // 12 lower-case hex
type Family string        // "yealink-t4t5", "poly-ucs", "grandstream-grp"
type State string         // INVENTORIED … REVOKED (§A.4.1)

func ParseMAC(s string) (MAC, error)                                              // pure
func LookupModel(cat Catalogue, model string) (ModelSpec, error)                  // pure
func ValidateAuthMode(m ModelSpec, mode AuthMode) error                           // pure
func ValidateOverrides(p Pack, overrides map[string]string) error                 // pure: grammar + managed_keys denylist
func PlanKeys(m ModelSpec, tpl []KeySpec, lines []Line) ([]Key, error)            // pure: fills line keys first, rejects overflow
func ParseRequestPath(path string) (Request, error)                               // pure: vendor filename → {Family?, MAC?, Kind}
func MACFromCert(f Family, leaf *x509.Certificate) (MAC, error)                   // pure: per-family subject/SAN parse
func UAMac(userAgent string) (MAC, bool)                                          // pure
func FirmwareFromUA(f Family, userAgent string) (string, bool)                    // pure
func NotifyEventFor(f Family, a ActionKind) (string, error)                       // pure
func Transition(cur State, ev Event, now time.Time) (State, error)                // pure
func Render(p Pack, in RenderInput) (Rendered, error)                             // pure, deterministic: same input → same bytes/sha
func RenderPhonebook(p Pack, entries []DirectoryEntry) (Rendered, error)          // pure
func GeneratePassword(r io.Reader) (string, error)                                // deterministic given r
func Seal(k Key, aad, plaintext []byte, r io.Reader) (Sealed, error)              // AES-256-GCM
func Open(keys KeyRing, aad []byte, s Sealed) ([]byte, error)

type RenderInput struct {
    Device    Device
    Lines     []RenderLine      // AuthUsername, Secret (plaintext, never logged), DisplayName, Number
    Keys      []Key             // BLF targets resolved per §A.3.9: opaque key + dialled number
    FeatureCodes map[string]string // tenant's effective codes (LLD-03 §3.13), e.g. directed_pickup → "**"
    Registrar Registrar         // from LLD-03 GetSipRegistrar's source
    ProvURL   string            // https://<PROV_PUBLIC_HOST>/p/
    Firmware  *FirmwareRef      // nil = leave as is
    Phonebook string            // URL
    TimeZone  string            // tenant IANA tz
    Locale    string
    Overrides map[string]string // already validated
}
```

`Render` runs `text/template` with `Option("missingkey=error")` and a
FuncMap of exactly: `esc` (the family's escaper — `cfg` rejects CR/LF
and `#`, XML escapes `<>&"'`), `bool01`, `join`. Every interpolation
goes through `esc`; a pack test fails the build if a template contains
`{{.` without it. No template may call a function with side effects, so
rendering stays pure.

```go
package ports // api/internal/pbx/provisioning/ports

type DeviceStore interface {
    WithTenantTx(ctx context.Context, tenant shared.TenantID, fn func(Tx) error) error
    ResolveMAC(ctx context.Context, mac domain.MAC) (shared.TenantID, shared.ID, bool, error) // prov_resolve_mac
    DueActions(ctx context.Context, now time.Time, limit int) ([]ActionRef, error)           // ids only
}
type Tx interface {
    InsertDevice(d domain.Device, lines []domain.Line, keys []domain.Key) error // AlreadyExists on MAC
    LoadForRender(id shared.ID) (domain.RenderBundle, error)
    UpdateDevice(d domain.Device, expectedVersion int) error                    // ErrVersionConflict
    RecordFetch(f domain.FetchRecord) error
    PutCredential(id shared.ID, hash string) error
    PutLineSecret(ext shared.ExtensionID, s domain.Sealed) error
    DeleteSecrets(id shared.ID) error
    EnqueueAction(a domain.Action) error
    Audit(e shared.AuditEntry) error
    Outbox(e shared.Event) error
}
// Implemented by pbx-core's extension service (LLD-03), in the caller's tx.
type ExtensionSecrets interface {
    RotateForDevice(ctx context.Context, tx pgx.Tx, ext shared.ExtensionID) (plaintext string, err error)
}
// Implemented in pbx/acl/asterisk: reads ps_contacts.
type RegistrationReader interface {
    Contacts(ctx context.Context, endpointIDs []string) (map[string]domain.Contact, error)
}
// Implemented in pbx/acl/asterisk over telephony/acl/ami (AMI PJSIPNotify).
type EngineNotifier interface {
    Notify(ctx context.Context, endpointID, event string) error
}
// Implemented by telephony-core (allowed: pbx-core → telephony-core, HLD 04 §10.1).
type CallActivity interface {
    ActiveOnExtensions(ctx context.Context, tenant shared.TenantID, ids []shared.ExtensionID) (map[shared.ExtensionID]bool, error)
}
type FirmwareBlobs interface {
    Put(ctx context.Context, r io.Reader, max int64) (sha [32]byte, size int64, err error)
    Open(ctx context.Context, sha [32]byte) (io.ReadSeekCloser, error)
}
type Clock interface{ Now() time.Time }
```

Inbound: `ProvisioningService` (RPC, §A.7) and `provisioning.Server`, an
`http.Handler` mounted on the `:8443` TLS listener — not on the API mux,
so no JWT middleware and no CORS ever apply to it.

## A.7 API contract — `ProvisioningService` (`api/proto/atsapbx/v1/provisioning.proto`)

Consumers (D-43): the console Phones screen (A1, A3, A6, A7, A8), the
System → Firmware screen (A4), and the e2e harness that factory-resets a
simulated phone. `docs/API.md` is updated by each change that lands an RPC.

| RPC | Request → Response (key fields) | Permission |
|---|---|---|
| `CreateDevice` | `mac, model, label, auth_mode, lines[{line_no, extension_id, label}], key_template_id?, overrides{}` → `device, provisioning_url, provisioning_username?, provisioning_password?` (once) , `redirect_hints[]` | `device.create` |
| `ImportDevices` | `rows[{mac, model, label, extension_number, key_template_name?}]` (≤ 500) → `devices[], credentials_csv` (once) or `row_errors[{row, field, message}]` | `device.create` |
| `ListDevices` | `state?, model?, search?, page_size (≤ 200), page_token` → `devices[], next_page_token` | `device.read` |
| `GetDevice` | `id` → `device` (+ last 20 fetch-log rows, live action) | `device.read` |
| `UpdateDevice` | `id, version, label?, lines?, keys?, key_template_id?, overrides?, pack_version?` → `device` | `device.update` |
| `RotateDeviceCredential` | `id` → `provisioning_password` (once) | `device.update` |
| `ResyncDevice` / `RebootDevice` | `id` → `action{id, status: SENT \| DEFERRED_BUSY \| PENDING}` | `device.resync` / `device.reboot` |
| `RevokeDevice` | `id, reason (1–255)` → `device` | `device.revoke` |
| `DeleteDevice` | `id` → `{}` | `device.delete` |
| `ReplaceDevice` | `id, new_mac` → `device, provisioning_password?` | `device.update` |
| `ListKeyTemplates`, `CreateKeyTemplate`, `UpdateKeyTemplate`, `DeleteKeyTemplate` | `name, family, keys[]` | `device.read` / `device.update` |
| `ListSupportedModels` | → `models[{id, vendor, family, line_keys, pages, auth_modes[]}]` | `device.read` |
| `GetProvisioningSettings` | → `enabled, provisioning_url, dhcp_option_66, dhcp_option_160, redirect_values{yealink_rps, poly_ztp, grandstream_gdms}, claim_window_enabled` | `device.read` |
| `RegisterFirmware`, `ListFirmware`, `DeleteFirmware`, `SetDefaultFirmwarePin` | installation | `firmware.manage` (system scope) |
| `SetFirmwarePin` | `model, firmware_id \| null` | `device.update` |

| Refusal | Connect code | Console wording |
|---|---|---|
| MAC malformed | `InvalidArgument` | "That doesn't look like a MAC address. It's 12 characters, 0–9 and A–F, printed on the label under the phone." |
| MAC already in this tenant | `AlreadyExists` | "This phone is already in your list." |
| MAC in another tenant | `AlreadyExists` | "This phone is registered to another organisation on this system. Ask your provider to release it." |
| Unsupported model | `InvalidArgument` | "This model isn't supported yet." + supported list |
| mTLS on a family without factory certificates | `InvalidArgument` | "This phone can't prove its identity with a certificate; use a provisioning password." |
| Extension is WEBRTC | `FailedPrecondition` | "Browser extensions can't be put on a desk phone. Choose a SIP extension." |
| Extension on another phone | `FailedPrecondition` | "Extension 1001 is already on the phone \"Reception\"." |
| Extension not found | `NotFound` | "That extension no longer exists." |
| Override touches a managed key | `InvalidArgument` | "\"account.1.password\" is managed by AtsaPBX and can't be overridden." |
| Too many keys for the model | `InvalidArgument` | "This phone has 27 keys; the layout uses 30." |
| Stale version | `Aborted` | "Someone else changed this phone. Reload to see their changes." |
| Not registered (resync/reboot) | `FailedPrecondition` | "This phone isn't registered, so it can't be told to update. It will pick up the change the next time it checks in." |
| Reboot within `reboot_min_interval` | `ResourceExhausted` | "This phone was restarted a moment ago. Try again in a few minutes." |
| Delete a non-revoked device | `FailedPrecondition` | "Revoke this phone first." |
| Delete pinned firmware | `FailedPrecondition` | "This firmware is pinned for T54W. Unpin it first." |
| Provisioning not set up | `FailedPrecondition` | "Phone provisioning isn't set up on this system." |
| Setup licence state | `FailedPrecondition` | "Activate this system before adding phones." |
| Import > 500 rows | `InvalidArgument` | "Import up to 500 phones at a time." |
| Missing permission | `PermissionDenied` | "You don't have permission to manage phones." |

Deleting an extension that is on a phone: LLD-03 `DeleteExtension` gains
one refusal, `FailedPrecondition` "This extension is on the phone
\"Reception\". Remove it from the phone first." (FK `ON DELETE RESTRICT`).

**Provisioning HTTP responses:** `200` served; `401` missing/wrong
credential; `404` unknown, revoked, suspended or unrecognised path (one
constant body); `429` rate-limited or locked (`Retry-After`); `503`
registrar not configured (`Retry-After: 300`); `500` render failure
(logged with device id, never the body).

## A.8 Configuration and limits

| Setting | Default | Range | Scope |
|---|---|---|---|
| `PROV_PUBLIC_HOST` | unset (provisioning disabled) | hostname | installation |
| `PROV_LISTEN_ADDR` | `:8443` | addr | installation |
| `PROV_TLS_CERT_FILE` / `PROV_TLS_KEY_FILE` | unset → disabled | operator-supplied, never in the image | installation |
| `ATSAPBX_PROVISIONING_KEY` (+ `_ID`) | unset → disabled | 32 bytes base64; previous keys in `ATSAPBX_PROVISIONING_KEYS_OLD` for rotation | installation |
| TLS minimum | 1.2 | 1.2–1.3 (1.3 breaks older Poly firmware) | installation |
| Per-IP rate | 60 req/min, burst 20 | 10–600 | installation |
| Auth-failure lockout | 5 failures / 15 min → locked 15 min | 3–20; 5–120 min | installation |
| `status_sweep_interval` | 30 s | 10–300 s | installation |
| `device_stale_after` | 15 min | 5–1440 min | tenant |
| `device_resync_interval` (phone-side periodic check, set in config) | 1440 min | 60–10080 | tenant |
| `auto_resync_on_change` / debounce | true / 30 s | bool / 10–300 s | tenant |
| `action_expiry` | 24 h | 1–168 h | installation |
| Deferred-busy recheck | 60 s | fixed | — |
| `reboot_min_interval` | 5 min | 1–60 min | installation |
| `firmware_window` | 02:00–04:00 tenant time zone | any ≥ 30 min window | tenant |
| `firmware_batch_size` | 10 per 10 min | 1–100 | tenant |
| Firmware pause threshold | > 20 % of batch not re-registered in 15 min | 5–50 % | installation |
| `firmware_max_bytes` | 256 MiB | 16–512 MiB | installation |
| `phonebook_refresh` | 3600 s | 600–86400 | tenant |
| Fetch-log retention | 30 days | 7–90 | installation |
| Import batch | 500 rows | fixed | — |
| Devices per tenant (sanity cap) | 10,000 | fixed | — |
| Claim window (OD-3) | disabled; when enabled 30 min, CIDR required | 5–240 min | tenant |
| Default `auth_mode` | `mtls` where the catalogue allows, else `basic` | — | per device |

## A.9 Failure and recovery

| Failure | Behaviour | Recovery |
|---|---|---|
| Postgres down during a fetch | `503 Retry-After: 120`; nothing written | Phone retries on its own schedule |
| Provisioning key missing/wrong at start | Listener not started; `/readyz` unaffected (provisioning is not a call path); console banner | Operator sets the key |
| A sealed secret cannot be opened (key rotated away) | Render fails for that device only: `500`, `device_fetch_log.result = render_failed`, dashboard "Needs attention: re-issue SIP password" | `UpdateDevice` re-binds → rotate + reseal |
| Render error (pack bug) | `500`, metric, device keeps its previous config (phones keep the last good config on a failed fetch) | Release fix; resync |
| Response lost after commit | `served_config_sha256` updated but phone has old config → stays `config_out_of_date` until next fetch; no credential changed by a fetch, so nothing is locked out | Periodic resync or manual |
| Engine (AMI) down | Actions retry 3×, then `FAILED`; status sweeper keeps last known state and marks the dashboard "Status unavailable since …" | AMI reconnect loop (existing) |
| App restart | Actions are durable rows; `SENT` actions older than 15 min are re-evaluated; no in-memory queue | — |
| Concurrent `UpdateDevice` | `version` guard → `Aborted` | Reload |
| Concurrent add of one MAC in two tenants | `device_macs` PK — exactly one wins | — |
| Reboot requested during a call | `DEFERRED_BUSY`, rechecked each minute until `expires_at` | — |
| Phone reboots itself mid-call because firmware arrived outside window | Cannot happen through us: firmware URL is only rendered inside the window | — |
| Firmware roll-out failures | Pause at threshold, email administrators | Admin unpins or resumes |
| Extension deleted while bound | Refused (FK RESTRICT) | Unbind first |
| Phone factory-reset | Fetches again → `PROVISIONED` → `REGISTERED`; no admin action (AC-03.5) | — |

## A.10 Invariants, events and audit

| Rule | How it holds |
|---|---|
| AC-03.5 encrypted channel | HTTPS only; no HTTP/TFTP listener exists |
| AC-03.5 unusable by any other device | Per-device credential or factory certificate bound to the MAC in the path; per-extension unique SIP secret; an extension on at most one phone (`UNIQUE (tenant_id, extension_id)`); UA-MAC mismatch flagged |
| No secret in a URL | Paths carry only the MAC (not secret) and firmware SHA-256; credentials only in `Authorization` or the TLS client certificate; access logs record path, never headers |
| Tenant isolation (D-24, D-55) | All tables RLS; the single cross-tenant read is `prov_resolve_mac` returning two ids |
| Engine least privilege (LLD-03 §5.3) | No new grant to `asterisk_engine` |
| INV-03 | Reboot and firmware wait for idle; resync on families that may reboot waits for idle |
| INV-01 | Unaffected: provisioning never changes routing or emergency numbers |
| D-56 | No cache of credentials, configs or phonebooks |
| Secrets in logs (D-39) | Rendered bodies, `Authorization` and sealed values are never logged; a test greps the logs |

Events (outbox, subject `tenant.<id>.event.pbx.device.<action>`; payload
`{device_id, tenant_id, mac, model, state, occurred_at}` plus the field
named):

| `<action>` | Extra field |
|---|---|
| `created`, `updated`, `deleted`, `replaced` | `changed_fields[]` / `old_mac` |
| `provisioned` | `config_sha256`, `pack_version` |
| `registered`, `stale` | `last_registered_at` |
| `revoked` | `reason` |
| `action_requested`, `action_completed` | `kind`, `status` |
| `identity_mismatch` | `user_agent_mac` |

Audit actions (`audit_logs`, resource `device`): `device.created`,
`device.updated`, `device.credential_rotated`, `device.resync_requested`,
`device.reboot_requested`, `device.revoked`, `device.deleted`,
`device.replaced`, `device.fetch_refused`, `device.identity_mismatch`,
`device.action_failed`, `firmware.registered`, `firmware.deleted`,
`firmware.pinned`. Before/after states never contain a credential.

## A.11 Test scenarios

Tiers: **U** unit · **I** integration (real Postgres, `atsapbx_test`) ·
**W** wire (RPC/HTTP through the real handler) · **E** e2e (running app,
live Asterisk) · **C** console component · **B** browser e2e · **A** UAT
(real phone or baresip).

| # | Given / When / Then | Tier |
|---|---|---|
| A-T1 | Given `"AA:BB:CC:00:11:22"`, `"aabb.cc00.1122"`, `"AABBCC001122"` / When `ParseMAC` / Then all yield `aabbcc001122`; `"aabbcc00112"` and `"gg…"` fail | U |
| A-T2 | Given the same `RenderInput` twice / When `Render` / Then bytes and SHA-256 are identical | U |
| A-T3 | Given a display name `Bob\naccount.1.password=x` / When rendering a Yealink cfg / Then render fails (CR/LF refused), never emits a second key | U |
| A-T4 | Given every template in every pack / When the pack lint runs / Then no interpolation lacks `esc` | U |
| A-T5 | Given overrides `{"account.1.password":"x"}` / When `ValidateOverrides` / Then `InvalidArgument` naming the key | U |
| A-T6 | Every row of §A.4.1 and §A.4.2, and one invalid event per state / When `Transition` / Then table result or `ErrInvalidTransition` | U |
| A-T7 | Given vendor sample certificates (fixtures) / When `MACFromCert` / Then the MAC is extracted; a cert for another MAC fails | U |
| A-T8 | Given each family's request filenames / When `ParseRequestPath` / Then kind and MAC as §A.3.2; traversal (`../`), `%2f`, upper-case variants handled | U |
| A-T9 | Given tenant A has MAC M / When tenant B creates M / Then `AlreadyExists` with the "another organisation" wording and no row in B | I |
| A-T10 | Given a created device / When reading `device_credentials` and `device_line_secrets` / Then no plaintext; `ps_auths.md5_cred` equals HA1 of the opened secret | I |
| A-T11 | Given tenant A context / When selecting devices, lines, secrets of tenant B / Then zero rows; `SELECT` on `device_macs` as `atsapbx_app` is denied | I |
| A-T12 | Given `asterisk_engine` / When selecting any 0070–0073 table / Then permission denied | I |
| A-T13 | Given an import of 3 rows, one with a WEBRTC extension / When `ImportDevices` / Then nothing is created and `row_errors` names row 2 | W |
| A-T14 | Given a basic-auth device / When `GET /p/<mac>.cfg` without credentials / Then `401`; with the right ones `200`, `Cache-Control: no-store`, state `PROVISIONED`, event emitted | W |
| A-T15 | Given 5 wrong passwords in 15 min / When the 6th (correct) request / Then `429`; after 15 min `200` | W |
| A-T16 | Given an unknown MAC, a revoked device, and a suspended tenant / When fetched / Then three byte-identical `404` bodies | W |
| A-T17 | Given an mTLS device / When fetched with a valid vendor cert for a **different** MAC / Then `401`, `device.fetch_refused` audited | W |
| A-T18 | Given the registrar is not configured / When fetched / Then `503 Retry-After: 300`, dashboard shows the configuration message | W |
| A-T19 | Given a device whose line is on an active call / When `RebootDevice` / Then `DEFERRED_BUSY`, no NOTIFY sent; after hang-up within a minute the NOTIFY is sent | E |
| A-T20 | Given a registered baresip "phone" / When `ResyncDevice` / Then Asterisk sends NOTIFY `Event: check-sync;reboot=false` to its contact (captured) | E |
| A-T21 | Given a provisioned simulated phone (e2e harness: fetch over mTLS, then REGISTER with the rendered credentials) / When it registers / Then state `REGISTERED` within `status_sweep_interval` + 5 s | E |
| A-T22 | Given a registered device / When `RevokeDevice` / Then the next REGISTER and INVITE with the old secret get `401`, and fetch gets `404` | E |
| A-T23 | Given a phone unplugged / When `device_stale_after` passes / Then state `STALE`, event emitted; re-plugging returns it to `REGISTERED` | E |
| A-T24 | Given a firmware pin outside the window / When rendering / Then no firmware URL; inside the window the URL is present and a `FIRMWARE` action exists | U, I |
| A-T25 | Given a batch where 3 of 10 phones fail to re-register / When 15 min pass / Then roll-out paused and administrators emailed | I |
| A-T26 | Phones screen: create form validates MAC as typed; one-time password shown once with copy; status chips for each state | C |
| A-T27 | An admin adds a phone, copies the DHCP value, and sees state move to Registered (simulated phone) | B |
| A-T28 | A factory-reset Yealink T54W and a Poly VVX 450 on the rig LAN with DHCP 66/160 become usable and place a call with no keypad input (AC-03.5) | A |
| A-T30 | Given a `blf` key on extension X / When rendering for any family / Then the subscription value is X's UUID hex, the dial value is X's number, and the pickup value uses the tenant's `directed_pickup` code | U |
| A-T31 | Given a tenant changes `directed_pickup` to `*9` / When the change commits / Then every phone with a `blf` key gets a resync action and the next render uses `*9` | I |
| A-T32 | Given SPIKE-P1 passed and two phones provisioned / When X's phone rings and is answered / Then Y's BLF lamp for X shows ringing then busy within 3 s (AC-03.3) | A |
| A-T29 | Logs of a full A-T14/A-T28 run contain no SIP secret, no provisioning password and no rendered body | E |

## A.12 Industry parity

| Capability | 3CX | VitalPBX EndPoint Manager | FreePBX EPM | FusionPBX | AtsaPBX (this LLD) |
|---|---|---|---|---|---|
| Add by MAC, model templates | Yes | Yes (editable per model) | Yes | Yes (device vendors) | Yes; packs versioned with the release |
| Config transport | HTTPS; HTTP on LAN | HTTP/HTTPS/TFTP | HTTP(S)/TFTP/FTP | HTTP(S)/TFTP | **HTTPS only** (AC-03.5) |
| Fetch authentication | Random folder path in the URL (a secret in the URL) | Global provisioning user/password | Basic auth, IP allowlist | Per-domain HTTP auth + CIDR | **Per-device** credential or factory certificate; no secret in URL |
| Redirect services | Yealink/Fanvil/Snom RPS automated with 3CX's accounts | RPS/GDMS integrations | Yealink/Sangoma RPS | Manual | Partner's own accounts, CSV assist (OD-4); API R1.1 |
| LAN plug-and-play | SIP multicast PnP | No | No | No | Not R1.0 (unauthenticated) |
| SIP passwords at rest | Readable in DB | Readable | Readable | Readable | Sealed, device-bound lines only (OD-2) |
| Firmware | Curated updates pushed by 3CX | Upload + per-model assign | Firmware slots | Manual | Operator upload, pin, window, staged batches, auto-pause |
| BLF / line keys | Yes, from hints | Yes, from hints | Yes, from hints | Yes (device keys) | Yes; key templates; lamps from platform-computed presence via custom device state and one pattern hint (LLD-03 §3.8); keys carry an opaque key, not the number |
| Phonebook | Yes | Yes | Yes | Yes | Yes, per request, RLS-scoped |
| Reboot/resync | NOTIFY | NOTIFY | NOTIFY | NOTIFY via event socket | NOTIFY via AMI, **deferred while on a call** (INV-03) |
| Status dashboard | Yes | Yes | Basic | Basic | States + config/firmware drift + identity mismatch |

Deliberate differences: no TFTP/HTTP, no secret in URL, no vendor-level
redirect accounts, call-aware reboots, and an engine that never gets a
new grant.

---

# Part B — Install, upgrade, rollback, diagnostics, backup (AD-09)

## B.1 Trigger, actors and permissions

| Flow | Trigger | PRD persona | Authority |
|---|---|---|---|
| B1 Install | `atsapbx-ctl install` on the host | Partner engineer | Host root (sudo); creates the first PLATFORM_ADMIN (existing bootstrap) |
| B2 Upgrade | `atsapbx-ctl upgrade <media>` | Partner engineer | Host root; OD-5: not from the console |
| B3 Rollback | `atsapbx-ctl rollback` (manual) or automatic on a failed health gate | Partner engineer / ctl | Host root |
| B4 Diagnostic bundle | `atsapbx-ctl support-bundle` **or** `InstallationService.CreateDiagnosticBundle` | Partner engineer | Host root / `installation.diagnostics` (system scope); PII opt-in additionally `installation.diagnostics.pii` |
| B5 Backup / restore | Schedule timer in the app, `RunBackupNow` RPC, `atsapbx-ctl backup`; restore via `atsapbx-ctl restore` only | Partner engineer | `installation.backup` (system scope) for schedule/run/list; restore: host root |
| Status | `InstallationService.GetInstallationStatus` | PLATFORM_ADMIN | `installation.read` (system scope) |

The ctl talks to the app over a Unix socket `/run/atsapbx/admin.sock`
(mode 0660, group `atsapbx`), never over the network, for: `drain start|status|cancel`,
`health`, `bundle collect`. When the app is down, ctl does what it can
directly (logs, DB via the `atsapbx_ops` role) and says what it skipped.

## B.2 Preconditions

| Flow | Precondition | When it fails |
|---|---|---|
| B1 | Pre-flight passes (§B.3.1); media signature verifies; no existing installation in `/etc/atsapbx` | Stop before any change; print each failing check |
| B2 | Installed version is in the target's `supported_from` (§B.8.2); previous upgrade **committed** (else ctl offers to commit); pre-flight passes; snapshot succeeds; no other lifecycle operation running (lock file + DB row) | Refuse; nothing changed |
| B3 | Current upgrade is uncommitted; previous images still present locally (retained until commit) | Refuse with the restore procedure (§B.3.5) |
| B4 | Disk free ≥ 2 × `bundle_max_bytes` in `/var/lib/atsapbx/bundles` | Refuse "Not enough disk space for a bundle." |
| B5 backup | Target reachable and writable; passphrase present in the secret store | Run `FAILED`, alert |
| B5 restore | Backup version == installed version (else "Install 1.4.2 first, restore, then upgrade"); passphrase; drain completes | Refuse; nothing changed |

## B.3 Sequences

### B.3.1 B1 — install from media, pre-flight first

**Media.** `atsapbx-<version>-linux-amd64.tar` containing `atsapbx-ctl`,
`images/*.tar` (`docker save` of api, console, asterisk (D-86 runtime),
postgres 16, nats, coturn), `compose/docker-compose.yml`,
`MANIFEST.json` (version, image digests, SHA-256 of every file,
`supported_from[]`, `schema_version`, `contract_version`), `MANIFEST.sig`
(Ed25519, release key), `UPGRADE.md`, `ROLLBACK.md`, `SBOM.spdx.json`.
An online variant pulls the same digests from the registry. **A media
without `UPGRADE.md` and `ROLLBACK.md` fails the release pipeline**
(AC-09.6), and ctl refuses one too.

Trust: a first install verifies the tarball's SHA-256 published on the
release page and `MANIFEST.sig` against the key printed in the install
guide; an upgrade verifies the new manifest with the release key pinned
in the **installed** ctl, so trust is carried forward, not re-asserted.

| # | Step | Budget |
|---|---|---|
| 1 | `atsapbx-ctl preflight` (also runnable alone, US-09.6) — table below | 1 min |
| 2 | Verify `MANIFEST.sig` and every file hash | 1 min |
| 3 | Interactive or `--answers file`: public host, SIP transport/port, TLS cert/key paths, provisioning host, admin e-mail, time zone | 5 min |
| 4 | Generate secrets (DB passwords for `atsapbx_app`, `asterisk_engine`, `atsapbx_ops`; JWT key; `ATSAPBX_PROVISIONING_KEY`; backup passphrase) into `/etc/atsapbx/secrets/` (0600 root); print the backup passphrase **once** with "store it outside this server" | 1 min |
| 5 | `docker load` images; write compose and env | 3 min |
| 6 | Start Postgres, NATS; run expand **and** contract migrations (a fresh install is committed by definition) | 2 min |
| 7 | Bootstrap tenant + PLATFORM_ADMIN (existing `bootstrap`), start Asterisk, app, console | 2 min |
| 8 | Health gate (§B.3.2 step 8) | 2 min |
| 9 | Print console URL, instance ID and fingerprint (licence state **Setup**, D-52), and the next step: register and apply a token | — |
| | **Machine time** | **≈ 17 min**, leaving the AC-09.1 60-minute budget for reading and DNS/cert work |

| Pre-flight check | Threshold | Level |
|---|---|---|
| OS | Debian 12, Ubuntu 22.04/24.04, RHEL 9 (and rebuilds) | FAIL otherwise |
| Arch | x86_64 | FAIL |
| Kernel | ≥ 5.15 | FAIL |
| CPU / RAM / disk free | ≥ 4 vCPU / ≥ 8 GiB / ≥ 50 GiB on `/var/lib` | FAIL; WARN under 2× |
| Docker / Compose | Engine ≥ 24, Compose plugin v2 | FAIL |
| Ports free | TCP 443, 5061 (and 5060 if UDP/TCP SIP chosen), 8443 (internal), UDP 10000–20000 | FAIL |
| `net.core.rmem_max` / `wmem_max` | ≥ 16777216 | WARN (ctl offers to set) |
| `ulimit -n` | ≥ 65536 | WARN (ctl sets in the unit) |
| Time sync | NTP synchronised, skew < 1 s | FAIL (TLS, JWT and licences depend on it) |
| DNS | public host and provisioning host resolve | WARN |
| TLS certificate | readable, matches the hosts, ≥ 14 days validity | FAIL if mismatched; WARN < 30 days |
| Upgrade only: free disk | ≥ 2 × DB size + 2 × image size + 5 GiB | FAIL |
| Upgrade only: last successful backup | < 24 h | WARN |

### B.3.2 B2 — upgrade

| # | Step | Owner |
|---|---|---|
| 1 | Acquire lock (`/run/atsapbx/lifecycle.lock` + `lifecycle_operations` row `UPGRADE`, state `PREFLIGHT`) | ctl |
| 2 | Verify media; check `supported_from`; pre-flight | ctl |
| 3 | `SNAPSHOT`: `pg_dump -Fc` of the whole database to `/var/lib/atsapbx/snapshots/pre-<from>-<to>.dump` (+ SHA-256); copy `/etc/atsapbx`; record snapshot id | ctl |
| 4 | `STAGING`: `docker load` new images; previous images tagged `atsapbx/*:rollback` and retained | ctl |
| 5 | `EXPANDING`: golang-migrate `up` on the **expand** source (`schema_migrations`). Old app keeps serving; the CI gate guarantees it runs on the expanded schema (HLD-06 §2.1 rule 3) | ctl, `atsapbx_ops` role |
| 6 | `DRAINING`: `drain start` over the admin socket. The app's `MaintenanceGate` refuses new non-emergency call setups (inbound: SIP 503 with Q.850 cause 41; `PlaceCall`: `Unavailable` "The system is being updated. Try again in a few minutes."); **emergency calls are always admitted (INV-01)** and extend the drain; registrations, the console and provisioning stay up. ctl polls `drain status` (active calls from `telephony-core`) every 5 s, printing the count and the longest call | app (gate in call admission) + ctl |
| 7 | Drain reaches 0 → `SWITCHING`: stop old app, (if the Asterisk digest changed) stop Asterisk, start new images. If `drain_timeout` passes first → `drain cancel`, state `DRAIN_TIMEOUT`, **old version keeps serving** on the expanded schema; ctl lists the remaining calls. An operator may end a specific call with `HangupCall` (a deliberate, audited action on one call); ctl **never** ends calls | ctl |
| 8 | `VERIFYING` (≤ `health_gate_timeout`): `/readyz` all green; `schema_migrations` = manifest; ARI `GET /asterisk/info` reports the expected Asterisk version; `pbx reconcile` dry run reports 0 drift; SIP `OPTIONS` to the public registrar answers `200` (sipgo); provisioning listener answers `404` to a probe MAC | ctl |
| 9 | Pass → `SERVING_UNCOMMITTED`; `rollback_available_until = now + commit_after`. Fail → automatic **rollback** (§B.3.3) | ctl |
| 10 | `COMMITTING` (manual `atsapbx-ctl commit`, or the app's timer at `commit_after`): apply the **contract** source (`schema_migrations_contract`), delete `:rollback` images and snapshots older than the last 2 → `COMMITTED` | ctl / app timer via admin socket |

**Drain mode `adopt` (later).** Once FL-04 channel re-adoption ships
(HLD-09 §4/§6 note), an upgrade that changes only the app image may skip
step 6: the new app re-adopts live channels. Until that is proven in E,
**every** upgrade drains. An Asterisk image change always drains (HLD-09
F-06: an engine restart ends its calls).

### B.3.3 B3 — rollback

| # | Step | Budget (AC-09.4: 30 min) |
|---|---|---|
| 1 | `atsapbx-ctl rollback` (or step B2.9 failure) → state `ROLLING_BACK` | clock starts |
| 2 | Drain the new version (`rollback_drain_timeout`); same rules as B2.6. Skipped when the new app never became healthy (it carries no calls) | ≤ 10 min |
| 3 | Stop new app (and Asterisk if changed); start `:rollback` images | ≤ 2 min |
| 4 | Schema: **nothing** — the expand-only rule means the previous version runs on the current schema. Rows written by the new version in new tables or columns stay (no data loss) and are ignored | 0 |
| 5 | Health gate (B2.8 against the previous manifest) | ≤ 5 min |
| 6 | `ROLLED_BACK`; e-mail operators; the new version may be re-attempted | — |
| | **Total** | **≤ 17 min**, 13 min margin |

**When a migration itself failed (step B2.5):** Postgres DDL is
transactional per file, so the failed file rolled back; ctl reads the
dirty version, verifies the file left nothing (and drops any `INVALID`
index from a failed `CREATE INDEX CONCURRENTLY`, which must be alone in
its file and `IF NOT EXISTS`), then `migrate force <previous>`. The old
app never stopped. No restore.

### B.3.4 Migration policy (expand / contract)

| Rule | Enforcement |
|---|---|
| Every migration file states `-- phase: expand` or `-- phase: contract` on line 1 | CI lint |
| Expand files only add: tables, nullable columns or columns with defaults, indexes (`CONCURRENTLY`, own file), functions, policies, grants. Never drop, rename, narrow a type, add `NOT NULL` without default, or change a policy's meaning | CI lint (statement allowlist) + review |
| Contract files live in `api/migrations/contract/`, tracked in `schema_migrations_contract`, run only at commit | ctl |
| A column rename is expand (add + dual-write) in N, read-switch in N+1, drop in N+1's contract | Review |
| Backward-compatibility gate: the previous release's integration suite runs against the new expanded schema | CI job `schema-compat` |
| Upgrade matrix: every `supported_from` version → target, with data seeded, 50 live calls held across the upgrade | CI job `upgrade-matrix` (AC-09.3) |
| Down migrations are dev-only and never used by ctl | — |

Migrations 0070–0074 in this LLD are all `expand`.

### B.3.5 Restore after commit (outside AC-09.4)

After commit, returning to the previous version means
`atsapbx-ctl restore <pre-upgrade snapshot>` on the previous version's
media: drain, stop, restore the dump, start, health gate. Data written
since the snapshot is lost; ctl states the snapshot time and asks for
the typed confirmation `RESTORE <date>`. The console shows "Rollback to
1.3.0 is no longer available: this upgrade was committed on 14 Nov."

### B.3.6 B4 — diagnostic bundle

| # | Step |
|---|---|
| 1 | RPC `CreateDiagnosticBundle{since_hours=48, include_pii=false, pii_reason?, encrypt_for_support=true}` or ctl equivalent → `lifecycle_operations` row `BUNDLE` |
| 2 | Collector gathers the manifest items below into a temp dir (app side via admin socket; host side by ctl) |
| 3 | Each text item streams through `Redact(rules, salt)` (pure); salt = 32 random bytes per bundle, **never** written into the bundle |
| 4 | Any item that fails to parse, times out (30 s), or exceeds its size cap is **excluded** and listed with the reason (fail closed) |
| 5 | `manifest.json`: version, instance ID, created_at, ruleset `redaction-v1`, per-file SHA-256, per-rule redaction counts, opt-ins, exclusions |
| 6 | `tar.gz`; if `encrypt_for_support`, sealed with `x/crypto/nacl/box` to the vendor support public key embedded in the release (default on; forced on when `include_pii`) |
| 7 | Stored in `/var/lib/atsapbx/bundles/` for `bundle_retention`; console offers a single-use download link valid 15 min |
| 8 | Audit `installation.diagnostics.created` (and `…pii_included` with the reason) |

| Item | Source | Treatment |
|---|---|---|
| `manifest.json` | generated | — |
| `host/system.txt` | os-release, kernel, CPU, memory, disk, uptime, NTP state | none needed |
| `host/docker.txt` | versions, `compose ps`, image digests, restart counts | none |
| `config/effective.txt` | app configuration | **allowlist**: only keys in `bundle_config_allowlist` printed with values; every other key name-only `=[REDACTED]` |
| `logs/app.jsonl`, `logs/console.log` | last `since_hours` | rules R1–R10 |
| `logs/asterisk.log` | last `since_hours` | R1–R10; SDP bodies and `Authorization` headers removed |
| `engine/pjsip.txt` | `pjsip show endpoints/contacts` (counts and states) | contact IPs R8; endpoint ids are opaque (LLD-03 §7.1) |
| `engine/channels.txt` | `core show channels concise` | caller ids R6 |
| `db/schema.txt` | migration versions (both tables), dirty flag | none |
| `db/stats.txt` | per-table row counts and sizes, pool stats | counts only |
| `licensing/status.json` | edition, state, expiry, instance ID, fingerprint **match count** (not attributes) | none |
| `health/readyz.json`, `metrics/prom.txt` | snapshot | label values R6/R8 |
| `lifecycle/operations.json` | last 20 operations | none |
| `pbx/reconcile.txt` | `pbx reconcile` dry run | counts and opaque ids |
| `provisioning/summary.json` | devices by state/model/firmware, fetch failures by result | MACs pseudonymised |

**Never included, opt-in or not:** recordings, voicemail, prompts, any
audio (OD-8); database dumps or rows of domain tables; `ps_auths`;
private keys; `/etc/atsapbx/secrets`; backups; NATS payloads; the raw env file.

| Rule | Matches | Replacement | Disabled by `include_pii`? |
|---|---|---|---|
| R1 | `key=value` / `"key":"value"` where the key matches `password|passwd|secret|token|api_key|authorization|md5_cred|private_key|passphrase` (case-insensitive) | `[REDACTED]` | No |
| R2 | JWT shape `eyJ…\.…\.…` | `[REDACTED]` | No |
| R3 | PEM blocks | `[REDACTED]` | No |
| R4 | SIP `Authorization` / `Proxy-Authorization` / `WWW-Authenticate` lines | `[REDACTED]` | No |
| R5 | Basic/Bearer HTTP credentials in URLs or headers | `[REDACTED]` | No |
| R6 | Phone numbers (7–15 digits, optional `+`) in caller/callee fields, SIP `From`/`To`/`P-Asserted-Identity`/`Contact` user parts, D-39 fields `caller`, `callee`, `number` | `num#<HMAC-SHA256(salt)[:8]>` | Yes |
| R7 | E-mail addresses | `email#<…>` | Yes |
| R8 | Public IPv4/IPv6 not in a configured carrier range (carrier addresses are business data support needs) | `ip#<…>`; RFC 1918/ULA/loopback kept | Yes |
| R9 | SIP display names (`"Name" <sip:`) and D-39 `display_name`, `principal_name` | `name#<…>` | Yes |
| R10 | MAC addresses in provisioning items | `mac#<…>` | Yes |

The same value maps to the same token inside one bundle, so support can
follow a call through the logs without seeing who it was.

### B.3.7 B5 — backup and restore

| # | Backup step | Owner |
|---|---|---|
| 1 | Scheduler in the app (cron spec, installation time zone) or `RunBackupNow` → `lifecycle_operations` row `BACKUP`, state `RUNNING`. The app runs `pg_dump` itself against Postgres (16 client tools in the API image, U-12.4) and writes to the mounted backup volume; no container control is needed, so no Docker socket (OD-5) | app |
| 2 | Contents: `db.dump` (`pg_dump -Fc`, whole DB incl. `ps_*` and `licensing_state`), `etc/` (compose, env, secrets, TLS material), `media/` (recordings, voicemail, prompts, firmware) when stored on the local volume — when on S3, a manifest of object keys and a note that bucket versioning/replication is the media backup | app |
| 3 | Stream through AES-256-GCM (chunked, 64 KiB frames, frame counter in the nonce) under a key from Argon2id(passphrase, salt; m = 64 MiB, t = 3, p = 4) | app |
| 4 | Write `atsapbx-<instance>-<version>-<UTC timestamp>.atsbak` + `.sha256` to the local target; then copy to the offsite target (OD-6) | app |
| 5 | `VERIFYING`: re-read and hash; weekly (`verify_restore`) also `pg_restore` the dump into a scratch database `atsapbx_restore_verify`, check table counts against the dump's TOC, drop it | app |
| 6 | `SUCCEEDED`; prune per retention (local and offsite, independently); audit `installation.backup.completed` | app |
| 7 | `FAILED` → e-mail `ATSAPBX_OPS_ALERT_EMAILS` (default: every PLATFORM_ADMIN); two consecutive failures → console banner on every page for PLATFORM_ADMINs | app |

Backups run online, with `nice`/`ionice` class idle, and never drain.

| # | Restore step (`atsapbx-ctl restore <file>`) |
|---|---|
| 1 | Verify hash, decrypt header with the passphrase, read the embedded version; refuse on mismatch with the installed version |
| 2 | Take a safety snapshot of the current database (so a restore can itself be undone) |
| 3 | Drain (B2.6 rules) — a restore replaces live configuration |
| 4 | Stop app and Asterisk; drop and recreate the database; `pg_restore`; restore `etc/` (ctl asks before overwriting TLS material) and `media/` |
| 5 | Start; health gate |
| 6 | Licence: the restored `licensing_state` is re-verified on load. On new hardware the 3-of-5 fingerprint (D-13) may not match → **Degraded, never Setup** (D-12, D-52); ctl prints "Re-apply your licence from the portal" |
| 7 | `RESTORED`; audit written into the restored database as the first row after restore |

## B.4 State machines

### B.4.1 Upgrade

| From | Trigger | Guard | To |
|---|---|---|---|
| `IDLE`/`COMMITTED` | `upgrade` | lock acquired | `PREFLIGHT` |
| `PREFLIGHT` | checks | all FAIL-level pass, version supported | `SNAPSHOT`, else `FAILED_PREFLIGHT` (terminal, nothing changed) |
| `SNAPSHOT` | dump ok | hash written | `STAGING`, else `FAILED_SNAPSHOT` (terminal, nothing changed) |
| `STAGING` | images loaded | digests match | `EXPANDING` |
| `EXPANDING` | migrations | all applied | `DRAINING`; error → `MIGRATION_FAILED` → (auto clean-up B3) `PREVIOUS_SERVING` |
| `DRAINING` | poll | active calls = 0 | `SWITCHING` |
| `DRAINING` | timer | `drain_timeout` passed | `DRAIN_TIMEOUT` → old version serving; re-run later |
| `DRAINING` | `drain cancel` | operator | `PREVIOUS_SERVING` |
| `SWITCHING` | containers up | — | `VERIFYING` |
| `VERIFYING` | gate | all checks pass within `health_gate_timeout` | `SERVING_UNCOMMITTED` |
| `VERIFYING` | gate | any check fails | `ROLLING_BACK` |
| `SERVING_UNCOMMITTED` | `rollback` | images retained | `ROLLING_BACK` |
| `SERVING_UNCOMMITTED` | `commit` / timer | — | `COMMITTING` → `COMMITTED` |
| `COMMITTING` | contract error | — | `COMMIT_FAILED` (still serving the new version; rollback no longer offered; operator runs `commit` again after fix) |
| `ROLLING_BACK` | gate passes | — | `ROLLED_BACK` |
| `ROLLING_BACK` | gate fails | — | `ROLLBACK_FAILED` → documented restore from snapshot (§B.3.5) |

### B.4.2 Backup run and bundle

| From | Trigger | To |
|---|---|---|
| `SCHEDULED` | time / `RunBackupNow` | `RUNNING` (refused if another `RUNNING`) |
| `RUNNING` | written | `VERIFYING` |
| `VERIFYING` | ok / fail | `SUCCEEDED` / `FAILED` |
| `RUNNING` | error / app restart | `FAILED` (partial file deleted on next start) |
| `SUCCEEDED` | retention | `PRUNED` |
| Bundle `COLLECTING` | done | `READY` → `DOWNLOADED` / `EXPIRED` (file deleted) |

## B.5 Data model (migration 0074)

```sql
-- 0074_lifecycle.up.sql  (phase: expand)
-- Installation-scoped like licensing_state (0005): NO tenant_id, NO RLS.
CREATE TABLE lifecycle_operations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          TEXT NOT NULL CHECK (kind IN ('INSTALL','UPGRADE','ROLLBACK','COMMIT','BACKUP','RESTORE','BUNDLE')),
    state         TEXT NOT NULL,                 -- §B.4 names, validated in Go
    from_version  VARCHAR(32),
    to_version    VARCHAR(32),
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    actor         VARCHAR(100) NOT NULL,         -- principal id or 'operator_cli:<unix user>'
    rollback_available_until TIMESTAMPTZ,
    artifact_path VARCHAR(512),                  -- backup/snapshot/bundle file
    artifact_sha256 BYTEA,
    size_bytes    BIGINT,
    detail        JSONB NOT NULL DEFAULT '{}'::jsonb   -- checks, counts, errors; never secrets
);
CREATE INDEX lifecycle_operations_kind_started_idx ON lifecycle_operations (kind, started_at DESC);
-- At most one running mutating operation.
CREATE UNIQUE INDEX lifecycle_one_running ON lifecycle_operations ((true))
    WHERE finished_at IS NULL AND kind IN ('UPGRADE','ROLLBACK','COMMIT','RESTORE');

CREATE TABLE backup_settings (                   -- single row
    id                 BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    schedule_cron      VARCHAR(64) NOT NULL DEFAULT '30 2 * * *',
    time_zone          VARCHAR(64) NOT NULL DEFAULT 'UTC',
    keep_daily         SMALLINT NOT NULL DEFAULT 7 CHECK (keep_daily BETWEEN 1 AND 35),
    keep_weekly        SMALLINT NOT NULL DEFAULT 4 CHECK (keep_weekly BETWEEN 0 AND 5),
    include_media      BOOLEAN NOT NULL DEFAULT true,
    offsite_kind       TEXT NOT NULL DEFAULT 'none' CHECK (offsite_kind IN ('none','s3')),
    offsite_endpoint   VARCHAR(255),
    offsite_bucket     VARCHAR(255),
    offsite_prefix     VARCHAR(255),
    verify_restore     TEXT NOT NULL DEFAULT 'weekly' CHECK (verify_restore IN ('never','weekly','every')),
    updated_by         UUID,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
GRANT SELECT, INSERT, UPDATE ON lifecycle_operations, backup_settings TO atsapbx_app;
-- atsapbx_ops (ctl) has the same plus migration ownership; offsite access keys live in
-- /etc/atsapbx/secrets, never in this table.
```

Retention: `lifecycle_operations` kept 2 years (a few hundred rows);
snapshots: last 2; bundles: `bundle_retention`; backups: §B.7.

The **host file** `/var/lib/atsapbx/lifecycle/state.json` is the
authority during an operation (the database may be the thing being
restored); the table is the console-visible record, written by ctl at
each transition and re-synced from the file after a restore.

## B.6 Ports and interfaces

```go
package lifecycle // api/internal/platform/lifecycle — pure parts

func RunPreflight(facts HostFacts, mode Mode) []CheckResult                 // pure: thresholds of §B.3.1
func NextUpgradeState(cur UpgradeState, ev UpgradeEvent) (UpgradeState, error) // pure: §B.4.1
func Supported(installed, target semver.Version, m Manifest) error         // pure: §B.8.2 (local semver type, no dependency)
func ClassifyMigration(sql string) (Phase, []Violation)                    // pure: expand allowlist lint
func Redact(rs RuleSet, salt []byte, in []byte) (out []byte, counts map[RuleID]int, err error) // pure
func PruneBackups(list []BackupRef, keepDaily, keepWeekly int, now time.Time) (keep, drop []BackupRef) // pure
func RollbackBudget(steps []StepTiming) (time.Duration, bool)              // pure: ≤ 30 min check used by tests

type HostProbe interface { Facts(ctx context.Context) (HostFacts, error) }          // ctl: /proc, os-release, ss, sysctl
type Containers interface { Load(ctx, tar string) error; Up(ctx, svc ...string) error; Stop(ctx, svc ...string) error; Tag(ctx, from, to string) error }
type Migrator interface { Up(ctx context.Context, source Source) (from, to uint, err error); Force(ctx context.Context, source Source, v int) error; Version(ctx context.Context, source Source) (uint, bool, error) }
type AdminSocket interface { Drain(ctx context.Context, cmd DrainCmd) (DrainStatus, error); Health(ctx context.Context) (HealthReport, error) }
type Dumper interface { Dump(ctx context.Context, w io.Writer) error; Restore(ctx context.Context, r io.Reader, db string) error }
type BackupTarget interface { Put(ctx context.Context, name string, r io.Reader) error; List(ctx context.Context) ([]BackupRef, error); Delete(ctx context.Context, name string) error }
// local filesystem: stdlib. s3: needs owner approval (OD-6).

// In the app, consumed by call admission (telephony-core screening and pbx PlaceCall):
type MaintenanceGate interface {
    Admit(ctx context.Context, emergency bool) error // ErrDraining unless emergency
    State() DrainStatus                              // {Draining bool, Since, ActiveCalls int, Longest time.Duration}
}
```

`MaintenanceGate` is an in-process flag (not persisted): an app restart
clears a drain, which is correct because ctl re-asserts it if the
operation is still `DRAINING`.

## B.7 API contract — `InstallationService` (`api/proto/atsapbx/v1/installation.proto`)

Consumer (D-43): the console System screen (PLATFORM_ADMIN). Upgrade,
rollback and restore have **no** RPC (OD-5): they need container control.

| RPC | Request → Response | Permission (system scope) |
|---|---|---|
| `GetInstallationStatus` | → `version, schema_version, contract_version, upgrade_state, rollback_available_until?, draining, active_calls, last_backup{at, state}, supported_upgrade_from[]` and the ctl command text for the next action | `installation.read` |
| `ListLifecycleOperations` | `kind?, page_size ≤ 100, page_token` → `operations[]` | `installation.read` |
| `CreateDiagnosticBundle` | `since_hours (1–168), include_pii, pii_reason (10–500 when include_pii), encrypt_for_support` → `operation_id` | `installation.diagnostics` (+ `installation.diagnostics.pii` when `include_pii`) |
| `GetDiagnosticBundle` | `operation_id` → `state, size_bytes, manifest_summary, download_url (single use, 15 min)` | `installation.diagnostics` |
| `GetBackupSettings` / `UpdateBackupSettings` | §B.5 fields (offsite keys write-only, never returned) | `installation.read` / `installation.backup` |
| `RunBackupNow` | → `operation_id` | `installation.backup` |
| `ListBackups` | → `backups[{name, at, version, size, sha256, location: local\|offsite\|both, verified}]` | `installation.read` |

| Refusal | Code | Console wording |
|---|---|---|
| Another backup running | `FailedPrecondition` | "A backup is already running." |
| Retention beyond OD-7 ceiling | `InvalidArgument` | "Backups can be kept for at most 35 days, so deleted data really is deleted." |
| Offsite unreachable on save | `FailedPrecondition` | "We couldn't write to that storage: <reason>. Settings were not saved." |
| PII without reason | `InvalidArgument` | "Say why personal data is needed; it's recorded." |
| PII without the PII permission | `PermissionDenied` | "You can't include personal data in a bundle." |
| Bundle disk space | `ResourceExhausted` | "Not enough disk space for a bundle." |
| Download link used/expired | `NotFound` | "This download link has expired. Create a new link." |
| Not system scope | `PermissionDenied` | "Only your provider can manage this system." |

## B.8 Configuration and limits

### B.8.1 Settings

| Setting | Default | Range | Scope |
|---|---|---|---|
| `drain_timeout` | 15 min | 1–120 min | ctl flag / `/etc/atsapbx/lifecycle.env` |
| `rollback_drain_timeout` | 10 min | 1–15 min | same |
| Drain poll | 5 s | fixed | — |
| `health_gate_timeout` | 5 min | 1–15 min | same |
| `commit_after` | 7 days (HLD-06) | 0 (manual) or 1–30 days | same |
| Snapshots retained | 2 | 1–5 | same |
| Bundle `since_hours` | 48 | 1–168 | per request |
| `bundle_max_bytes` | 200 MiB | 50–1024 MiB | installation |
| `bundle_retention` | 7 days | 1–30 | installation |
| Bundle per-item timeout | 30 s | fixed | — |
| Backup schedule | daily 02:30, installation time zone | cron | installation |
| Retention | 7 daily + 4 weekly | daily 1–35, weekly 0–5, total span ≤ OD-7 ceiling (35 days) | installation |
| `verify_restore` | weekly | never/weekly/every | installation |
| Local backup path | `/var/backups/atsapbx` | any mount | installation |
| Argon2id for backup key | m = 64 MiB, t = 3, p = 4 | fixed per format version | — |
| `ATSAPBX_OPS_ALERT_EMAILS` | all PLATFORM_ADMINs | list | installation |

### B.8.2 Supported-version matrix

| Item | Rule |
|---|---|
| Upgrade into `X.Y.Z` | From any patch of `X.Y`, `X.(Y-1)` and `X.(Y-2)`; anything older steps through an intermediate release (ctl names it) |
| Major version | From the last minor of the previous major only |
| Rollback | Only to the exact version upgraded from, only while uncommitted |
| Restore | Only onto the same version that wrote the backup |
| Operating systems | Debian 12; Ubuntu 22.04, 24.04; RHEL 9 and rebuilds — x86_64 |
| Tested | CI `upgrade-matrix` runs every supported `from` × each OS family image before release (AC-09.3); a missing cell blocks the release |

## B.9 Failure and recovery

| Failure | Behaviour | Recovery |
|---|---|---|
| Power loss mid-upgrade | `state.json` names the step; ctl on next run offers *resume* (idempotent steps) or *rollback* | Operator chooses |
| Migration fails | §B.3.3 "when a migration itself failed"; old app never stopped | Fix and re-run |
| Drain never completes | `DRAIN_TIMEOUT`, old version serves; remaining calls listed | Re-run in a quieter period or end specific calls deliberately |
| New version unhealthy | Automatic rollback within budget | Investigate with a bundle |
| Rollback gate fails | `ROLLBACK_FAILED`; ctl prints restore command for the pre-upgrade snapshot | §B.3.5 |
| Disk fills during snapshot | `FAILED_SNAPSHOT`, nothing changed, partial file removed | Free space |
| App down during bundle | ctl collects host-side items, lists app items as excluded `app_unreachable` | — |
| Redactor error | Item excluded (fail closed) | — |
| Backup target unreachable | `FAILED`, alert; next schedule retries; local copy kept even when offsite fails (`location: local`) | — |
| Passphrase lost | Backups are unrecoverable — stated at install and on the settings page | Set a new passphrase; next backup uses it |
| Restore to a host with a different fingerprint | Licence Degraded (4 SC), never Setup; calls work | Re-apply token |
| Emergency call during drain | Admitted; drain waits for it | — |
| App restart during drain | Gate cleared; ctl re-asserts within 5 s (poll) | — |

## B.10 Invariants, events and audit

| Rule | How it holds |
|---|---|
| **INV-03** | No lifecycle step stops a container carrying calls: drain to zero first; timeout returns to the old version; ctl has no "force" |
| **INV-01** | `MaintenanceGate.Admit(emergency=true)` always nil |
| AC-09.3 | Expand-only before switch; contract after commit; `upgrade-matrix` CI |
| AC-09.4 | Rollback needs no schema change; budget table §B.3.3 is asserted by the canary rollback benchmark (HLD-06 §5 item 3) |
| AC-09.5 | Allowlisted config, rules R1–R10, fail-closed exclusion, never-included list, PII opt-in audited |
| AC-09.6 | Media without `UPGRADE.md`/`ROLLBACK.md` fails the pipeline and ctl |
| D-12 / D-52 | Restore and upgrade never put a licensed system into Setup |
| AC-07.3 / AC-23.2 / T-9 | Backup retention ceiling (OD-7) below the stated deletion window |
| Secrets | Backups encrypted; offsite keys and passphrase only in `/etc/atsapbx/secrets` |

**Events.** None on NATS: outbox subjects are tenant-scoped
(`tenant.<id>.event.…`) and nothing consumes installation lifecycle
events in R1.0 (D-43 test: no named consumer). Operators are reached by
e-mail; the record is `lifecycle_operations`.

**Audit** (`audit_logs` under the acting PLATFORM_ADMIN's home tenant;
ctl writes `actor_type = operator_cli` under the bootstrap tenant):
`installation.upgrade.started|completed|rolled_back|committed|failed`,
`installation.restore.completed`, `installation.backup.completed|failed`,
`installation.backup.settings_updated`, `installation.diagnostics.created`,
`installation.diagnostics.pii_included` (with reason),
`installation.diagnostics.downloaded`.

## B.11 Test scenarios

| # | Given / When / Then | Tier |
|---|---|---|
| B-T1 | Given host facts with 4 GiB RAM / When `RunPreflight` / Then FAIL "RAM" and no further steps | U |
| B-T2 | Every row of §B.4.1 / When `NextUpgradeState` / Then table result; others error | U |
| B-T3 | Given `Supported(1.2.3 → 1.5.0)` / Then refused naming 1.3.x; `1.3.9 → 1.5.0` accepted | U |
| B-T4 | Given SQL with `DROP COLUMN` tagged expand / When `ClassifyMigration` / Then violation; every file in `api/migrations` passes | U |
| B-T5 | Given a corpus seeded with canary secrets (SIP password, JWT, PEM, Basic URL), numbers, e-mails, public IPs, display names / When `Redact` / Then zero canaries remain and the same number maps to the same token | U |
| B-T6 | Given `include_pii=true` / When `Redact` / Then R6–R10 untouched, R1–R5 still applied | U |
| B-T7 | Given 40 daily backups / When `PruneBackups(7,4)` / Then 7 most recent + 4 Sundays kept | U |
| B-T8 | Given drain on / When a non-emergency inbound call and an emergency call arrive / Then 503 (cause 41) and connected respectively | E |
| B-T9 | Given 50 live calls / When upgrade with drain / Then 100 % complete normally; switch happens only at zero (HLD-06 §5 item 2) | E |
| B-T10 | Given one call outlasting `drain_timeout=1m` / When upgrading / Then `DRAIN_TIMEOUT`, old version serving, call intact | E |
| B-T11 | Given an injected unhealthy canary / When upgrading / Then automatic rollback, `ROLLED_BACK` in ≤ 30 min measured (AC-09.4) | E |
| B-T12 | Given previous release's integration suite / When run on the new expanded schema / Then green (`schema-compat`) | I |
| B-T13 | Given a failing expand file / When upgrading / Then old app never stops, version forced back, no `INVALID` index remains | I |
| B-T14 | Given a generated bundle on the rig after a call with a known number / When grepping / Then no secret, no audio file, number only as `num#…`; manifest hashes verify | E |
| B-T15 | Given a backup / When `verify_restore` / Then scratch DB counts match TOC and the scratch DB is dropped | I |
| B-T16 | Given a backup from 1.4.0 / When restoring onto 1.4.1 / Then refused with the version message | W |
| B-T17 | Given a restore onto different hardware / When the app starts / Then licence Degraded, not Setup | E |
| B-T18 | Given `UpdateBackupSettings` keep_daily=60 / Then `InvalidArgument` with the retention wording | W |
| B-T19 | System screen shows version, upgrade state, rollback deadline, drain count, last backup; PII checkbox requires a reason | C |
| B-T20 | A PLATFORM_ADMIN creates a bundle and downloads it once; a second download is refused | B |
| B-T21 | A certified engineer installs on a clean Debian 12 VM from media, unaided, in < 60 min, then makes a call between two phones (AC-09.1) | A |
| B-T22 | Upgrade N-1 → N with two baresip phones mid-call: neither call drops; then rollback; the phones re-register and call | A |

## B.12 Industry parity

| Capability | 3CX | VitalPBX | FreePBX | FusionPBX | AtsaPBX |
|---|---|---|---|---|---|
| Install media | ISO / Debian packages | ISO / install script | ISO (SNG7/Debian) | Install script | Signed offline tarball or digest-pinned online pull; pre-flight first |
| Upgrade path | From console | Console / package manager | `fwconsole ma upgrade` per module | `git pull` + upgrade script | CLI, expand/contract, drain, health gate (OD-5) |
| Live calls during upgrade | Services restart; calls may drop | Restart drops calls | Reload/restart may drop | Restart drops | **Never dropped**: drain or wait |
| Rollback | Restore a backup | Restore a backup | Module downgrade, no schema guarantee | Manual | Guaranteed ≤ 30 min while uncommitted, no restore needed |
| Scheduled backup | Yes; local, FTP/SFTP/SMB, Google Drive; optional encryption; keep N | Yes; local + remote | Backup & Restore module; local, SFTP, S3, e-mail | Scripted `pg_dump` | Daily, encrypted always, local + S3 (OD-6), retention capped by deletion window, weekly test-restore |
| Diagnostics | Support info / logs download | Logs download | `fwconsole` logs | Logs | Manifested, redacted, pseudonymised, encrypted to support, PII opt-in audited |

Deliberate differences: no console-driven container control, encryption
not optional, retention bounded by data-deletion promises, and a
rollback that does not depend on a backup.

---

## C. Definition of Ready check

| DoR item | A1–A8 | B1–B5 |
|---|---|---|
| 1 Trigger and actors | §A.1 | §B.1 |
| 2 Preconditions | §A.2 | §B.2 |
| 3 Sequence (engine resources named) | §A.3 (AMI `PJSIPNotify`, `ps_contacts`, realtime `ps_auths`; ARI has no NOTIFY) | §B.3 (ARI `GET /asterisk/info`, `/readyz`, SIP OPTIONS) |
| 4 State machines | §A.4 | §B.4 |
| 5 Data model | §A.5 (0070–0073) | §B.5 (0074) |
| 6 Ports | §A.6 | §B.6 |
| 7 API contract | §A.7 | §B.7 |
| 8 Configuration | §A.8 | §B.8 |
| 9 Failure and recovery | §A.9 | §B.9 |
| 10 Invariants, events, audit | §A.10 | §B.10 |
| 11 Tests | §A.11 | §B.11 |
| 12 Parity | §A.12 | §B.12 |

## D. Spikes and unresolved items

| # | Item | Blocks |
|---|---|---|
| S-12.1 | Confirm per family that the factory client certificate is presented on HTTPS provisioning and that its subject/SAN carries the MAC; obtain each vendor CA bundle and its redistribution terms | A2 `mtls` default per family |
| S-12.2 | Confirm NOTIFY event strings for reboot/resync on Grandstream GRP and Poly Edge E firmware in the matrix | A3 |
| S-12.3 | How to remove a realtime `ps_contacts` contact on revoke without editing the engine's table | A7 known limitation |
| U-12.1 | BLF lamps depend on LLD-03 SPIKE-P1 (pattern hint `atsa-blf`); fallback rendering defined in §A.3.9 | BLF lamps only |
| U-12.2 | `RotateForDevice` and the `DeleteExtension` refusal are LLD-03 changes; OD-2 amends LLD-03 §7.3 | A1 |
| U-12.3 | `MaintenanceGate` is consulted in `telephony-core` call admission; LLD-01 needs the matching line | B2 |
| U-12.4 | API image must carry `pg_dump`/`pg_restore` 16 client tools (D-86 image size impact ≈ +10 MB) | B5 |
| U-12.5 | D-48 numbering (OD-9) | Index accuracy |

## E. OpenSpec handoff

| Change | Content | Order |
|---|---|---|
| `pbx-device-inventory` | 0070–0071, A1, A7, A8 key templates, `ProvisioningService` CRUD, console Phones list | 1 (after OD-1, OD-2) |
| `pbx-provisioning-server` | `:8443` listener, A2 (basic + mtls), A5, A6 sweeper, Yealink pack | 2 |
| `pbx-provisioning-packs` | Poly and Grandstream packs, redirect-service hints | 3 |
| `pbx-device-actions` | 0073, A3 with the INV-03 deferral | 4 |
| `pbx-device-firmware` | 0072, A4 | 5 |
| `lifecycle-media-and-install` | media format, signing, ctl install + pre-flight (B1) | 6 (parallel with 1–5) |
| `lifecycle-upgrade-rollback` | 0074, migration phases, drain gate, B2, B3, CI `schema-compat` and `upgrade-matrix` | 7 |
| `lifecycle-diagnostic-bundle` | B4, `InstallationService` read side | 8 |
| `lifecycle-backup-restore` | B5 (local first; S3 after dependency approval) | 9 |
