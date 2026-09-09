## Purpose

Counts concurrent channels against the installation's licensed
entitlement so that over-use is refused at call setup, while guaranteeing
that no call already in progress is ever affected and that no licence
state can stand between a caller and an emergency number.

## ADDED Requirements

### Requirement: Capacity is counted per installation and consulted before external contact

The system SHALL maintain a count of concurrent channels in use for the
installation and SHALL consult it during call Screening, before any
external contact is attempted.

The count SHALL be installation-scoped, not tenant-scoped: one licence
governs one installed instance regardless of how many tenants it serves.

The capacity check SHALL return a verdict only. It SHALL NOT return a SIP
response code, and it SHALL NOT decide whether a destination is exempt.

#### Scenario: A call is permitted while capacity remains

- **WHEN** a call setup is screened and channels in use are below the
  entitlement
- **THEN** the verdict is permitted
- **AND** the count of channels in use increases by one

#### Scenario: Two tenants draw on the same installation entitlement

- **GIVEN** an entitlement of 2 concurrent channels
- **WHEN** tenant A holds one active call and tenant B sets up a call
- **THEN** tenant B's call is permitted and the installation reaches 2 in
  use
- **AND** a third call from either tenant is refused

#### Scenario: The verdict carries no signalling decision

- **WHEN** any capacity verdict is produced
- **THEN** it states only whether the call is permitted and a reason code
- **AND** it names no SIP response code and no retry interval

### Requirement: Capacity is released when a call ends

The system SHALL decrease the count of channels in use when a call
terminates, for every route by which a call can terminate — normal
hangup, screening rejection, and failure during setup.

A completed call and a refused call SHALL both leave the count exactly as
they found it.

#### Scenario: A completed call returns its channel

- **GIVEN** an entitlement of 2 and both channels in use
- **WHEN** one of the two calls is hung up
- **AND** a new call setup is screened
- **THEN** the new call is permitted

#### Scenario: A refused call does not consume a channel

- **GIVEN** an entitlement that is fully consumed
- **WHEN** a further call setup is screened and refused
- **THEN** the count of channels in use is unchanged by the refusal

#### Scenario: A call failing after screening returns its channel

- **GIVEN** a call that passed screening and consumed a channel
- **WHEN** the call fails before it is answered and is terminated
- **THEN** the count of channels in use returns to its pre-call value

#### Scenario: The count does not drift under sustained load

- **WHEN** calls are set up and torn down repeatedly at ten times the
  expected setup rate
- **THEN** the count of channels in use returns to zero once all calls
  have ended
- **AND** no permitted call was refused while capacity was available

### Requirement: A bounded burst allowance absorbs busy periods before rejection

The system SHALL permit concurrent usage to exceed the licensed
entitlement by a bounded allowance for a bounded period, so that a busy
period does not immediately produce refusals.

Once the allowance is exhausted, further call setups SHALL be refused
with a reason code distinct from every other refusal reason, so that
over-capacity is separable from a compliance refusal or an entitlement
failure in telemetry.

#### Scenario: Usage just above entitlement is absorbed

- **GIVEN** usage has reached the licensed entitlement
- **WHEN** a further call setup arrives within the burst allowance and
  within its permitted window
- **THEN** the call is permitted

#### Scenario: Sustained overage is refused with its own reason

- **GIVEN** usage has been above the licensed entitlement for longer than
  the permitted window
- **WHEN** a further call setup is screened
- **THEN** the call is refused
- **AND** the reason code identifies over-capacity specifically

#### Scenario: Usage beyond the allowance is refused immediately

- **GIVEN** usage has reached entitlement plus the full burst allowance
- **WHEN** a further call setup is screened
- **THEN** the call is refused without waiting for any window to elapse

### Requirement: A call in progress is never affected by capacity

No capacity condition SHALL terminate, degrade, mute, or otherwise alter
a call that is already in progress. Capacity is enforced at setup only.

#### Scenario: Reaching capacity leaves active calls untouched

- **GIVEN** active calls holding the full entitlement
- **WHEN** further setups are refused for capacity
- **THEN** every active call remains active and audible for its full
  duration

#### Scenario: Lowering the entitlement below current usage drops nothing

- **GIVEN** active calls holding more channels than a newly applied
  entitlement allows
- **WHEN** the lower entitlement takes effect
- **THEN** no active call is terminated
- **AND** new setups are refused until usage falls below the entitlement

### Requirement: A capacity increase applies without interruption

The system SHALL apply an increased entitlement without restarting, and
without interrupting any call in progress.

