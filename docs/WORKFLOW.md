# Development Workflow

How work actually moves through this repository, end to end, and — more
importantly — **how to pick it back up after a break** without
re-deriving where things stood.

This is the process document. What the system does lives in `PRD.md` and
the specs; why it is shaped that way lives in `DECISIONS.md`; this file
is only about the order of operations.

## 1. The document chain

Each layer answers a different question, has a different audience, and is
never converted into the layer below it (D-29):

| Layer | Answers | Lives in |
|---|---|---|
| BRD | What business are we in | `docs/BRD.md` |
| PRD | What must the product do | `docs/PRD.md` |
| DECISIONS | Why did we choose this, and what did we reject | `docs/DECISIONS.md` |
| TRD / HLD | What shape does the system have | `docs/TRD.md`, `docs/hld/` |
| LLD | Exactly how is one bounded context built | `docs/lld/LLD-NN-*.md` |
| OpenSpec change | One feature-sized slice, proposed and applied | `openspec/changes/<name>/` |
| Living spec | What the system currently does | `openspec/specs/<context>/<capability>/` |

**Feedback flows up, not just down.** If implementation shows a document
is wrong, the document is corrected in the same change — `/opsx:update`
for a local mismatch, a new `DECISIONS.md` entry for a real reversal.
Engineering never silently drifts from a document it disagrees with.

## 2. The loop, once per feature slice

```
LLD exists for the context
        │
        ├─ /opsx:propose  → proposal.md, specs/, design.md, tasks.md
        │                   (planning only — no code)
        │      ↓ review: does it match BRD/PRD? does it skip an invariant?
        │
        ├─ /opsx:apply    → work the tasks, checking each one off
        │      ↓ pause on: ambiguity, a design issue, added scope
        │
        ├─ verify against the LLD's Definition of Done
        │      ↓ "tests pass" is not the bar; the DoD is
        │
        └─ /opsx:archive  → delta specs merge into openspec/specs/
                            which becomes the new source of truth
```

Build order is fixed by `docs/hld/04-bounded-contexts.md` §10 and
`docs/lld/README.md`. Do not start row *N+1* until row *N* passes.

## 3. Resuming an in-flight change

**The checkboxes in `tasks.md` are the resume state.** There is no second
progress record to keep in sync, deliberately — a status file that drifts
from the task list is worse than no status file.

To pick up where the last session stopped:

```bash
openspec list --json                       # which changes are active
openspec status --change "<name>"          # artifacts + task progress
openspec instructions apply --change "<name>" --json
```

Then read, in this order:

1. `openspec/changes/<name>/proposal.md` — what and why
2. `openspec/changes/<name>/design.md` — the decisions already taken, and
   **the "Corrected during apply" notes**, which are where a previous
   session hit something the plan got wrong
3. `openspec/changes/<name>/tasks.md` — the first unchecked box is the
   resume point
4. The owning LLD section named in the change's design

`git log --oneline` on the change's commits shows what has already
landed. A task is checked only when its stated verification actually
passed — never when it is partially done.

## 4. Gates

Run before any commit that touches what they cover:

| Command | Covers |
|---|---|
| `mise run docs` | HLD markers, D-number citations, internal links, every proto RPC documented in `API.md` |
| `mise run lint` | `golangci-lint` **with `--build-tags integration,e2e`**, including the `depguard` bounded-context import rules |
| `mise run proto` | `buf lint` and `buf breaking` |
| `mise run test` | unit, and with tags `integration` / `e2e` |
| `mise run diagrams` | every mermaid block in `docs/` renders (needs Chromium; not in `ci`) |
| `mise run ci` | docs, lint, vuln, test in pipeline order |

**The build tags on `lint` are load-bearing.** Without them
`golangci-lint` never compiles a file behind `//go:build integration` or
`//go:build e2e`, so the boundary rules skip every integration and e2e
test in the repo — silently, reporting `0 issues`, which reads like
coverage rather than absence. Adding the tags on 2026-09-08 surfaced nine
findings that had been invisible since the suites were written. A gate
believed to cover more than it does is worse than no gate (D-28): if a
new suffix or tag is introduced, add it here in the same change.

