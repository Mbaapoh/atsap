## Context

`pbx-trunks-routes` left trunks and routes projected, `SelectRoutes` deterministic, and `ReportRouteHealth` port-only with no reporter. `telephony-core`'s `CallService.InitiateCall` originates **both legs at once** (`SourceEndpointURI`, `DestEndpointURI`), bridges them when both answer, and already carries `BypassScreening` from licensing 6.3. Its `CallStore.GetCall` records `StartedAt`, `AnsweredAt`, `EndedAt` and a free-form `TerminationReason`. It records **no SIP cause code**. `pbx/domain` already has `IsEmergency`/`EmergencyNumberSet` (exact digit match). The running dev app boots in Setup: its image has no development verification key and no token is supplied, so no call through it has ever been possible. The UAT tier (`make uat-auto`) drives real baresip phones for the static fixtures 1000/1001 and asserts RTP in both directions.

## Goals / Non-Goals

**Goals:**

- A real call placed through `PbxService.PlaceCall` on the running app connects with media, proven at e2e (scripted SIP) and UAT (real baresip phones), over a trunk and internally.
- Zero `telephony-core` change, zero engine vocabulary outside `pbx/acl/asterisk`.
- `channel_limit` enforced exactly under concurrency, and restart-safe.
- Trunk health reported only from outcomes that genuinely implicate the carrier.

**Non-Goals:**

- Outbound-class toll-fraud enforcement (its own change, which needs tenant country).
- Mid-setup failover after a trunk accepts a leg and then fails (LLD-03 §9).
- `Degraded` from latency: no setup-latency signal exists without SIP-level capture in `telephony-core`.
- Inbound, `emergency_numbers` table, SIP `503`/`Retry-After` (`pbx-inbound-and-emergency`).

## Decisions

**D1 — Dial-plan resolution order: emergency, then internal, then routes.**
`PlaceCall(extension_id, destination)` validates the caller's extension (RLS: another tenant's reads as nonexistent). It then resolves the destination in a fixed order. (1) `IsEmergency` against the built-in set: if it matches, `BypassScreening` is set, but the call still needs a route to leave by. (2) An exact match on a tenant extension number gives an internal call to that extension. (3) `SelectRoutes` gives a trunk leg. Otherwise the destination is unroutable, refused as `FAILED_PRECONDITION`. The destination is validated as `+?[0-9]{1,32}` before any of this, so the digits that reach the engine dial string are a closed alphabet (LLD-03 §10.5). *Alternative rejected:* routes before extensions. A route like `1` would capture an internal `1005`, and an extension is never meant to leave the building.

**D2 — The engine dial string is built only in the ACL.**
A pbx-owned port `EndpointAddresser { ExtensionAddress(ExtensionID) string; TrunkAddress(TrunkID, dialled string) string }` is implemented in `pbx/acl/asterisk`. It returns `PJSIP/e_<hex>` and `PJSIP/<digits>@t_<hex>` (leading `+` stripped for the carrier). `pbx/application` passes these strings to telephony opaquely and never inspects them. The archtest engine-table scanner and the DoD 13 response test keep it that way.

**D3 — Origination through telephony's port directly; call status through a pbx-owned port.** *(Revised at apply time — see the note at the end of this decision.)*

**Original text:**
`CallOriginator { Originate(ctx, OriginateRequest) (CallID, error); Hangup(ctx, CallID) error }` and `CallStatus { Status(ctx, tenant, CallID) (CallOutcome, error) }` are declared in `pbx/ports` and satisfied in `cmd/atsap-api` over `telephony/ports.CallService` and `CallStore`. This is the licensing-adapter pattern again. pbx→telephony would be a legal import (Tier 1 → Tier 0), but owning the shape keeps `pbx/application` testable with fakes and stops telephony's types spreading through pbx. `OriginateRequest` carries `BypassScreening`, and the only assignment in shipped code stays in `pbx/application`, which archtest's bypass-provenance scanner already enforces.

