## ADDED Requirements

### Requirement: An extension may have one owner

The system SHALL let an authorized administrator assign an active
membership of the same tenant as the owner of an extension. An extension
SHALL have at most one owner; a membership MAY own several extensions.
Unassigning SHALL remove the owner, and deleting the extension SHALL
remove its assignment. Assigning a disabled membership or a member of
another tenant SHALL be refused.

#### Scenario: An administrator assigns an owner
- **WHEN** an administrator assigns an active member to an extension
- **THEN** extension reads return that member's id, name and email as the owner

#### Scenario: A disabled or foreign member is refused
- **WHEN** the chosen membership is disabled or belongs to another tenant
- **THEN** the assignment is refused and the existing owner is unchanged

#### Scenario: Unassigned and deleted extensions
- **WHEN** an extension has no owner, or is deleted
- **THEN** the line works without an owner and a deleted extension leaves no assignment
