# Design — licensing-capacity-grace

**Design source:** [`docs/lld/LLD-08-licensing.md`](../../../docs/lld/LLD-08-licensing.md)
(the whole document; §3.1 for the port question, §5 for the table, §8 for
the Definition of Done). This file records only what the LLD does not
already decide, and the two places this change deviates from it.

## D1. The tripwire fired: `telephony-core` gains `ReleaseCapacity`

LLD-08 §1 says work stops if this change must edit anything under
`internal/telephony/`. It must.

**Observed, not assumed:** `ReleaseCapacity` has zero occurrences in the
Go codebase. `telephony/ports.LicenseManager` (`ports.go:141`) declares
only `ValidateCapacity`; `service.go:127` calls it at Screening; nothing
releases. LLD-01 built the half of the seam its stub needed.

Landing a real counter against that code gives a number that only rises.
Every installation would refuse all calls after enough traffic, and no
call would ever be dropped to correct it — a defect that would look like
a licensing bug and be diagnosed as one.

**What changes, minimally:**

| File | Change |
|---|---|
| `telephony/ports/ports.go` | `LicenseManager` gains `ReleaseCapacity(ctx, callID)` — keyed by call so it is idempotent (D3, D-58) |
| `telephony/application/service.go` | `finalizeTermination` (`:368`) calls it once per released channel |

**What does not change:** the direction of the seam. `telephony-core`
still calls a port it owns and still never imports `licensing`. The
tripwire existed to catch `licensing` leaking into `telephony-core`; that
has not happened.

**Rejected, both forbidden by HLD 04 §10.1 (`licensing` may depend on
nothing):** having `licensing` subscribe to call-lifecycle events, or
read `telephony-core`'s tables. **Also rejected:** shipping without
release — a defect is not a limitation.

LLD-08 §1 and DoD 7 are amended by this change to record that the
tripwire fired and to narrow the rule to what it was actually protecting.

## D2. Two ports, and the adapter lives in the composition root

Settled by LLD-08 §3.1 and restated here because it is the first thing an
implementer will want to "simplify":

`telephony/ports.LicenseManager` (now two methods) is the **consumer's**
interface. `licensing/ports.LicenseManager` (four methods) is the
**provider's**. They are not duplicates to be merged.

Merging them requires `licensing` to import `telephony/ports` for the
`CapacityVerdict` type — a Tier-0 context depending on a Tier-0 peer,
denied by HLD 04 §10.1. `cmd/atsap-api` holds a small adapter translating
between the two verdict types. That is the composition root doing its job.

## D3. Release is keyed by call, so it is idempotent rather than merely guarded

**Revised under D-58.** The first draft relied on `finalizeTermination`
running once per call — the single funnel every termination route
reaches, guarded by the `CallTerminated` state check. That is
**at-most-once by guard, not idempotency**, and the distinction matters
because of which way it fails.

A guard that stops holding decrements a counter twice for one call. The
count then reads *lower* than reality, so the installation permits calls
it should refuse — licence leakage that no test notices, because
everything still works. Nothing surfaces until a partner is running more
channels than they bought.

So `ReleaseCapacity` takes a **call identifier** rather than a channel
count, and the counter tracks which calls hold a reservation. A repeat
release for a call that no longer holds one is a no-op returning success,
not an error the caller must interpret and not a decrement. The guard
stays — it is still correct and still cheap — but the invariant no longer
depends on it, and a future termination path added by someone who has not
read this file cannot silently break it.

**A screened-out call never reserved anything.** `ValidateCapacity`
increments only when it permits, so a refusal leaves the counter
untouched and termination must not release. Keying by call makes this
fall out for free: there is no reservation under that call id to release.


## D4. Capacity state is in memory; `licensing_state` is not a counter

`licensing_state` holds the *entitlement* — edition, capacity, expiry,
last confirmation. The count of channels *in use* is process-local and
deliberately not persisted (LLD-08 §5, §7).

Consequence, stated so it is not discovered later: **a restart resets the
in-use count to zero while calls are still up.** With one node this
self-corrects as those calls end, and the alternative — persisting a
counter that must be reconciled against live channels — is the multi-node
work D-08 defers. The reconciliation problem is real and is not solved
here; it is named in the LLD's Known Limitations and stays there.

## D5a. Setup is the absence of an entitlement, and it is not a floor (D-52)

**This supersedes D5 below, which was written before D-52.** D5 is kept
because its rejected alternatives are still the right rejections; only its
conclusion changed.

A missing `licensing_state` row means **Setup**: no call path, admin
surface fully available, a distinct refusal reason. It is not four
channels, not unlimited, and not the degraded state.

The distinction is load-bearing rather than pedantic. Degradation exists
to protect a *running* phone system from a licence problem, so it must
never disable one (D-12, INV-03, BR-09). Setup protects nothing — there
is no system yet. If the two shared a code path, the first refactor that
simplified them would make an expired licence disable a production
installation, which is the one outcome the product promises never to
produce. **Two states, two reason codes, asserted apart** (LLD-08 DoD 11).

The consequence for this change is that the 4-channel floor is reached
only by *degrading from* a real entitlement, never by *lacking* one:

| Condition | Channels |
|---|---|
| No entitlement on record | none — Setup |
| Free Community entitlement | 4 |
| Expired, grace elapsed, or tampered | 4 (the floor constant) |
| Valid paid entitlement | as purchased |

