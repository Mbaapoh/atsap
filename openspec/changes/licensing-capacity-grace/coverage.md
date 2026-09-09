# Coverage — licensing-capacity-grace

Scenario → test map for the four delta specs, and the gaps, named.

A gap here is not a to-do list item. It is a scenario this change
specified and did **not** prove, recorded so it cannot be mistaken for
covered by anyone reading the ticks in `tasks.md`.

## `licensing/capacity-enforcement`

| Scenario | Proven by |
|---|---|
| A call is permitted while capacity remains | `domain.TestReserveWithinEntitlement` |
| Two tenants draw on the same installation entitlement | `postgres.TestCapacityIsInstallationScopedNotTenantScoped` (integration) |
| The verdict carries no signalling decision | `domain.CapacityVerdict` has no SIP field; asserted by construction |
| A completed call returns its channel | `application.TestCapacityIsReleasedOnEveryTerminationRoute/normal_hangup` |
| A refused call does not consume a channel | `domain.TestRefusedCallConsumesNothing`, `application.TestScreenedOutCallReleasesNothing` |
| A call failing after screening returns its channel | `application.TestCapacityIsReleasedOnEveryTerminationRoute/last_participant_leaves` |
| The count does not drift under sustained load | `domain.TestCounterUnderRace` (10×, `-race`) |
| Usage just above entitlement is absorbed | `domain.TestBurstAllowance`, `postgres.TestBurstBehaviourAgainstARealLicence` |
| Sustained overage refused with its own reason | same, plus `domain.TestRefusalReasonsAreDistinct` |
| Usage beyond the allowance is refused immediately | `domain.TestBurstAllowance/refuses_immediately_beyond_the_full_allowance` |
| Reaching capacity leaves active calls untouched | **GAP G1** |
| Lowering the entitlement below current usage drops nothing | **GAP G1** |
| More capacity becomes usable promptly | **GAP G1** (written, skipped) |
| Emergency calls connect at full capacity | **GAP G2** |
| Emergency calls connect while degraded | **GAP G2** |
| Emergency calls connect with no licence at all | **GAP G2** |
| A fresh installation refuses calls until activated | `application.TestSetupRefusesCallsWithTheNotActivatedReason` |
| Absence is never unlimited | `domain.TestZeroEntitlementPermitsNothing`, `application.TestNoLicenceIsSetupNotAFloor` |
| Administration survives the absence of an entitlement | Observed live: the app boots reporting `state=Setup channels=0` and serves HTTP |
| A degraded installation is not confused with a fresh one | `domain.TestSetupIsNotDegraded` |
| The entitlement states all three limits | `domain.TestFreeTierValues` |
| The free entitlement publishes its caps | `domain.TestFreeTierValues` |
| Degrading leaves the provisioned estate intact | `domain.TestDegradingDoesNotShrinkTheEstate` |
| The exception is proven rather than assumed | `postgres.TestLicensingSchema_IsInstallationScopedNotTenantScoped` (fault-injected) |
| No licence data in logs or telemetry | `postgres.TestNoLicenceMaterialInLogsOrErrors` (fault-injected) |

## `licensing/entitlement-grace`

| Scenario | Proven by |
|---|---|
| Confirmation runs on its own schedule | `application.TestVerifyDailyEntitlement` |
| An unreachable entitlement service does not affect calls | `application.TestConfirmationFailureDoesNotAffectCallSetup` |
| A slow entitlement service does not affect calls | same — confirmation is never in the call path by construction |
| Full function on the first day / throughout the grace period | `domain.TestApplyGrace_FullFunctionThroughoutTheWindow`, `postgres.TestGraceEndToEndAgainstARealLicence` |
| No warning on the first day / warnings from the second | `domain.TestWarningTiming` |
| Capacity reduces once the grace period elapses | `postgres.TestGraceEndToEndAgainstARealLicence/day_8` |
| Degradation never reaches zero | `domain.TestDegradedNeverDisables` |
| Degradation does not drop calls in progress | **GAP G1** |
| The system can be repaired from itself | Observed live (administration available while in Setup) |
| Recovery from degraded / does not disturb active calls | `postgres.TestGraceEndToEndAgainstARealLicence` (recovery); active-call half is **GAP G1** |
| The same elapsed time yields the same state | `domain.TestGraceStateDependsOnlyOnElapsedTime` |
| Repeated failures do not accelerate degradation | same |
| A restart does not reset the grace period | same, and `application.TestApplyLicenseKeyIsIdempotent` |

