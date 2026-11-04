# Coverage — extension ownership

- Domain: assignment validation.
- Integration (real Postgres, migration 0022 up/down): assign, list
  shows the owner's name/email, unassign, reassign, disabled membership
  refused, deleting the extension removes the assignment, and no
  cross-tenant visibility.
- RPC: assign/unassign round-trip and invalid principal id.
- Console: "Assigned to" and "Unassigned", Assign/Change owner dialog,
  active-member filtering, save, remove, invalid and generic failures,
  and modal close while idle/busy; full console-check including UATs.
- Full Go integration, full e2e on the hardened Asterisk image, unit
  gate 79.5%, container race, lint, SCA, image scan and SBOM.