## D5b. Storage integrity and the trusted key set (D-53, D-54)

Two mechanisms that did not exist when this change was first drafted, and
that most of the new task volume comes from.

**The row is not the entitlement.** `licensing_state` stores
`signed_payload` and `signature`; everything else in the row is a display
cache. The load path is read → verify → parse → cache in memory. Without
this, one `UPDATE` grants any capacity — the partner owns the database,
so a trusted row is an editable one. Verification runs on load and on
apply, **never during call setup** (AC-06.3).

**Verification never has an off switch.** What varies between builds is
the trusted key set: a released binary trusts one key; a development
build additionally trusts a key injected via `-ldflags`, empty by
default so a forgotten flag produces the strict binary. `ATSAPBX_LICENSE_TOKEN`
carries a *token*, never a mode.

A mode flag was the obvious alternative and is the worst one: a single
string that disables D-11, D-13, D-14 and D-53 at once, and it would mean
the verification path is exercised only in production.

## D5. Unlicensed means the Unregistered tier, reached by a route PRD §11.2 did not list

> **Superseded by D5a.** Retained for its rejected alternatives, which
> still hold. Its conclusion — that a missing row means a four-channel
> floor — does not.

BRD §10.4 already defines this case: a never-licensed installation is
the permanent, zero-config **Unregistered tier** (BR-LIC-01) — **4
concurrent channels, max 10 extensions, 1 tenant** — which is also the
degradation floor for every other degraded cause (BR-LIC-02). This
change deletes the stub that made the case moot, so it implements the
tier rather than inventing a floor.

An installation with no `licensing_state` row is **Degraded** at the
Unregistered entitlement, with a reason code distinct from
over-capacity on a valid licence.

- **Why not permissive:** it would reproduce the stub and ship a
  licensing system that licenses nothing.
- **Why not zero:** D-12 and PRD §11.2 both say never disabled, and
  AC-09.1 expects a fresh install to be provable by a certified engineer.
- **Why not a seventh PRD state:** §11.2's `Degraded` row already
  describes this behaviour exactly — reduced capacity, never disabled,
  emergency calls connect, API available. A new state would duplicate it
  to record a different cause, and the cause is already carried by the
  reason code.
- **Why 4, not 2:** BR-LIC-01 mandates 4; no document authorises a
  smaller floor, and the e2e's single two-party call fits under 4 with
  room to spare, so nothing in the test design depends on 2.
- **Why the extension and tenant caps are recorded here but enforced elsewhere:**
  the caps are entitlement data owned by licensing; enforcement happens
  where extensions and tenants are provisioned (pbx and identity sides),
  which this change must not absorb. This change exposes `maxExtensions`
  and `maxTenants` on the entitlement
  and files the provisioning-side enforcement as an explicit follow-up
  (owner: extension provisioning, pbx side; tenant provisioning, identity side).

**Why the migration does not seed a row instead:** a seeded row is an
unsigned entitlement at rest, which is exactly what D3's signature
discipline exists to prevent. The missing-row branch is cheaper than the
precedent.

## D6. Expiry and grace both resolve to the same degraded floor

An expired-but-validly-signed licence and an elapsed grace period both
produce Degraded at the same minimum. Two causes, one behaviour, one code
path — the cause is carried in the reason code and the administrator
message, not in a second mechanism.

## D7. Verification precedes parsing, structurally

`VerifyToken` takes bytes and returns a parsed token or an error. There
is no exported way to parse without verifying, so "verify first" is not a
convention an implementer can forget — it is the only available call
shape. The vendor public key is a package-level constant in the binary
(LLD-08 §10): a key fetched at runtime is a key an attacker can
substitute.

## D8. What proves this rather than asserts it

| Claim | Proof |
|---|---|
| Counter is exact under load | Race test at 10× setup rate, `-race`, asserting return to zero (AC-06.10) |
| No active call is ever dropped | Live call held across each state transition — valid → over-capacity → unverified → degraded (INV-03) |
| Emergency is never blocked | Asserted in every licence state including tampered and unlicensed (AC-06.7) |
| Release happens on every route | One test per termination route, plus a double-termination test |
| `licensing_state` has no `tenant_id` and no RLS | Schema assertion that fails if either is added (LLD-08 DoD 6) |
| Zero unintended `telephony-core` change | `git diff --stat api/internal/telephony/` shows only the two files D1 names |
| `licensing` imports nothing | `licensing-imports-nothing` depguard rule, fault-injected once (LLD-08 DoD 9) |

## D9. Out of scope, and where each lands

| Deferred | Owner |
|---|---|
| Hardware fingerprint collection and matching | `licensing-apply-key` |
| `ApplyLicenseKey` / `GetLicenseStatus` RPCs and CLI | `licensing-apply-key` |
| SIP `503` + `Retry-After` on refusal | `pbx-core` (LLD-03) — ingress shaping, not a domain verdict |
| Emergency-number identification and bypass | `pbx-core` (INV-01). This change only guarantees licensing cannot block it |
| Multi-node shared counting | D-08 multi-node work |

`ApplyLicenseKey` exists as a port method and a verified domain operation
in this change — it is how `licensing_state` is written — but has no
external surface until `licensing-apply-key`. The change that adds the
RPC updates `docs/API.md` §1 in the same commit (D-43).
