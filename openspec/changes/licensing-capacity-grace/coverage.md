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
| Reaching capacity leaves active calls untouched | `e2e.TestNoActiveCallIsDroppedByAnyLicenceTransition/over_capacity` |
| Lowering the entitlement below current usage drops nothing | same, `degraded` and `tampered` stages |
| More capacity becomes usable promptly | same, `a capacity increase applies without a restart` (AC-06.8) |
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
| Degradation does not drop calls in progress | `e2e.TestNoActiveCallIsDroppedByAnyLicenceTransition/degraded` |
| The system can be repaired from itself | Observed live (administration available while in Setup) |
| Recovery from degraded / does not disturb active calls | `postgres.TestGraceEndToEndAgainstARealLicence`; active-call half by the e2e capacity stage |
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

### G1 — CLOSED. INV-03 is proven.

All five stages pass against live Asterisk, with a real call up
throughout: **over capacity**, **entitlement unverified**, **degraded**,
**tampered**, and **a capacity increase applying without a restart**
(AC-06.8). LLD-08 DoD 4 is met.

Getting there uncovered four defects, none of them in the assertions:

1. **`Store.Load` picked arbitrarily between rows.** `instance_id` as the
   primary key stopped one instance being recorded twice but not a second
   instance joining the first, and `LIMIT 1` then chose. A service
   holding one key read a row signed by another and reported a perfectly
   good licence as TAMPERED — silent, and presenting as a security event.
   Fixed with a singleton unique index and a replacing write.
2. **The entitlement cache never re-evaluated time**, so a running
   installation would never degrade.
3. **`ApplyGrace` promoted an expired licence** back to Unverified with
   its capacity restored.
4. **Five ARI client defects**, including an unbounded HTTP client and a
   goroutine leaked per reconnect.

Each has a regression test. The e2e earned its keep several times over
before it went green.

**Residual, and it is the rig rather than this change.** Asterisk in the
dev container accepts a limited number of Stasis application
registrations per lifetime; after one full e2e run they are exhausted and
the next run cannot register. This affects the untouched
`TestWalkingSkeleton` identically, so it is not introduced here. Run
`mise run rig:restore` before the e2e tier — the documented step, which
now restarts Asterisk for exactly this reason (docs/WORKFLOW.md §4).

Two things were tried and rejected on evidence rather than taste. Giving
each test its own Stasis app name did not help: the limit is on
registrations, not names. Performing the RFC 6455 closing handshake made
it **worse** — teardown slowed enough that the next registration arrived
before Asterisk released the previous one, taking a suite that passed
three tests down to one. Both are recorded where the code is, so neither
is re-attempted.

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