**Revision.** The originator half of this cannot be built as written. archtest's bypass-provenance scanner allows an assignment to `BypassScreening` in shipped code **only** in `pbx/application`, and a root adapter copying `OriginateRequest.BypassScreening` into telephony's `InitiateCallCommand` is exactly such an assignment, in `cmd/`. Widening the scanner to admit a pass-through would weaken the one invariant LLD-03 §10.1 calls a security property. So `pbx/application` takes `telephony/ports.CallService` and builds `InitiateCallCommand` itself, which keeps the sole assignment where the scanner already demands it. This is the legal Tier 1 → Tier 0 import that LLD-03 §4.1 describes and `.golangci.yml` states ("pbx-core may depend on telephony-core"). `CallStatus` stays a pbx-owned port with a root adapter over `CallStore`: it carries no bypass and keeps telephony's `Call` type out of pbx.

**D4 — Legs are persisted and counted lazily: `placed_call_legs`.**
Migration `0008` adds `placed_call_legs` (`call_id` PK, `tenant_id`, `extension_id`, `trunk_id` NULL for internal calls, `route_id`, `destination`, `emergency`, `created_at`, `released_at`), with RLS forced like every pbx table. This also puts the matched route on record for the call (AC-02.3/02.6).
Placement over a trunk: take a per-trunk advisory lock. Prune: for each unreleased leg on that trunk, ask `CallStatus`, and mark the leg released if its call has ended. Count what remains. If at the limit, try the next candidate. Otherwise insert the leg and commit, then originate. A failed origination releases the leg. Pruning costs at most `channel_limit` status reads, and only when a placement happens.
*Alternatives rejected:* an in-memory counter (lost on restart; undercounts, so it fails open). A NATS `call.terminated` consumer (no consumer infrastructure exists yet, and at-least-once delivery plus restart gaps need the same reconciliation anyway). A count over telephony's `calls` table (a cross-context table read).
**Emergency legs ignore the channel limit** (INV-01) but are still recorded, so they occupy capacity that others see.

**D5 — Health observer: report only carrier-attributable outcomes.**
A failed `Originate` on a trunk leg reports `ReportRouteHealth(0, 0)`, so the trunk goes Unhealthy and leaves selection. After a successful origination, a watcher goroutine polls `CallStatus` every 500 ms for up to 40 s (Presenting timeout plus margin). If the call is answered it reports `(200, 0)`. Latency is deliberately 0: answer time includes human ring time, and feeding it to D8's 2-second threshold would degrade every carrier whose callee took three seconds to pick up. If the call ends unanswered it reports nothing, because without a SIP cause the platform cannot tell a busy callee from a dead carrier. The watcher runs on `context.WithoutCancel` of the request context, so it keeps the caller's tenant for `ReportRouteHealth`. A process restart drops in-flight watchers, which loses observations but nothing else.

**D6 — HangupCall is tenant-checked by pbx, idempotent by outcome.**
`HangupCall(call_id)` first reads the leg row under RLS. Another tenant's call, or one never placed through `PlaceCall`, is `NOT_FOUND`. If `CallStatus` says the call has ended, it returns success with no effect. Otherwise it calls `Hangup`, treating telephony's already-terminated and not-found answers as success, and marks the leg released.

**D7 — Built-in emergency set.**
`pbx/domain.DefaultEmergencyNumbers = {112, 911, 999}`: the EU-wide, North American and UK numbers, all exact-matched. It is a constant, not configuration, so no deployment can empty it. `pbx-inbound-and-emergency` replaces it with the per-tenant seeded table (LLD-03 §5.1). Its test pins the three members so a deletion fails loudly.

