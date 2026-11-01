## MODIFIED Requirements

### Requirement: Inbound signalling is accepted only from a trunk's carrier address

The system SHALL recognise a call presented by a carrier as arriving on a trunk only when it comes from that trunk's carrier address. A trunk whose host is an IP address SHALL be recognised by that address immediately on create and update. A trunk whose host is a name SHALL be recognised by the addresses the platform last resolved that name to; the platform SHALL resolve it as soon as the trunk is created or its host changes, and again at every trunk check. Recognition SHALL NOT wait on name resolution: no trunk's host, resolvable or not, SHALL delay the recognition or setup of any call for another trunk or tenant. When a name that resolved before fails to resolve, the trunk SHALL keep its last resolved addresses; when it resolves to different addresses, those SHALL replace them; when the host changes, the previous host's addresses SHALL stop being recognised at once. A deleted trunk SHALL stop being recognised immediately.

#### Scenario: A carrier's call is recognised as its trunk

- **WHEN** a call arrives from a trunk's configured IP address
- **THEN** it is attributed to that trunk and its tenant

#### Scenario: A carrier configured by name is recognised once resolved

- **WHEN** a trunk is created with a host name that resolves
- **AND** its first check has completed
- **THEN** a call arriving from an address that name resolved to is attributed to that trunk and its tenant

#### Scenario: An unresolvable host stalls no one

- **WHEN** one tenant has a trunk whose host name does not resolve
- **THEN** that trunk is accepted and reported `Unhealthy` with the reason "host not found"
- **AND** it recognises no inbound call
- **AND** calls for every other trunk and tenant are recognised and set up without waiting on that name

#### Scenario: A brief resolution failure keeps a carrier recognised

- **WHEN** a trunk's host name resolved before and now fails to resolve
- **THEN** calls from its last resolved addresses are still attributed to the trunk
- **AND** its health reports the failure

#### Scenario: A changed host stops recognising the old carrier

- **WHEN** a trunk's host is changed
- **THEN** calls from the previous host's addresses are no longer attributed to the trunk
- **AND** the new host is recognised as soon as it is an IP address or has resolved

#### Scenario: A deleted trunk's carrier is no longer recognised

- **WHEN** a trunk is deleted and its former carrier presents a call
- **THEN** the call is refused as from an unknown source
