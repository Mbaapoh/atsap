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
- [ ] 2.2 `TrunkProber`: a scheduled round over every tenant's trunks, bounded to 8, with rounds that never overlap; `CheckTrunk` after create and after a host or port change; `ATSAPBX_TRUNK_PROBE_INTERVAL`; wiring in `cmd`; D-74 in `docs/DECISIONS.md`.

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

## 3. The console shows it

- [ ] 3.1 The trunks list and the overview's trunk-health table show "Not checked yet", or the state with its reason and observation time.

  Verify:
  - component tests for every state and reason;
  - browser test (mocked);
  - integration on the rig: a trunk added in the console shows its real result, and the exposure scan stays clean;
  - `make console-check`.
- [ ] 3.2 UAT: in the A3–A5 journey, the carrier trunk the real baresip carrier answers reads "Healthy · Answering".

  Verify: `make console-uat` passes on the rig.

## 4. Close-out

- [ ] 4.1 Update:
  - LLD-03 (health section and document history);
  - `docs/PROGRESS.md` (the gap "health only from answered calls" closed; the DNS-stall gap kept);
  - every tier: Go unit, integration (on `atsapbx_test`), e2e, `make coverage-all`, console tiers, the real-phone UATs, `make docs`, `make proto`;
  - ratchet the floors.

  Verify: all pass, and the measured coverage is recorded.
