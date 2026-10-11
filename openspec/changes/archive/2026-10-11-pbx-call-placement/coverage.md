# Coverage — pbx-call-placement

Every scenario in both delta specs, mapped to what proves it. **Tiers:** U = unit, I = integration (real Postgres, fake telephony), W = wire (real service through `PbxHandler`), E = e2e (running licensed app, public API, live Asterisk, SIP agents), A = UAT (real baresip phones, RTP measured). Test names are Go test functions unless they say otherwise.

## pbx-core/call-placement

### A call is placed from an extension to a dialled destination through the public API

| Scenario | Proven by |
|---|---|
| An outbound call is placed over a trunk | **E** `TestPlaceCall_OutboundOverATrunkConnectsAndHangsUp`: Active with both legs answered, and the carrier receives `442071234567`. **A** `make uat-placement`: real phones, phone tx 307 / rx 306, carrier tx 306 / rx 307 RTP packets, 0 errors. **I** `TestPlaceCall_OutboundOverATrunk` (addresses, leg record) |
| A call to another extension stays internal | **E** `TestPlaceCall_InternalCallConnectsWithoutATrunk`. **I** `TestPlaceCall_InternalCallUsesNoTrunk` (a shadowing `1` route does not capture it). **U** `TestResolveDestination/internal wins over a shadowing route` |
| An unroutable destination is refused | **E** `TestPlaceCall_RefusalsThroughTheWire`. **W** `TestPlaceCallRPC_RefusalCodes` (FAILED_PRECONDITION). **I** `TestPlaceCall_Refusals` (nothing set up) |
| Another tenant's extension cannot place a call | **W** `TestPlaceCallRPC_RefusalCodes`: NOT_FOUND, byte-identical to a nonexistent extension. **I** `TestPlaceCall_Refusals` |
| An unauthorized caller cannot place a call | **W** `TestPlaceCallRPC_RefusalCodes` (PERMISSION_DENIED). **I** `TestPlaceCall_Refusals` |

### A call placed through the API can be ended through the API

| Scenario | Proven by |
|---|---|
| Hanging up ends the call | **E** `TestPlaceCall_OutboundOverATrunkConnectsAndHangsUp`: Terminated, **and both far ends receive a BYE**. **A** "both phones released by HangupCall". **U** `TestHangupCall_ReleasesEveryLegOnTheEngine`, `TestHangupCall_ReleasesRingingLegs`. **E** telephony `requireCallChannelsReleased` in the emergency and licence-transition suites. **I** `TestHangupCall` (channel freed) |
| Hanging up twice is harmless | **I** `TestHangupCall` (no second telephony call). **E** repeated `HangupCall` in the outbound test |
| Another tenant's call cannot be ended | **I** `TestHangupCall` (NOT_FOUND; an unplaced call too) |

### An emergency destination is never refused by a commercial control

| Scenario | Proven by |
|---|---|
| An emergency call connects with capacity exhausted | **E** `TestPlaceCall_EmergencyConnectsWithCapacityExhausted`: capacity is exhausted for real, by ringing calls, until the running app's Screening refuses an ordinary call; then `112` reaches Active. **I** `TestPlaceCall_EmergencyBypassesAndIgnoresTheLimit` |
| A near-emergency number is an ordinary call | **U** `TestResolveDestination/near-emergency is ordinary`, `TestValidateDestination` (`*112`, `112#`). **I** `1120` in `TestPlaceCall_EmergencyBypassesAndIgnoresTheLimit`. **W** `TestPlaceCallRPC_BypassComesOnlyFromTheDialPlan` (`1120`) |
| The wire cannot request a bypass | **W** `TestPlaceCallRPC_BypassComesOnlyFromTheDialPlan`: every request field filled and wire round-tripped; bypass only for `112`. Fault-injected. archtest `ScanBypassScreeningProvenance`, fault-injected in `pbx/rpc` |

### A trunk's outcome on a placed call is observed as its health

| Scenario | Proven by |
|---|---|
| An answered trunk leg keeps the trunk healthy | **I** `TestPlaceCall_AnsweredCallReportsHealthyAndUnansweredReportsNothing`: answered after 15 s of ringing, Degraded becomes Healthy |
| A trunk that fails to originate leaves selection | **I** `TestPlaceCall_OriginateFailureTakesTheTrunkOutOfSelection` (Unhealthy; next placement uses the other trunk) |
| An unanswered call does not blame the carrier | **I** `TestPlaceCall_AnsweredCallReportsHealthyAndUnansweredReportsNothing` (Degraded stays Degraded) |

### No engine identifier reaches a caller on the placement path

| Scenario | Proven by |
|---|---|
| Responses and errors are engine-free | **W** `TestPlaceCallRPC_NoEngineIdentifierReachesACaller`, including an internal failure whose text names `PJSIP/…@t_…`, `stasis-in` and `transport-udp`. Fault-injected |

## pbx-core/trunk-routing (delta)

### A trunk's channel limit caps its concurrent placed calls

| Scenario | Proven by |
|---|---|
| A full trunk is passed over | **I** `TestPlaceCall_ChannelLimitPassesOverAndFrees` |
| No room anywhere refuses the call | **I** `TestPlaceCall_ChannelLimitPassesOverAndFrees`. **W** `TestPlaceCallRPC_RefusalCodes` ("trunk capacity"). **E** `TestPlaceCall_RefusalsThroughTheWire` (`channel_limit` 1, a ringing call holding it) |
| An ended call frees its channel | **I** `TestPlaceCall_ChannelLimitPassesOverAndFrees`: the far end ends the call, no hang-up through pbx, and the next count notices. Fault-injected (pruning removed). **I** `TestHangupCall`. **E** refusals test (hang-up frees the channel) |
| Racing placements cannot exceed the limit | **I** `TestPlaceCall_RacingPlacementsCannotShareTheLastChannel`, deterministic and lock-held. Fault-injected (lock removed) |

## Gaps (G-series)

- **G1 — No `Degraded` from latency.** Telephony records only `AnsweredAt`, and that includes ring time. A slow-carrier signal needs post-dial-delay capture in `telephony-core`. Design D5 non-goal.
- **G2 — Unanswered calls are not attributed.** Without a SIP cause code, a busy callee and a dead carrier look the same, so neither changes health. Lands with cause-code capture.
- **G3 — ERROR noise after every API hang-up.** The `ChannelDestroyed` events that D10's teardown triggers reach a call already removed and log "participant-left handler failed: call not found". Harmless but misleading. `TestParticipantLeft_UnknownCall` pins the error as a contract, so quieting it is a follow-up `telephony-core` change.
- **G4 — Outbound-class toll-fraud enforcement deferred** to its own change. It needs a tenant country the platform does not hold.
- **G5 — Built-in emergency set** (112/911/999) until `pbx-inbound-and-emergency` lands the per-tenant table. An emergency number with no matching route is still unroutable.
- **G6 — Both legs ring at once.** `InitiateCall` originates in parallel. "Ring me first" needs a `telephony-core` change.
- **G7 — e2e/UAT depend on host firewall ports.** On the reference host `ufw` admits the Docker bridge only on UDP 5070–5071 and 18000–18039. The suites use those ports (env-overridable) and the reason is written in both files. A host that blocks them fails with calls that never answer.
- **G8 — No live e2e of a trunk failing to originate.** Proven at integration with fake telephony only. A live version needs an engine endpoint that fails allocation on demand.