#### Scenario: More capacity becomes usable promptly

- **GIVEN** setups are being refused because the entitlement is reached
- **WHEN** a larger entitlement takes effect
- **THEN** a subsequent call setup is permitted
- **AND** no call in progress was interrupted

### Requirement: Licensing can never refuse an emergency call

The capacity subsystem SHALL expose no state, verdict, or failure mode
capable of preventing an emergency call from proceeding, in every licence
state including valid, over-capacity, unverified, degraded, and tampered.

Emergency destinations are identified and exempted before the capacity
check is consulted; this capability's obligation is that no path through
it can block one.

#### Scenario: Emergency calls connect at full capacity

- **GIVEN** usage has exhausted the entitlement and the burst allowance
- **WHEN** an emergency call is placed
- **THEN** it proceeds

#### Scenario: Emergency calls connect while degraded

- **GIVEN** the installation is in a degraded licence state
- **WHEN** an emergency call is placed
- **THEN** it proceeds

#### Scenario: Emergency calls connect with no licence at all

- **GIVEN** the installation has never been licensed
- **WHEN** an emergency call is placed
- **THEN** it proceeds

### Requirement: A never-activated installation has no call path, and is not a degraded one

An installation with no entitlement on record SHALL be in a
pre-activation state: it SHALL refuse every call setup, and the refusal
SHALL carry a reason code identifying that no entitlement has been
applied — distinct from over-capacity, from expiry, and from tampering.

Absence of an entitlement SHALL NOT be treated as unlimited capacity, and
SHALL NOT be treated as the degraded floor. Administration SHALL remain
fully available so the installation can be activated from itself.

#### Scenario: A fresh installation refuses calls until activated

- **GIVEN** an installation with no entitlement on record
- **WHEN** a call setup is screened
- **THEN** it is refused
- **AND** the reason code identifies that no entitlement has been applied

#### Scenario: Absence is never unlimited

- **WHEN** no entitlement is on record
- **THEN** no capacity is available
- **AND** the state is never reported as unlimited or as valid

#### Scenario: Administration survives the absence of an entitlement

- **GIVEN** an installation with no entitlement on record
- **THEN** the administration surface remains available, and reports the
  installation identity needed to obtain an entitlement

#### Scenario: A degraded installation is not confused with a fresh one

- **GIVEN** an installation whose entitlement has expired
- **THEN** it permits calls up to the degraded floor
- **AND** its state is distinguishable from an installation that was
  never activated, which permits none

### Requirement: Non-channel entitlement limits are published, not enforced here

The entitlement in force SHALL carry the maximum number of extensions and
the maximum number of tenants permitted, alongside the channel capacity.

This capability SHALL NOT enforce those two limits. Extensions and
tenants are created by other parts of the system, and enforcement belongs
where creation happens. This capability's obligation is that the limits
are readable, correct for the current licence state, and correct for an
unlicensed installation.

Reducing an entitlement SHALL NOT remove, disable, or delete any
extension or tenant that already exists. Only new call capacity is
constrained.

#### Scenario: The entitlement states all three limits

- **WHEN** the entitlement in force is read
- **THEN** it states the channel capacity, the maximum extensions, and
  the maximum tenants

#### Scenario: The free entitlement publishes its caps

- **GIVEN** an installation activated with the free entitlement
- **WHEN** the entitlement in force is read
- **THEN** it states the free channel allowance, the free extension cap,
  and a single tenant

#### Scenario: Degrading leaves the provisioned estate intact

- **GIVEN** an installation with more extensions and tenants than a
  degraded entitlement permits
- **WHEN** the installation degrades
- **THEN** no extension and no tenant is removed or disabled
- **AND** the estate is unchanged when a valid licence is restored

### Requirement: Licence state is installation-scoped and carries no tenant data

Stored licence state SHALL be scoped to the installation and SHALL NOT
carry a tenant identifier or tenant-scoped access control, this being a
deliberate exception to the platform-wide rule that every table is
tenant-scoped.

The exception SHALL be asserted, so that it cannot later be mistaken for
an oversight and "corrected".

#### Scenario: The exception is proven rather than assumed

- **WHEN** the stored licence state is inspected
- **THEN** it carries no tenant identifier
- **AND** no tenant-scoped access control is enabled on it
- **AND** a test asserts both, failing if either is added

#### Scenario: No licence data is written to logs or telemetry

- **WHEN** a capacity verdict is produced or refused
- **THEN** the reason code and counts are recorded
- **AND** no licence key, signature, or fingerprint value appears in any
  log or telemetry record