## `licensing/license-token`

Every scenario is proven. Verification, the trusted key set, the storage
rule and the activation path are covered by `domain.TestVerifyToken`,
`TestSplitToken`, `TestTrustedKeys_*`, `TestNoExportedParseWithoutVerify`,
`application.TestApplyLicenseKey*`, and
`postgres.TestEditingClaimColumnsChangesNoEntitlementDecision` /
`TestBreakingTheSignatureDegradesRatherThanHonours` — the last two
fault-injected against the pre-D-53 design.

## `telephony-core/call-lifecycle` (modified)

| Scenario | Proven by |
|---|---|
| Both verdicts permitted / a verdict is not permitted | pre-existing, unchanged |
| A completed call releases its reserved channel | `application.TestCapacityIsReleasedOnEveryTerminationRoute` |
| A screened-out call releases nothing it did not reserve | `application.TestScreenedOutCallReleasesNothing` |
| A call failing after Screening releases its channel | `application.TestCapacityIsReleasedOnEveryTerminationRoute` |
| Repeated termination does not release twice | same, `terminating_twice_releases_once` (fault-injected both directions) |
| Release is identified by the call, not counted blindly | `domain.TestReleaseIsIdempotent`, `TestReleaseOfAnUnknownCallDoesNothing` |

---

## Gaps

### G1 — INV-03 is not proven end to end

**Scenarios:** no active call dropped by any licence transition; capacity
increase without interrupting a call in progress.

**Status:** the tests are written
(`internal/telephony/e2e/licence_transitions_test.go`) and **skipped**.
On a healthy rig the first stage passed — a refusal at capacity left a
live call untouched — so the mechanism works. The remaining stages have
never been observed.

**Why it is blocked, and it is not licensing:**

`waitForStasisApp` uses `http.DefaultClient`, which has no timeout, so
its own 10-second deadline cannot fire while a request is blocked inside
`Do`. When Asterisk accepts a connection and does not answer, a 10s guard
becomes a hang bounded only by the test context.

Asterisk stops answering because of a feedback loop: when the app
container's ARI event stream fails, it retries forever on a backoff, and
those retries consume Asterisk's HTTP sessions, which blocks every other
caller including the tests. Restarting Asterisk clears it; running e2e
re-enters it. The same fault took down the untouched `TestWalkingSkeleton`
on 2026-09-09, which is how it was identified.

**What must land first,** neither of it licensing work:

1. A timeout on the e2e HTTP client, so a stalled Asterisk fails in ten
   seconds naming what stalled instead of hanging.
2. A bound on the app's ARI reconnect storm, so a failing stream degrades
   one container instead of the whole rig.

**LLD-08 DoD 4 is therefore unmet.** INV-03 — "no active call is dropped
by any licence state" — is the invariant this design is arranged around,
and nothing else in this change proves it.

### G2 — INV-01 cannot be proven here at all

**Scenarios:** emergency calls connect at full capacity, while degraded,
and with no licence.

**Status:** not written, deliberately.

**Why:** there is no emergency bypass to test. Identifying an emergency
destination and routing around Screening is `pbx-core`'s work
(LLD-08 §1 places it explicitly out of scope; LLD-03 owns it). A test
asserting "an emergency call connects" would today be asserting that an
ordinary call connects, which proves nothing about the invariant and
would read as covered.

**What licensing does guarantee, and it is weaker than INV-01:** this
context returns a verdict and performs no enforcement, so it has no
mechanism by which to block anything. That is a property of the design
rather than an assertion, and it is not a substitute.

**Owner:** `pbx-inbound-and-emergency` (LLD-03). INV-01 is proven there,
with licensing in a refusing state, or it is not proven at all.
