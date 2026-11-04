# Coverage — outbound fraud guard

- Domain: policy validation, blocked prefixes, business hours (including
  overnight and all-day), spend caps and the anomaly rule.
- Integration (real Postgres, migration 0021): policy defaults, set/get,
  suspend/resume, spend add/read, attempt count under the tenant lock, and
  cross-tenant invisibility; placement refuses a blocked prefix, a spent
  cap and an anomaly suspension, alerts once, and leaves emergency alone.
- RPC: policy round-trip and resume; call errors map to fixed
  permission/failed-precondition messages.
- Full integration, full Go e2e, container race, lint, SCA and docs.
- Console: admin and read-only views, load/save/resume failures, empty
  field defaults, midnight hours, suspended state and resume; full
  console-check including the phone UAT.
