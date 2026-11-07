## MODIFIED Requirements

### Requirement: A DID can route through a time-based flow

The system SHALL let a tenant attach a DID to a flow whose revision
starts at an entry node. A `schedule` node SHALL choose its open, closed
or holiday branch from a named schedule evaluated in the schedule's IANA
time zone (the tenant default unless set): several ranges per weekday,
ranges that may cross midnight, and the tenant's holidays matched on the
local date. A DID SHALL NOT be both a flow and a direct inbound number.

#### Scenario: Open hours ring the open destination
- **WHEN** a call arrives inside one of the schedule's ranges for the local weekday and the local date is not a holiday
- **THEN** the call follows the open branch

#### Scenario: Weekends are closed unless a range covers them
- **WHEN** a schedule has Monday–Friday ranges only and a call arrives on a local Saturday
- **THEN** the call follows the closed branch

#### Scenario: The time zone decides the local time
- **WHEN** a schedule in `Africa/Douala` is open 08:00–17:00 and a call arrives at 07:30 UTC (08:30 local)
- **THEN** the call follows the open branch

#### Scenario: Closed hours and holidays use the closed destination
- **WHEN** the call arrives outside every range, or the local date is a tenant holiday
- **THEN** the call follows the closed branch, or the holiday branch for a holiday when one is set

#### Scenario: A closed flow with no destination refuses
- **WHEN** the branch taken ends without a destination
- **THEN** the call is refused rather than left silent

### Requirement: A flow can play a prompt and route on a digit

The system SHALL let a `menu` node define a prompt, options for the keys
0–9, `*` and `#`, a timeout, a retry count (default 2), optional direct
dial, and no-input/invalid branches. It SHALL answer the caller, play the
prompt, and follow the option for the key pressed, stopping the prompt
and timer. Invalid input or a timeout SHALL replay the prompt until the
retries are used, then follow the fallback branch.

#### Scenario: A digit routes the caller
- **WHEN** a caller presses a configured key
- **THEN** the prompt stops and the call follows that option's node

#### Scenario: The menu re-prompts before falling back
- **WHEN** a caller presses nothing for the timeout and retries remain
- **THEN** the prompt plays again, and only after the last retry does the call follow the no-input branch, or end with `ivr_no_input` when none is set

#### Scenario: No input or invalid input uses its fallback
- **WHEN** the retries are used up without a valid key
- **THEN** the call follows the no-input or invalid branch, or ends with `ivr_no_input`/`ivr_invalid_input` when none is set

#### Scenario: Direct dial reaches an extension
- **WHEN** direct dial is on and the caller keys a tenant extension number
- **THEN** the call rings that extension

#### Scenario: Closed/holiday never enters the menu
- **WHEN** the schedule branch taken is closed or holiday
- **THEN** no menu prompt on the open branch is played

### Requirement: Menu revisions are validated before publish

The system SHALL refuse to publish or activate a revision that has a
missing entry, an unreachable node, a reference to a missing node, a
cycle that can be taken without caller input, menu nesting deeper than
five (counting flow hops), an unknown or not-yet-available destination
kind, a missing extension, flow or schedule, or an external number the
tenant's outbound policy refuses. Each refusal SHALL name the node. A
published revision SHALL be immutable and a live call SHALL keep the
revision it started on.

#### Scenario: A bad destination is refused
- **WHEN** an administrator publishes an option to a missing extension
- **THEN** the publish is refused naming the node, and the previous revision remains active

#### Scenario: A reserved destination kind is refused
- **WHEN** a revision routes to a `queue` before queues exist
- **THEN** the publish is refused

#### Scenario: Too deep a menu is refused
- **WHEN** menus nest six levels deep
- **THEN** the publish is refused

## ADDED Requirements

### Requirement: Flow nodes route to typed destinations

The system SHALL route a `goto` node to a typed destination: an
extension, another flow (entering its active revision), an external
number (placed under the tenant's outbound class and fraud guard), or
hang-up. Queue, ring group and voicemail kinds SHALL be refused until
their capability exists.

#### Scenario: A menu option enters a second-level flow
- **WHEN** a caller selects an option whose destination is another flow
- **THEN** the call enters that flow's active revision at its entry node

#### Scenario: An external destination obeys the outbound policy
- **WHEN** a destination is an international number and the tenant may not dial it
- **THEN** the publish is refused

### Requirement: Schedules can be overridden by hand

The system SHALL let an administrator force a schedule open or closed,
either until cleared or until the next scheduled transition, and SHALL
audit every change.

#### Scenario: Closed until the next transition
- **WHEN** an administrator sets "closed until next" during open hours
- **THEN** calls follow the closed branch until the next scheduled opening, after which the schedule applies again

### Requirement: Revisions written before the graph keep routing identically

The system SHALL read a revision published before this change as the
equivalent graph, never rewriting it, so that it routes exactly as before
and rolling back to it is unchanged.

#### Scenario: Rolling back to an old revision
- **WHEN** an administrator activates a revision published with a single UTC window
- **THEN** calls route on that window on every day, exactly as before

### Requirement: An extension used by a flow cannot be deleted silently

The system SHALL refuse to delete an extension that an active flow
revision routes to, naming the flows.

#### Scenario: Deleting an extension a flow uses
- **WHEN** an administrator deletes an extension that a published menu option rings
- **THEN** the deletion is refused and the response names the flow
