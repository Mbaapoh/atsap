# Coverage — outbound destination classes

## Automated

- Domain: ranking, default, dialled-form classification, validation.
- RPC: wire enum round-trip; the placement refusal maps to PERMISSION_DENIED.
- Integration (real Postgres, migration 0019 up/down): a new extension is
  NATIONAL; a national extension is refused `+` before any leg or
  channel and succeeds in national form; widening to INTERNATIONAL then
  succeeds; emergency is exempt.
- Full Go e2e: the new refusal through the real API and engine; every
  existing outbound test creates its extension INTERNATIONAL as setup.
- Console: the list shows the class; Add sends the chosen class and
  defaults NATIONAL; Edit changes it; the UAT widens a real phone to
  INTERNATIONAL before its real carrier call.
- `make console-check`, `make docs`, `make sca`, the unit gate (79.4%)
  and the container race tier pass.

## Not covered

- AC-08.2 anomaly detection (high volume to a high-cost destination
  outside business hours) is **not** part of this change and remains a
  `compliance` gap.
