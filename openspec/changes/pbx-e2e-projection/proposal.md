## Why

The archived change `pbx-extensions-projection` closed with gap **G1**:
three scenarios of `pbx-core/endpoint-projection` were proven live
against the real engine and never automated. They are that change's
headline claims — an extension is registrable the moment it is created, a
wrong secret is refused, and deleting it takes effect immediately — and
they rested on a human having done it once. A regression would not have
been caught by `mise run test`.

That gap had no dependency. It was deferred for batching, and batching is
a weaker reason than "the claims the change is built on should be
regression-proof".

## What Changes

- New e2e suite `api/internal/pbx/e2e/` (`//go:build e2e`) automating the
  three G1 scenarios against the live dev stack, driving mutations over
  the public HTTP API and asserting engine state directly in Postgres.
- `docs/TESTING.md` gains the pbx e2e row, including the rebuild
  prerequisite that otherwise makes every call 404.
- **No product code.** Any defect found is reported, not patched here.

## Capabilities

### New Capabilities
None — this adds coverage, not behaviour.

### Modified Capabilities
None. The scenarios already exist in
`openspec/specs/pbx-core/endpoint-projection/spec.md`; automating them
changes what is proven, not what is required. `skip_specs: true`.

## Impact

- **New**: `api/internal/pbx/e2e/projection_test.go`.
- **Changed**: `docs/TESTING.md` only.
- **Dependencies**: none added — `sipgo` and `icholy/digest` are already
  in `go.mod` and already used by `cmd/sip-ua`.
- **Rig**: the app container must carry `PbxService`, so it needs
  rebuilding before this suite can run.
