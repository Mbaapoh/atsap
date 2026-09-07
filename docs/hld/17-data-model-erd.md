<!-- OpenSpec: TRD-HLD-18 -->
# Data Model — Entity Relationships

Visual companion to the authoritative DDL in
[03-domain-model.md §5](03-domain-model.md#5-comprehensive-relational-schema-postgresql-16).
This document draws those tables — it does not define them. Where this
file and [03](03-domain-model.md) disagree, [03](03-domain-model.md)
wins, and this file is the one that gets fixed.

Attributes shown are the keys and the columns that carry meaning for a
reader. Every table's full column list, defaults, and RLS policies live
in [03](03-domain-model.md) §5.

## 1. Reading rules

- **`tenant_id` is on every table but two.** `licensing_state` is
  installation-scoped, not tenant-scoped (T-1, D-24) — it has no
  `tenant_id` by construction, and RLS must never be applied to it. The
  ACL projection tables (§6) are the other exception, for a different
  reason given there. Every other table carries `tenant_id NOT NULL
  REFERENCES tenants(id)` with `ENABLE` **and** `FORCE ROW LEVEL
  SECURITY`.
- **`tenant_id` edges are omitted from the detail diagrams in §3–§5.**
  Drawing seventeen lines into `tenants` makes the diagram unreadable and
  says nothing the rule above does not. §2 shows them once; after that,
  assume them.
- **A dotted relationship (`..`) is not a foreign key.** It marks a
  correspondence something in code maintains — used only in §6, where
  the ACL projects domain rows into tables Asterisk owns.
- Ubiquitous language (D-17, D-18, D-32): the participant table is
  `call_participants`, never "legs". Asterisk channel identifiers appear
  only in `channel_history`, inside the ACL.

## 2. Overview — every table

```mermaid
erDiagram
    tenants ||--o{ principals : scopes
    tenants ||--o{ role_bindings : scopes
    tenants ||--o{ api_keys : scopes
    tenants ||--o{ extensions : scopes
    tenants ||--o{ carrier_trunks : scopes
    tenants ||--o{ carrier_routes : scopes
    tenants ||--o{ ivr_flows : scopes
    tenants ||--o{ flow_versions : scopes
    tenants ||--o{ calls : scopes
    tenants ||--o{ call_participants : scopes
    tenants ||--o{ channel_history : scopes
    tenants ||--o{ usage_seconds : scopes
    tenants ||--o{ recordings : scopes
    tenants ||--o{ audit_logs : scopes
    tenants ||--o{ outbox : scopes

    principals ||--o{ role_bindings : "granted"
    principals ||--o{ api_keys : "authenticates"

    carrier_trunks ||--o{ carrier_routes : "terminates"
    ivr_flows ||--o{ flow_versions : "versioned as"

    calls ||--o{ call_participants : "joined by"
    calls ||--o{ usage_seconds : "metered as"
    calls ||--o{ recordings : "recorded as"
    call_participants ||--o{ channel_history : "observed as"
    call_participants ||--o{ usage_seconds : "metered as"
    call_participants |o--o{ recordings : "recorded as"

    licensing_state {
        uuid instance_id PK "installation-scoped, NO tenant_id (T-1)"
    }
```

`licensing_state` is drawn unattached on purpose. It has no relationship
to any other table, and adding one would be a design error: a licence
belongs to the installed instance, not to a tenant inside it.

## 3. Identity and tenancy

Implemented in `0003_identity.up.sql` (LLD-02).

```mermaid
erDiagram
    tenants ||--o{ principals : scopes
    principals ||--o{ role_bindings : "granted"
    principals ||--o{ api_keys : "authenticates"

    tenants {
        uuid id PK
        varchar name
        varchar status "ACTIVE / SUSPENDED"
        varchar residency_zone "EU by default"
        timestamptz created_at
    }
    principals {
        uuid id PK
        uuid tenant_id FK
        varchar username "UNIQUE with tenant_id"
        varchar email
        varchar password_hash "Argon2id PHC string, never plaintext (INV-11)"
        varchar role "default role at provisioning only"
        varchar status
    }
    role_bindings {
        uuid principal_id PK "also FK to principals"
        varchar role PK
        varchar scope PK "tenant or department scope"
        uuid tenant_id FK
    }
    api_keys {
        uuid id PK
        uuid tenant_id FK
        uuid principal_id FK
        varchar key_hash "SHA-256 hex; raw key returned once"
        timestamptz expires_at
        timestamptz revoked_at
    }
```

Two things this diagram is meant to make hard to get wrong:

- **`role_bindings` is what authorizes, not `principals.role`.** The
  column on `principals` records the role a principal was provisioned
  with; the binding rows are what `AuthorizeAction` reads. A principal
  with no binding has no permission, whatever its `role` column says.
  This is enforced in `identity/application` and covered by test.
- **`api_keys` stores a SHA-256 digest, not the key.** The raw key is
  returned exactly once, at issue. A key is tenant-embedded
  (`atsa_<tenant-id>_<random>`) so the tenant context needed to satisfy
  RLS can be recovered before the lookup — without that, the lookup
  cannot find its own row.

## 4. Configuration — what the console writes

`extensions`, `carrier_trunks` and `carrier_routes` land in Phase A
(LLD-03). `ivr_flows` and `flow_versions` are Phase B, drawn here
because their shape constrains Phase A's routing columns.

```mermaid
erDiagram
    carrier_trunks ||--o{ carrier_routes : "terminates"
    ivr_flows ||--o{ flow_versions : "versioned as"

    extensions {
        uuid id PK
        uuid tenant_id FK
        varchar extension_number "UNIQUE with tenant_id; tenant-local"
        varchar display_name
        uuid department_id
        varchar auth_username
        varchar password_hash
        varchar device_type "WEBRTC / SIP"
    }
    carrier_trunks {
        uuid id PK
        uuid tenant_id FK
        varchar name
        varchar host
        int port "default 5060"
        varchar credentials_ref "secret reference, not the secret"
        int channel_limit
        int priority
        varchar health_status
    }
    carrier_routes {
        uuid id PK
        uuid tenant_id FK
        varchar prefix_pattern
        uuid trunk_id FK
        bigint cost_rate_per_min
        int priority "lower wins; LCR order"
    }
    ivr_flows {
        uuid id PK
        uuid tenant_id FK
        varchar name
        uuid published_version_id "points at flow_versions.id"
    }
    flow_versions {
        uuid id PK
        uuid tenant_id FK
        uuid flow_id FK
        int version_number "UNIQUE with flow_id"
        varchar status "DRAFT / PUBLISHED / ARCHIVED"
        jsonb graph_json "the flow-as-data node graph (D-22)"
    }
```

`extensions.extension_number` is **tenant-local** — extension 1000 may
exist in every tenant. That is a product requirement, and it is what
forces the projected identifier in §6 to be something else.

`ivr_flows.published_version_id` is deliberately not a declared foreign
key: `ivr_flows` and `flow_versions` reference each other, and one
direction has to give. Publishing sets it under the same transaction
that flips the version's status.

## 5. Calls, usage and recordings

Implemented in `0001_telephony_core.up.sql` (LLD-01), except
`recordings` (Phase B).

```mermaid
erDiagram
    calls ||--o{ call_participants : "joined by"
    calls ||--o{ usage_seconds : "metered as"
    calls ||--o{ recordings : "recorded as"
    call_participants ||--o{ channel_history : "observed as"
    call_participants ||--o{ usage_seconds : "metered as"
    call_participants |o--o{ recordings : "recorded as"

    calls {
        uuid id PK
        uuid tenant_id FK
        varchar direction "INBOUND / OUTBOUND / INTERNAL"
        varchar state "see 16-call-lifecycle-state-machine"
        varchar source_number
        varchar dest_number
        timestamptz started_at
        timestamptz answered_at
        timestamptz ended_at
        varchar termination_reason
    }
    call_participants {
        uuid id PK
        uuid tenant_id FK
        uuid call_id FK
        varchar role "CALLER / AGENT / IVR / QUEUE / SUPERVISOR / AI"
        varchar endpoint_uri
        varchar state "Invited / Ringing / Connected / OnHold / Disconnected"
        int billable_seconds
    }
    channel_history {
        bigserial id PK
        uuid tenant_id FK
        uuid participant_id FK
        bytea channel_ref "ENCRYPTED Asterisk channel id — ACL only"
        varchar bridge_id
        varchar node_id
        varchar event_type
    }
    usage_seconds {
        uuid participant_id PK "also FK to call_participants"
        timestamptz second_ts PK
        uuid tenant_id FK
        uuid call_id FK
        bigint cost_micros
        bigint revenue_micros
        numeric quality_mos
        bool ai_streamed
    }
    recordings {
        uuid id PK
        uuid tenant_id FK
        uuid call_id FK
        uuid participant_id FK "nullable — whole-call recordings"
        varchar storage_uri
        bytea wrapped_dek "envelope key, crypto-shreddable (T-9/T-10)"
        varchar consent_state
        varchar sha256_hash
        timestamptz deleted_at
    }
```

`channel_history` is the **only** table that holds an Asterisk
identifier, it is encrypted at rest, and nothing outside
`internal/telephony/acl` reads it (HLD 01 §1.2, D-41). `usage_seconds`
is per participant per second — the seam D-24 requires for the R3 margin
ledger, populated from LLD-01 onwards even though nothing bills from it
yet.

## 6. The ACL projection (D-47)

Asterisk does not read the tables above. The ACL projects the domain row
into tables Asterisk owns the shape of, and Asterisk reads those via
PJSIP Realtime — no file generation, no reload.

```mermaid
erDiagram
    extensions ||..|| ps_endpoints : "projected by the ACL"
    extensions ||..|| ps_auths : "projected by the ACL"
    extensions ||..|| ps_aors : "projected by the ACL"
    carrier_trunks ||..|| ps_endpoints : "projected by the ACL"

    ps_endpoints {
        varchar id PK "globally unique — NOT the extension number"
        varchar aors
        varchar auth
        varchar context "stasis-in"
        varchar transport
        varchar allow
    }
    ps_auths {
        varchar id PK
        varchar auth_type
        varchar username
        varchar password
    }
    ps_aors {
        varchar id PK
        int max_contacts
        varchar remove_existing
    }
```

Four properties of this boundary, each of which is a rule and not a
preference:

1. **The dotted lines are not foreign keys, and never become them.**
   `ps_*` has no reference to `extensions` and the domain has no
   reference to `ps_*`. The correspondence is maintained by the ACL,
   inside the transaction that writes the domain row.
2. **`ps_endpoints.id` is globally unique, derived from
   `extensions.id`.** `ps_*` is one flat namespace shared by every
   tenant, so two tenants' extension 1000 cannot both be `'1000'`. The
   number the user sees stays tenant-local; the projected identifier is
   never shown in any interface, API response, or error (PRD principle 4).
3. **`ps_*` is not under RLS.** Asterisk connects as its own database
   role and cannot set a tenant context, so a policy would simply hide
   every row from it. Isolation here is by construction — globally
   unique ids — and the control is the grant: that role gets `SELECT` on
   `ps_*` only, and nothing at all on the domain tables.
4. **`ps_contacts` is deliberately absent.** Registration contacts live
   in `astdb`, which is node-local and sufficient for a single node. The
   multi-node work (D-08) maps `ps_contacts` to realtime so any node
   knows where a phone is registered; Phase A does not need it and must
   not pretend to have solved it.

## 7. Cross-cutting tables

```mermaid
erDiagram
    tenants ||--o{ audit_logs : scopes
    tenants ||--o{ outbox : scopes

    audit_logs {
        bigserial id PK
        uuid tenant_id FK
        uuid actor_id "principal or api key"
        varchar actor_type
        varchar action
        varchar resource_type
        varchar resource_id
        jsonb before_state
        jsonb after_state
        inet ip_address
    }
    outbox {
        uuid id PK
        uuid tenant_id FK
        varchar event_type "event.call.* / event.participant.*"
        uuid aggregate_id
        jsonb payload
        timestamptz published_at "NULL until the worker publishes"
    }
```

`audit_logs` is append-only: no `UPDATE`, no `DELETE`. `outbox` is the
durability boundary for domain events (INV-06) — a NATS restart delays
delivery, it never loses an event, because the row is committed with the
state change that produced it.

## 8. What exists at the end of each phase

| Table | Phase | Status today |
|---|---|---|
| `tenants`, `calls`, `call_participants`, `channel_history`, `usage_seconds`, `outbox` | pre-A | **Implemented** (`0001`, LLD-01) |
| `principals`, `role_bindings`, `api_keys`, `audit_logs` | pre-A | **Implemented** (`0003`, LLD-02) |
| `licensing_state` | **A** | Designed; LLD-02's licensing half |
| `extensions`, `carrier_trunks`, `carrier_routes` | **A** | Designed; LLD-03 |
| `ps_endpoints`, `ps_auths`, `ps_aors` | **A** | Decided (D-47); LLD-03 |
| `ivr_flows`, `flow_versions` | **B** | Designed |
| `recordings` | **B** | Designed |
| `ps_contacts` | multi-node | Deferred (D-08) |

Phase A therefore adds five tables to a schema that already has ten of
the seventeen in place.

## 9. Traceability and verification

- Authoritative DDL: [03-domain-model.md §5](03-domain-model.md#5-comprehensive-relational-schema-postgresql-16).
  Migrations: `api/migrations/0001_telephony_core.up.sql`,
  `api/migrations/0003_identity.up.sql`.
- Tenant isolation: [05-security.md](05-security.md); the RLS isolation
  test in LLD-01 is what proved `FORCE` is required, not just `ENABLE`.
- ACL projection: **D-47**; the boundary rule it rests on is HLD
  [01-architecture.md](01-architecture.md) §1.2 and D-41.
- Call and participant states shown as `state` columns here are defined
  in [16-call-lifecycle-state-machine.md](16-call-lifecycle-state-machine.md).
- Phase A journeys that write these tables:
  [18-phase-a-user-flows.md](18-phase-a-user-flows.md).
