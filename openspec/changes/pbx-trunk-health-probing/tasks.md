## 1. Health that says what it knows

- [x] 1.1 Domain and storage:
  - `HealthUnknown` and the five reasons;
  - `ObserveProbe`, and `ObserveHealth` returning a reason too;
  - migration 0012 (UNKNOWN check, default and backfill, `health_reason`, `health_observed_at`);
  - `SetHealth` writes all three, guarded by host and port;
  - `CreateTrunk` starts `UNKNOWN`.

  Verify:
  - unit: the mapping table; selection treats `UNKNOWN` as `HEALTHY`;
  - integration: a new trunk is UNKNOWN with no time; a guarded write for a changed address or a deleted trunk is dropped; the migration's down and up round-trip;
  - `make test`, `make lint`.
- [x] 1.2 API: `TRUNK_HEALTH_UNKNOWN`, `TrunkHealthReason` and `health_observed_at` on `Trunk`, mapped in the RPC layer.

  Verify:
  - unit: the RPC mapping for every state and reason;
  - `make proto` (`buf breaking`);
  - the regenerated console clients typecheck;
  - Go tiers and `make console-check`.

  *(1.1 and 1.2 done together on 2026-10-14. They form one commit because the wire test cannot pass on the new domain without the new API value.)*

  *Domain:*
  - `HealthUnknown`, the five `HealthReason`s and `HealthObservation`;
  - `ObserveProbe` and `ProbeSlowThreshold` (1 s);
  - `ObserveHealth` now returns a reason.

  *Store and service:*
  - Migration 0012 adds `UNKNOWN` (also the default and the backfill), `health_reason` and `health_observed_at`.
  - `SetHealth` takes an observation. The new `SetCheckedHealth` writes only while the host and port are unchanged.
  - Both refuse "Unknown", or an observation without a reason or time.
  - `CreateTrunk` starts `UNKNOWN`.
  - `TrunkView` carries the reason and time.

  *API:* `TRUNK_HEALTH_UNKNOWN`, `TrunkHealthReason`, `health_reason` and `health_observed_at`, mapped in the RPC layer.

  *Console:* "Not checked yet" was added, which is needed for the console to typecheck.

  Verified:
  - unit: `TestObserveHealth`, `TestObserveProbe` (nine cases), `TestTrunkHealthAndReasonValues`, `TestSelectRoutes_UncheckedTrunkIsSelectedLikeHealthy`, the property test with Unknown in the mix, and `TestToProtoTrunk_Health` for every state and reason;
  - integration (`atsapbx_test`): `TestTrunkStore_CheckedHealthIsGuardedByAddress` (a changed host or port and a deleted trunk drop the result), `TestTrunkStore_RefusesWhatIsNotAnObservation`, a new trunk is UNKNOWN with no time, and the tenant's own report records its reason and time;
  - the wire lifecycle and e2e tests now expect `TRUNK_HEALTH_UNKNOWN`;
  - `make proto`, lint, the Go unit, integration and e2e tiers, and `make console-check` (395 / 24 / 24 / 3) all pass.

  **Found while testing:** the first migration's backfill ran under FORCE RLS as the app role and changed **0 rows**, silently leaving every existing trunk HEALTHY. Its down migration failed outright on the new check constraint. Both directions now lift RLS for their own transaction, as 0010 does, and both were verified as the app role. The dev rig had already applied the first version, so its one never-observed trunk was corrected by hand. Nothing else had applied it, because it was never pushed.)*

## 2. The check

