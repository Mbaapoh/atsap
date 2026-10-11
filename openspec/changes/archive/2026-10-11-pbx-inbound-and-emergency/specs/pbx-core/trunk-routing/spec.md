## ADDED Requirements

### Requirement: Inbound signalling is accepted only from a trunk's carrier address

The system SHALL recognise a call presented by a carrier as arriving on a trunk only when it comes from that trunk's configured host. The recognition SHALL follow the trunk's configuration immediately on create, update and delete, like the rest of its engine state.

#### Scenario: A carrier's call is recognised as its trunk

- **WHEN** a call arrives from a trunk's configured host
- **THEN** it is attributed to that trunk and its tenant

#### Scenario: A deleted trunk's carrier is no longer recognised

- **WHEN** a trunk is deleted and its former carrier presents a call
- **THEN** the call is refused as from an unknown source