**D8 — Dev-rig licence: ephemeral key, nothing committed.**
`api/cmd/dev-licence` creates or reuses a keypair under the gitignored `.dev-licence/`, prints the public-key hex, and signs a token in the `base64url(payload).base64url(sig)` format `ApplyLicenseKey` expects. The token has a generous edition: capacity 16, tenant and extension caps 0, so existing e2e is unaffected. `make dev-licence` writes `ATSAPBX_DEV_LICENCE_PUBKEY` and `ATSAPBX_LICENSE_TOKEN` into `.env`. `api/Dockerfile` and `Dockerfile.dev` gain an `ARG DEV_LICENCE_PUBKEY` feeding the existing `-ldflags -X …developmentPublicKeyHex`, empty by default. Compose and Tilt pass it from `.env`. `make images-push` refuses to run when the variable is set, so a dev-keyed image cannot be published. The existing default-build test (exactly one trusted key) still guards production.

**D9 — Proving calls at three tiers.**
- *Integration:* the application with fake originator and status, against real Postgres. Covers resolution order, limits (deterministic lock test, as in `license-provisioning-enforcement`), pruning, watcher reports, and hangup idempotency.
- *e2e* (`internal/pbx/e2e`, running app, public API): create an extension and register a Go sipgo UA with the generated secret, set to auto-answer. A Go sipgo UAS acts as the "carrier" on a host port. Create an IP-auth trunk whose host is the Docker gateway address Asterisk reaches the host by (resolved at runtime, overridable by env), and a route. Then `PlaceCall`, assert both UAs receive INVITEs and answer, the call reaches Active (`GetCall`), and the trunk stays healthy. `HangupCall` releases both legs and the count. Also: an internal extension-to-extension call, an emergency call under an exhausted licence, channel-limit refusal, and an unroutable refusal.
- *UAT* (`make uat-placement`, new script beside `uat-auto`): real baresip as the extension (account written from the API-created credentials) and real baresip as the carrier on port 5072, IP-auth. `PlaceCall` over the trunk, RTP tx/rx > 0 on both phones, then `HangupCall`. The rig is restored afterwards whatever the outcome. `make uat-auto` must stay green alongside it.

**D10 — `HangupCall` releases the legs on the engine.** *(Added at apply time, user-approved.)*
The UAT's real phones exposed a pre-existing `telephony-core` defect: `HangupCall` destroyed only the bridge, so the call read Terminated while both phones stayed connected with media flowing. Every earlier test ended calls from the phone side, and two e2e tests had quietly hung the channels up themselves after `HangupCall`, compensating for the defect without naming it. The fix captures the participants not yet Disconnected on entry. After `finalizeTermination` it calls the existing `MediaGateway.DestroyChannel` for each, addressed by the deterministic `atsa-part-<participant>` channel id they were originated with (now a single helper). Doing it after finalizing means the `ChannelDestroyed` events it triggers find the call already gone, rather than racing a second termination. The cost is that each of those events is logged at ERROR as "participant-left handler failed: call not found". `TestParticipantLeft_UnknownCall` pins that error as a contract, so quieting it is left as a recorded follow-up rather than widened into this change.

## Risks / Trade-offs

- [Both legs ring at once] → `InitiateCall` originates in parallel, so the far end can answer before the caller's phone. This is accepted for Phase A and stated in the API docs. Sequential "ring me first" needs a `telephony-core` change, which is out of scope here.
- [Pruning reads telephony state per placement] → bounded by `channel_limit` and only on trunk placements. Acceptable for Phase A volumes; a cached live count is a reporting-era optimisation.
- [Watcher goroutines are in-process] → bounded at 40 s each. Lost on restart, which loses only an observation. Never touches call state.
- [Docker gateway address varies by host] → resolved at runtime from the Asterisk container's network, with an env override. Both e2e and UAT fail fast with a named cause if unreachable.
- [Emergency with no matching route] → still unroutable, because the platform cannot invent a carrier. Recorded as a known limitation; emergency routing defaults belong to `pbx-inbound-and-emergency`.
- [Dev key mistaken for production] → empty by default, `images-push` guard, and the existing one-trusted-key test on default builds.

## Migration Plan

`0008_placed_call_legs` up/down (table + RLS + policy; down drops it). There is no backfill: no call has ever been placed through pbx. Rollback is down-migrate plus revert. The dev-licence step is opt-in per developer and has no production footprint.
