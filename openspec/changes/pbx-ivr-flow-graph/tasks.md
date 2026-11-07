# Tasks — IVR flow graph

Shift-left cadence (D-93, `docs/TESTING.md` §8). Each task is one
commit, pushed after its gates pass:
1. `qa-engineer` writes the task's tests and shows them failing;
2. `implementer` makes them pass and runs the fast tiers;
3. `code-reviewer` approves;
4. the main session ticks, commits and pushes.

The **slice gate** after each section runs full integration on
`atsapbx_test`, that slice's e2e and its browser specs. The test plan is
`coverage.md`.

## 0. Test plan

- [ ] 0.1 `coverage.md`: every delta-spec scenario mapped to tiers (U/I/W/E/C/B/A) and named tests; gaps with reasons (qa-engineer)

## 1. Slice: the graph runs today's flows unchanged (no behaviour change)

- [ ] 1.1 Domain: `Destination` value type and kind registry; revision graph types; graph validation of structure (D1, D2, D6 structural rules) — U
- [ ] 1.2 Domain: pure interpreter `Step` for schedule/menu/play/goto/hangup with today's semantics (D3) — U
- [ ] 1.3 Domain: v1 → v2 conversion on read, a test per v1 shape proving identical routing (D5) — U
- [ ] 1.4 Migration 0026 (`schema_version`, `graph`) and store read/write of v2; v1 rows load as v2 — I
- [ ] 1.5 Telephony IVR session steps the interpreter over ARI, replacing the D-90 path; existing IVR e2e stays green — U (media fake), I, **slice gate: E existing IVR tests, full I**

## 2. Slice: schedules in a time zone (weekends closed)

- [ ] 2.1 Domain: `Schedule` with IANA zone, weekday ranges, cross-midnight, local-date holidays, override states — U including DST cases
- [ ] 2.2 Migration: `ivr_schedules` with RLS, `tenants.time_zone`; schedule CRUD and audited override in the application — I
- [ ] 2.3 API: schedule and override RPCs, tenant time zone; proto/OpenAPI/TS; `docs/API.md` — W
- [ ] 2.4 Console: tenant time zone and Schedules screen with override — C, B on the real API, **slice gate: E a weekend call takes the closed branch; full I**

## 3. Slice: typed destinations and multi-level menus

- [ ] 3.1 Application: publish/activate validation across aggregates (extensions, flows, outbound policy, schedules; depth ≤ 5; names the node) — U, I
- [ ] 3.2 Telephony routing to `flow`, `external` (through placement policy) and `hangup` destinations — U, I
- [ ] 3.3 API: graph/destination messages on publish/get; v1 fields deprecated and translated — W
- [ ] 3.4 Console: Call flows tree editor (menu → option → node, destination picker by kind, validation errors per node) — C, B, **slice gate: E carrier call through a two-level flow; full I**

## 4. Slice: menu behaviour

- [ ] 4.1 Re-prompt with retries, then fallback; `ivr_no_input`/`ivr_invalid_input` — U, I
- [ ] 4.2 Direct dial and `*`/`#` keys (D8), console fields — U, C, **slice gate: E where DTMF can be driven, else recorded gap; full I**

## 5. Slice: integrity

- [ ] 5.1 `DeleteExtension` refuses while an active revision targets it, naming flows; console shows the refusal — I, W, C, **slice gate: full I**

## 6. Change end

- [ ] 6.1 Full Go e2e, `make console-check`, phone UATs (owner consent), `docs/uat/ivr-menu.md` extended and run with a real handset, `make quality`, `make coverage-ratchet` (qa-engineer)
- [ ] 6.2 Docs: LLD-03 §3.2.1 marked implemented, PROGRESS, HANDOFF, `make docs`, `openspec validate --specs --strict` (docs-steward); then `/opsx:archive`
