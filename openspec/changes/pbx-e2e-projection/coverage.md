# G1 closure and findings — `pbx-e2e-projection`

Test-only change. No product code was modified; no delta specs, because
the scenarios already exist in
`openspec/specs/pbx-core/endpoint-projection/spec.md`.

## G1 — closed

The three scenarios the archived `pbx-extensions-projection` recorded as
proven live but never automated now run in
`api/internal/pbx/e2e/projection_test.go` (`//go:build e2e`), driven over
the public HTTP API against the live dev stack.

| Scenario (living spec) | Automated as | Assertion that matters |
|---|---|---|
| A device registers immediately after creation | `registers immediately after creation` | REGISTER succeeds on the **first attempt**, no sleep and no retry — immediacy is the property, so tolerating a retry would test something weaker. Then the contact is confirmed bound in `ps_contacts` and the API reports `REGISTERED`, because a `200 OK` with no bound contact is the silent failure this design exists to prevent |
| A wrong secret is refused | `wrong secret is refused` | `401` for a wrong secret, then `200` again for the correct one — a refusal must not lock the endpoint out |
| Deletion takes effect immediately | `deletion takes effect immediately` | the correct secret stops working with no reload, and the projected endpoint row is gone |

Supporting assertions in the same suite: `ps_auths.password` is `NULL`
(no recoverable secret, including in engine-facing state), and an
unauthenticated `CreateExtension` returns `401` — a guard against a
regression that mounted `PbxService` without the auth interceptor, which
would leave every scenario above passing while the API was open.

Stability: three consecutive runs, zero retries, zero flakes.

## G2 — still open, unchanged

Cross-tenant separation (*identical numbers in two tenants remain
separate*) remains covered at the database and in the derivation, not
staged live. This change did not attempt it: the fixture path now exists
(a tenant can be seeded directly, as `newRig` does), so the earlier
"cannot bootstrap a second tenant" reasoning no longer holds and G2 is
now a scope decision rather than a blocked one.

## G3 — new: deleting an extension leaves an orphaned contact row

**Found by this work, product behaviour, not a defect in it.**

`projector.RemoveExtension` deliberately does not delete `ps_contacts`
rows: contacts are the engine's own bookkeeping, written on registration
and pruned by it on expiry, and the ACL reaching into them would exceed
its remit. The first draft of the deletion test asserted the contact was
gone and failed — correctly. The test was changed to assert the
documented behaviour; the product was not.

What this leaves:

- After a delete, a contact row survives until its `expiration_time`
  passes. It is unreachable — the endpoint it names no longer exists — so
  this is housekeeping, not a correctness or security issue.
- **Nothing reports it.** `atsap-api pbx reconcile` inventories
  `ps_endpoints` only, so an orphaned contact is invisible to the one
  tool whose job is finding engine state that disagrees with the
  platform.
- Volume is bounded by registration expiry, so it self-heals; a tenant
  churning extensions faster than that accumulates rows in between.

Not urgent, and deliberately not fixed here — this change alters no
product code. Worth a decision in the next `pbx-core` change: either
extend `reconcile` to report orphaned contacts, or record in D-47 that
they are the engine's to prune and are intentionally unreported.

The deletion test asserts the contact **remains**, so if the ACL ever
starts deleting contacts the test fails and this finding is revisited
rather than silently invalidated.