- [x] 2.1 `internal/pbx/acl/sipprobe`: OPTIONS over UDP with resolve, retransmit, Call-ID matching and RTT.

  Verify with unit tests against a local UDP responder:
  - 200;
  - 403 (answering);
  - 503;
  - 100 then 200;
  - a stray packet with another Call-ID ignored;
  - silence (no response within the budget);
  - retransmission observed;
  - a host that does not resolve (`.invalid`).

  Lint clean.

  *(done 2026-10-14: `sipprobe.Prober.Probe(ctx, host, port)` returns a `Result` (outcome, status, RTT) and never an error.
  - **Resolve:** IP literals are used as given. Names are resolved with a 2 s limit and IPv4 preferred. DNS "not found" gives HostNotFound; any other resolver failure gives NoResponse, because it says nothing about the host.
  - **Request:** an RFC 3261 OPTIONS with a fresh branch, tag and Call-ID, `rport`, and Content-Length 0.
  - **Retransmit:** at T1 = 500 ms, doubling, within a 4 s budget.
  - **Read:** provisional responses and other calls' packets are ignored. An ICMP refusal ends the check at once.
  - It uses the standard library only.

  Verified:
  - 11 unit tests against a loopback responder: 200, 403, 404 and 503 are answers; a well-formed request; 100 then 200; a stray Call-ID ignored; silence after at least 3 sends; an answer to a retransmission; a refused port; NXDOMAIN; a resolver failure is not "host not found"; cancellation. All pass with `-race`.
  - **Fault injection:** accepting any Call-ID fails `IgnoresAnotherCallsResponse`; disabling retransmission fails the silence and the retransmission tests.
  - **Integration `TestProbe_RealPeers`:** the rig's real Asterisk answered **401 in 311 ms**; `no-such-carrier.invalid` gave HostNotFound through the real resolver; `192.0.2.1` gave NoResponse after exactly the 4 s budget.
  - Lint, Go unit and integration pass. The package is not wired yet (task 2.2), so e2e and the console are unaffected.)*
- [x] 2.2 `TrunkProber`: a scheduled round over every tenant's trunks, bounded to 8, with rounds that never overlap; `CheckTrunk` after create and after a host or port change; `ATSAPBX_TRUNK_PROBE_INTERVAL`; wiring in `cmd`; D-74 in `docs/DECISIONS.md`.

  Verify:
  - integration with a fake probe:
    - a created trunk is checked once;
    - an update without an address change is not;
    - a round covers two tenants;
    - a result for a stale address is dropped;
  - e2e on the rig through the public API:
    - a trunk to the engine (`asterisk:5060`) becomes Healthy · Answering within seconds;
    - `no-such-carrier.invalid` becomes Unhealthy · Host not found;
    - `192.0.2.1` (TEST-NET) becomes Unhealthy · No response;
    - changing the host re-checks at once;
  - Go tiers.

  *(done 2026-10-14: `TrunkProber` runs a scheduled round over every tenant's trunks, at most 8 at a time, with rounds that never overlap. `CheckTrunk` runs in the background after create, and after an update only when the host or port changed. Checked health is written through the address-guarded `SetCheckedHealth`.
  - The store's `Update` returns a re-addressed trunk to UNKNOWN, because its old observation was of another address. The RPC response says so too.
  - `listTenants` is shared with the reconciler.
  - `ATSAPBX_TRUNK_PROBE_INTERVAL` defaults to 30 s: at least 5 s, or 0 for off, validated at start.
  - Wired in `main`, with `*sipprobe.Prober` as the `CarrierProbe` (its `Result` is `ports.CarrierCheck`).
  - D-74 was recorded in `f07893c`.

  **Two findings on the rig, both fixed:**
  - **The A3–A5 UAT failed: the call was refused as "unroutable".** Its carrier phone starts after the trunk is created, so the first check found nobody, the trunk read Unhealthy, and selection skipped it. A check can be wrong, for example with a carrier that ignores OPTIONS from the platform's address. So an Unhealthy trunk is now **the last resort**: skipped while another can carry the call, and tried when none can (design D7). The spec, the selection doc, the example test and the property invariant (2,000 random cases) were updated, and the property test was split into helpers to stay under gocognit 30.
  - **A mistyped single-label host (`localhosts2`) read "No response".** Docker's DNS returns a temporary failure, not NXDOMAIN, for such names. Any resolution failure is now "Host not found".

  Verified:
  - config unit tests;
  - integration with a fake carrier (under `-race`): a created trunk is checked at once and the RPC does not wait; a new name is not re-checked but a new address is, going UNKNOWN then the new result; a stale check is dropped; a round covers two tenants. Removing the address guard fails the stale-check test.
  - **e2e on the rig** (`TestTrunkHealth_IsWhatTheCarrierSays`): `asterisk:5060` became Healthy · Answering; `no-such-carrier.invalid` became Unhealthy · Host not found; `192.0.2.1` became Unhealthy · No response; fixing the host became UNKNOWN in the response, then Healthy. All in 4.4 s.
  - The owner's trunk (`localhosts2`) now reads Unhealthy · Host not found.
  - The Go unit, integration, e2e and lint tiers pass, `make console-check` passes (395 / 24 / 24 / 3), and `uat-inbound`, `uat-placement`, `uat-auto` and `make docs` pass.)*

