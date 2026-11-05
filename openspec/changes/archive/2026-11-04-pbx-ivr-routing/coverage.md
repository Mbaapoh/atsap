# Coverage — IVR routing

- Domain: flow validation, open/closed window including overnight and
  all-day, holiday branch, no-closed-destination refusal, DID normalisation.
- Store integration (migration 0024): create/find, immutable publish,
  rollback pointer, holidays, active version.
- Application/inbound integration: open hours ring the open extension,
  a holiday and outside hours ring the closed one; the source trunk is
  attributed correctly.
- RPC: create/get/list/publish/rollback/delete/versions/holidays and
  invalid flow/config IDs.
- Console: Call flows create/publish/versions/rollback/delete, holiday
  add/remove, read-only agent, defaults and failure states.
- Full integration, full e2e, race, console-check and phone UATs; lint,
  SCA, image scan/SBOM and docs. Unit 79.8% >= 79.7%.
