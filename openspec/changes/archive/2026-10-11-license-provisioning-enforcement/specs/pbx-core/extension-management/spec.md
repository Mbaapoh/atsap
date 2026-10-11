## ADDED Requirements

### Requirement: Extension provisioning respects the installation's extension cap

The system SHALL refuse to create an extension once the installation already holds as many extensions as its entitlement allows. The cap is supplied to the creation operation through a port this context owns and never fetched from the licensing context. A cap of zero SHALL mean unlimited and disable the check. Reads, updates, deletions and registration state are never gated by the cap.

#### Scenario: An eleventh extension is refused on a ten-extension installation

- **WHEN** the installation's extension cap is 10 and ten extensions exist
- **THEN** creating another extension is refused with `FAILED_PRECONDITION` and an entitlement reason identifying the licence, not the caller's roles

#### Scenario: Zero cap means unlimited

- **WHEN** the installation's extension cap is 0
- **THEN** extension creation is never refused for entitlement, regardless of how many extensions exist

#### Scenario: Existing extensions survive a lowered cap

- **WHEN** the entitlement in force allows fewer extensions than already exist
- **THEN** no extension is removed, hidden or deprovisioned from the engine, and only new creation is refused