A tagged integration or e2e test **may** wire concrete adapters — it is a
composition root, doing on a small scale what `cmd/atsap-api` does, and
it never ships. Both gates carve that out by file suffix
(`_integration_test.go`, `_e2e_test.go`) and no wider: an ordinary unit
test reaching for an ACL adapter is still a failure, and is the signal
that a port is missing.

Integration tests need the dev stack up (`mise run dev`) and connect over
host ports, so override the compose hostnames:

```bash
DATABASE_URL="postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable" \
  go test -tags integration ./... -p 1
```

`-p 1` is required: the integration suite resets the schema, so packages
must not run in parallel.

**Restore the rig between tiers with `mise run rig:restore`.** Two things
rot independently, and neither symptom names its cause — which is why
this is a command rather than a paragraph.

**Asterisk's ARI WebSocket stops accepting event streams** once a
long-running container has served enough of them. Observed 2026-09-09
after roughly twenty hours and a day of e2e runs. The HTTP API still
answers `200`, so Asterisk looks healthy and its healthcheck passes, but
every event-stream dial times out and the app logs
`ari: event stream disconnected, retrying` on a growing backoff. The
test-side symptom is:

```
timed out waiting for Stasis app voip-app-e2e to register
```

which names neither Asterisk nor WebSockets, and sends you debugging your
own test. **The discriminator is to run an e2e test you did not touch**
— `TestWalkingSkeleton` — before suspecting new code. If that fails too,
the rig is broken, not the change. Restarting Asterisk clears it.

**Run the e2e tier *after* re-registering the SIP fixtures.** Since
`ps_contacts` became realtime-backed (D-47, `pbx-extensions-projection`
design D8), device registrations live in the database, so the integration
suite's schema reset deregisters `cmd/sip-ua`. The e2e tier then fails
with ARI `500 "Allocation failed"` on `PJSIP/1000` — an error that names
neither registration nor the database. Between the tiers:

```bash
docker restart deploy-sipua-1 && sleep 12
```

**A gate that cannot check something must not claim to.** Where a rule is
enforced by review rather than by a machine, the change says so plainly
(D-28).

## 5. Who does which work

Two agents, deliberately different roles — full guidance in the
`agent-delegation` skill:

- **Claude Code** holds judgement: decisions, HLD/LLD, proposals, delta
  specs, design, security, and reviewing everything that comes back.
- **OpenCode with a cheaper model** holds volume: Docker rebuilds, test
  runs, repetitive edits with a fixed shape, code generation, long UAT
  rigs.

**Delegation never transfers responsibility.** Work returned by OpenCode
is unreviewed until Claude re-runs the gates and reads the diff. That
check has caught real defects on this project more than once, each time
after a "success" report.

Never delegate: an ambiguous spec, the first instance of a new pattern,
or anything touching credentials, grants, or bypass paths.

## 6. Git discipline

- Small, scoped commits. One concern per commit; a docs edit and a code
  edit do not share one unless the change says they are one unit.
- House style: `docs: ...` for documentation, `Add ...` for new
  artefacts, imperative subject, bullet body for multi-part commits,
  requirements traceability where the convention applies.
- **No AI attribution trailers.** The author is the repository owner's
  own git identity.
- **Commit and push are separate authorizations.** A request to commit is
  not permission to push.
- Never `--force`, never `--no-verify`, never amend someone else's commit.
- `.env` is never staged; scan the staged diff for credentials before
  committing.

## 7. Empirical verification over reasoning

The rule that has paid off most on this project: **when a claim about the
engine or the database can be tested in minutes, test it.**

- D-47 exists because ARI was *tried* and returned
  `403 "Cannot create sorcery objects of type 'endpoint'"` — the
  documentation alone would have led to the wrong decision.
- The credential design exists because `md5_cred` was *tried* against a
  row with no plaintext password and accepted a real `REGISTER`.
- Three mermaid diagrams had never rendered until a gate was added that
  actually rendered them.
- A test asserting "the engine's read failed" was strengthened to assert
  SQLSTATE `42501`, because a missing table would have made it pass
  while proving nothing.

Spikes are torn down afterwards and the dev stack restored; the result
goes into a decision or a design note, never into a memory.
