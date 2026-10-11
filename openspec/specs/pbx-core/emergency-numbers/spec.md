## Purpose

Defines which dialled numbers are emergency numbers for a tenant, by the country it operates in, so that an emergency call is recognised on every path a call can take, whatever the jurisdiction — and guarantees no tenant can configure a real emergency number away.

## Requirements

### Requirement: A tenant's emergency set follows its emergency country

The system SHALL let an authorized caller set the tenant's emergency country (an ISO 3166-1 country code). The tenant's emergency set SHALL then contain: 112 (the international number, always); the national emergency numbers the platform's built-in catalogue lists for that country; and the numbers the tenant has added. Numbers that are emergency in other countries but not in the tenant's SHALL NOT be treated as emergency.

#### Scenario: A country's own numbers are emergency

- **WHEN** a tenant's emergency country is Cameroon
- **THEN** 112 and each number the catalogue lists for Cameroon are recognised as emergency for that tenant

#### Scenario: Another country's numbers are ordinary

- **WHEN** a tenant's emergency country is a country whose catalogue does not list 911
- **THEN** a call to 911 from that tenant is ordinary and subject to every check

#### Scenario: Changing the country takes effect on the next call

- **WHEN** a tenant changes its emergency country
- **THEN** the very next call is evaluated against the new country's numbers, on every origination path, and the change is audited

#### Scenario: A country outside the catalogue still has a floor

- **WHEN** a tenant sets a country the catalogue does not list
- **THEN** 112 and the tenant's own added numbers are emergency, and the response says the country has no built-in numbers so the tenant must enter them

#### Scenario: No country set is safe, not empty

- **WHEN** a tenant has not set an emergency country
- **THEN** 112, 911 and 999 are recognised as emergency, and the tenant's emergency profile reports that no country is set

### Requirement: Built-in numbers cannot be removed; a tenant's own numbers can

The system SHALL let an authorized caller add, list and delete the tenant's own emergency numbers. A number supplied by the built-in catalogue for the tenant's country, or 112, SHALL NOT be deletable. Every change is audited and takes effect on the next call.

#### Scenario: A tenant adds a local number the catalogue lacks

- **WHEN** a tenant adds a number to its emergency set
- **THEN** a call to that number from that tenant, by the API or a desk phone, bypasses capacity, compliance and suspension checks

#### Scenario: A built-in number cannot be deleted

- **WHEN** a caller attempts to delete 112 or a number the catalogue lists for the tenant's country
- **THEN** the request is refused and the number stays in the set

#### Scenario: A tenant's own number can be removed

- **WHEN** a caller deletes a number the tenant added
- **THEN** it is no longer emergency from the next call

#### Scenario: Another tenant's numbers do not apply

- **WHEN** one tenant adds an emergency number or sets a country
- **THEN** no other tenant's emergency set changes

### Requirement: An emergency call from a desk phone is never refused by a commercial control

The system SHALL place a call dialled on a registered phone to a number in the tenant's emergency set with capacity, compliance and suspension checks bypassed (INV-01), exactly as it does for a call placed through the API.

#### Scenario: A desk phone reaches a national emergency number with capacity exhausted

- **WHEN** a registered phone in a tenant whose country is Cameroon dials one of Cameroon's emergency numbers while licensed capacity is exhausted
- **THEN** the call leaves through a matching route and connects

#### Scenario: A near-emergency number from a desk phone is ordinary

- **WHEN** a registered phone dials digits that only begin with an emergency number
- **THEN** the call is ordinary and subject to every check
