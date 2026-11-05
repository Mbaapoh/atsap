# Coverage — call detail reporting

- Domain/application: agent scope cannot widen; supervisor sees tenant;
  CSV includes attribution; missing context is refused; no-participant
  rows export.
- Postgres integration (migration 0023): terminated-only, participant
  extension/principal attribution, usage seconds, person/extension
  filters, export ordering, cross-tenant RLS.
- Telephony/PBX: placement and inbound commands carry the extension and
  owner; the caller/callee participants persist them.
- RPC: list/export wire mapping, filters, invalid IDs, durations and
  participant fields; public procedure allowlist includes ReportingService.
- Console: Reports page records, filters, paging, names, empty/error
  states and CSV export; navigation/keyboard order updated.
- Full integration, full e2e (including a reporting read after a real
  call), race, console-check and phone UATs; lint, SCA, image scan/SBOM,
  docs. Unit 79.7% >= 79.5%.
