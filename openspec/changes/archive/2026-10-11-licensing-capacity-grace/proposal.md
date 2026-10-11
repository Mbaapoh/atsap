## Why

`telephony-core` has called `LicenseManager.ValidateCapacity` on every
call setup since LLD-01, against `alwaysPermitLicense` — a five-line stub
that returns `Permitted: true` and counts nothing. Every commercial
control the product is sold on (EPIC-06: edition, channel capacity,
entitlement, offline grace) is therefore unimplemented while the seam
that would carry it is already wired and load-bearing.

This is Phase A work (D-46): a partner must be able to install the
platform, licence it, and make calls. The licence half does not exist.

## What Changes

- **New bounded context `licensing`** (Tier 0, depends on nothing) under
  `api/internal/licensing/`, implementing the HLD 04 §4 `LicenseManager`:
  `ValidateCapacity`, `ReleaseCapacity`, `ApplyLicenseKey`,
  `VerifyDailyEntitlement`.
- **Atomic concurrent-channel counter** with a 10% burst allowance
  absorbing a bounded overage before rejection begins (T-2/T-3,
  AC-06.6/06.10). Correct under a 10× setup-rate race test.
- **Ed25519 licence token verification** (T-1, AC-06.1) — signature
  checked before any field is parsed, against an embedded vendor public
  key. Uses stdlib `crypto/ed25519`; no new dependency.
- **7-day offline grace tracker** (D-12/D-14, AC-06.4/06.5): a pure state
  machine over timestamps — `Valid` → `EntitlementUnverified` (warnings
  from day 2, no functional restriction) → `Degraded` (reduced capacity,
  **never disabled**).
- **`licensing_state` migration** — the HLD 03 §5 DDL that no migration
  has ever created. Installation-scoped: **no `tenant_id`, no RLS**, the
  one deliberate exception to D-24 seam 1, asserted by test.
- **`stub_license.go` is deleted** and `cmd/atsap-api` wires the real
  adapter behind the same port (LLD-08 DoD 7).
- **BREAKING (internal port):** `telephony/ports.LicenseManager` gains
  `ReleaseCapacity`, and `telephony-core` calls it at call teardown. See
  "The tripwire fired" below — this is the one part of this proposal that
  its design source forbids, and it is proposed deliberately.
- **A missing `licensing_state` row is Setup, not a capacity floor**
  (D-52). A never-activated installation has **no call path** — admin
  console only. The 4-channel floor belongs to Free Community and to
  degradation, never to absence, so there is no unsigned entitlement
  path anywhere in the context. The extension and tenant caps are
  published as entitlement data and enforced where extensions and
  tenants are created (pbx and identity side, separate changes) — see
  design D5.
- **Token intake, because without it nothing can place a call** — see
  "Re-sliced" below. `ApplyLicenseKey`'s domain operation and the
  `ATSAPBX_LICENSE_TOKEN` variable land here; the portal-facing RPC, CLI
  and hardware-fingerprint matching stay in `licensing-apply-key`.
- **The stored entitlement is the signed payload, re-verified on load**
  (D-53). `licensing_state` gains `signed_payload` and `signature`; the
  claim columns become a display cache never read for a decision.
- **Verification always runs; only the trusted key set varies** (D-54).
  A production build trusts one key; a development build additionally
  trusts a key injected via `-ldflags`, empty by default so a forgotten
  flag yields the strict binary. There is no bypass flag in any build.

## Re-sliced: this change was not shippable as first scoped

As first written this change assumed a missing row meant four working
channels, so it could land, pass its own end-to-end check, and ship.
**D-52 removed that assumption.** A never-activated installation now has
no call path at all, and applying a token was scoped to the *next*
change.

