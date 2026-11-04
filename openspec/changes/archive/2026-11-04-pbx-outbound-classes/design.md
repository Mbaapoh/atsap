# Design — outbound destination classes

## D1. Three classes, ranked

`INTERNAL` reaches extensions only; `NATIONAL` adds trunk destinations
dialled without a leading `+`; `INTERNATIONAL` adds every destination.
Ranking makes the check one comparison and keeps the rule obvious.

## D2. The dialled form decides

E.164 reserves the leading `+` for international form. A destination
with it is INTERNATIONAL; without it, NATIONAL. This is deterministic,
needs no country table, and matches how a compromised phone is abused:
premium international numbers are dialled in international form. A
future home-country field can refine what "national" means without
changing the stored classes.

## D3. Default NATIONAL

`CreateExtension` supplies NATIONAL when the caller omits the field, and
the database defaults existing rows to NATIONAL. A new or upgraded
extension cannot immediately dial international numbers.

## D4. Refused before the engine

The check runs after the dial plan resolves a trunk destination and
before any leg is reserved or originated. A refusal therefore creates
no call, takes no channel, and never reaches the carrier. Emergency is
exempt because the platform already established emergency provenance
(AC-08.3); internal destinations never need the check.

## D5. The engine is not the enforcement point

The class is platform configuration. Projecting it into Asterisk would
add an engine vocabulary the platform must keep in sync, and a second
place to get the rule wrong. Placement is the only path to a trunk, so
it is the one enforcement point.

## Risks

- A tenant that dials international numbers in national form would be
  classified NATIONAL. The class is still correct; only the operator's
  expectation of what "national" means differs. Documented in the
  console hint.
- Anomaly detection (AC-08.2) is not satisfied by this change; spend
  caps and fraud detection remain a `compliance` change.