## 3. The console shows it

- [x] 3.1 The trunks list and the overview's trunk-health table show "Not checked yet", or the state with its reason and observation time.

  Verify:
  - component tests for every state and reason;
  - browser test (mocked);
  - integration on the rig: a trunk added in the console shows its real result, and the exposure scan stays clean;
  - `make console-check`.

  *(done 2026-10-14:
  - **`TrunkHealthCell`:** the badge, then the reason in words (Answering, Slow to respond, Host not found, No response, Carrier error) and "checked <time>", or the date and time when the check was not today. "Not checked yet" shows neither. It is used on Trunks & routes and in the overview's trunk-health table.
  - **Adapters:** they carry `healthReason` and `healthObservedAt` as a `Date`.
  - **Refresh:** both lists re-read health every 2 s while a trunk awaits its first check, otherwise every 30 s (`healthRefreshMs`), so a new trunk's result appears without a reload.
  - **Copy:** the route dialog's ordering text now says an unhealthy trunk is used only when no other can take the call (design D7).

  **Found:** at 390 px, every admin table with row actions made the whole page scroll sideways (548 px on Trunks & routes). The visually-hidden "Actions" header was absolutely positioned outside the scroll box. `.table-scroll` is now `position: relative`. New 390 px tests for Trunks, Extensions and Users all failed without the fix.

  Verified:
  - component tests: every state, reason and time; today versus an older day; the refresh rule; the overview's detail;
  - browser (mocked) at 390 and 1280 px, with no page overflow;
  - **rig:** a trunk added in the console to `asterisk` reads "Healthy · Answering · checked …" and one to `no-such-carrier.invalid` reads "Unhealthy · Host not found · checked …", within seconds and without a reload;
  - `make console-check`: component 398, e2e 29, integration 25 (the exposure scan included), UAT 3, audit 0, secrets clean; oxlint clean.)*
- [x] 3.2 UAT: in the A3–A5 journey, the carrier trunk the real baresip carrier answers reads "Healthy · Answering".

  Verify: `make console-uat` passes on the rig.

  *(done 2026-10-14: the A3–A5 UAT now starts the carrier phone before adding its trunk, as a real carrier already exists, then waits for the trunk row to read "Healthy · Answering · checked …".
  - The real baresip carrier answered the platform's OPTIONS check from the app's container, through the host firewall's 5071 rule.
  - The console showed the result within seconds, without a reload.
  - The rest of the journey (registration, route, DID, emergency, call, history) is unchanged.

  `make console-uat` passes (3/3), and so does `make console-check`.)*

## 4. Close-out

- [ ] 4.1 Update:
  - LLD-03 (health section and document history);
  - `docs/PROGRESS.md` (the gap "health only from answered calls" closed; the DNS-stall gap kept);
  - every tier: Go unit, integration (on `atsapbx_test`), e2e, `make coverage-all`, console tiers, the real-phone UATs, `make docs`, `make proto`;
  - ratchet the floors.

  Verify: all pass, and the measured coverage is recorded.