Landing the original slice would therefore have produced an installation
that **cannot place a single call** — its own Definition of Done ("the
walking skeleton still passes with the stub gone") would have been
unmeetable, and both e2e suites would fail with no defect to fix.

The boundary moves rather than the goal: **the token intake path joins
this change**, and the portal-facing surface stays in
`licensing-apply-key`.

| Here | `licensing-apply-key` |
|---|---|
| `VerifyToken`, the trusted key set, `-ldflags` injection | `ApplyLicenseKey` **RPC** and CLI |
| `ApplyLicenseKey` as a domain operation | Hardware fingerprint **collection** |
| `ATSAPBX_LICENSE_TOKEN` intake at startup | Fingerprint **3-of-5 matching** |
| Signed-payload storage and load-time re-verification | `GetLicenseStatus` RPC |
| Counter, burst, grace, `licensing_state` migration, cutover | Console licence screen |

Both halves stay independently shippable: this one activates an
installation from a token supplied by configuration; the next gives that
token a portal, an operator interface and machine binding.

**The name is kept.** Capacity and grace remain the bulk of the work, and
`licensing-capacity-grace` is cited from LLD-08 §9 — renaming an
in-flight change to improve a label is churn for no reader's benefit.

## The tripwire fired, and this proposal does not route around it

LLD-08 §1 states that if this work requires editing anything under
`internal/telephony/` beyond deleting the stub's wiring, "the seam LLD-01
built has failed and work stops". It does require exactly that, for a
reason no document anticipated:

**`ReleaseCapacity` has zero occurrences in the codebase.** It is named
in HLD 04 §4 and in LLD-08 §3's port, but `telephony/ports.LicenseManager`
declares only `ValidateCapacity` (`ports.go:141`), and nothing anywhere
calls a release. LLD-01 built half the seam — enough for a stub that
counts nothing, not enough for a counter. Landing a real counter against
today's code produces a capacity figure that only ever rises, until every
call in the installation is rejected and no call is ever dropped to
correct it.

The alternatives are worse and two of them are forbidden:

- *Have `licensing` subscribe to call-lifecycle events to decrement.*
  Forbidden: HLD 04 §10.1 says `licensing` may depend on **nothing**.
- *Have `licensing` read call state from `telephony-core`'s tables.*
  Forbidden by the same row, and by the cross-context table-read rule.
- *Ship the counter without release.* Rejected — it is a defect, not a
  limitation.

So `telephony-core` changes: one method added to a port it owns, one call
added at `finalizeTermination` (`service.go:368`). The seam direction is
unchanged — `telephony-core` still calls a port and never imports
`licensing`. LLD-08 §1 and DoD 7 are amended by this change to record
that the tripwire fired, why, and what the narrowed rule is.

## Capabilities

### New Capabilities

- `licensing/capacity-enforcement`: concurrent-channel counting against
  an entitlement, the burst allowance, rejection with a distinct reason,
  release at teardown, and the invariants that no active call is ever
  dropped (INV-03) and no licence state can block an emergency call
  (INV-01).
- `licensing/entitlement-grace`: the daily entitlement check, the 7-day
  offline grace state machine with day-2 warnings, degradation to a
  defined minimum, and recovery on confirmation.
- `licensing/license-token`: Ed25519 verification before parsing, the
  trusted key set, what a tampered or expired payload does, how a token
  is applied, and why editing the stored row changes no decision.

### Modified Capabilities

- `telephony-core/call-lifecycle`: Screening already consults capacity;
  this adds the matching **release at termination**, so a rejected call
  and a completed call both leave the counter where they found it.

## Impact

- **New:** `api/internal/licensing/{domain,ports,application,postgres}`,
  `api/migrations/0005_licensing.{up,down}.sql`.
- **Changed:** `api/internal/telephony/ports/ports.go` (one port method),
  `api/internal/telephony/application/service.go` (one call site),
  `api/cmd/atsap-api/main.go` (real adapter replaces the stub).
- **Deleted:** `api/internal/telephony/application/stub_license.go`.
- **Docs:** `docs/lld/LLD-08-licensing.md` §1/§8 amended (tripwire),
  `docs/API.md` unchanged — `ValidateCapacity` and `ReleaseCapacity` stay
  port-only per D-43 and LLD-08 §6; the two licensing RPCs belong to
  `licensing-apply-key`.
- **Config:** `ATSAPBX_LICENSE_TOKEN` (matching the existing
  `ATSAPBX_JWT_KEY` prefix), and a build-time `-ldflags` variable for the
  development public key, empty by default.
- **Gates:** a `licensing-imports-nothing` depguard rule (LLD-08 DoD 9),
  fault-injected once to prove it rejects; plus a test asserting a
  default build trusts exactly one key and rejects a development-signed
  token (D-54).
- **Test rig:** the dev stack and both e2e suites gain the committed
  development token through `ATSAPBX_LICENSE_TOKEN` — they place real
  calls and can no longer do so unlicensed (D-52's stated cost). The
  suites exercise activation rather than bypassing it.
- **Not here:** hardware fingerprint collection and matching, the
  `ApplyLicenseKey`/`GetLicenseStatus` RPCs and CLI — all
  `licensing-apply-key`. The emergency bypass itself is `pbx-core`'s
  (INV-01, LLD-03); this change only guarantees licensing cannot block it.
  Extension-count enforcement against the recorded cap is a pbx-side
  provisioning concern and is filed as a follow-up (design D5), not
  built here.
