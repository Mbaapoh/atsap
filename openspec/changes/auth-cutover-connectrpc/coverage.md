# Scenario coverage — `auth-cutover-connectrpc`

The delta modifies one requirement in `identity/public-api`. Each of its
scenarios, and what verifies it.

| Scenario | Verified by | Kind |
|---|---|---|
| Mutating operations refuse an unauthenticated caller | `TestEveryMethod_RequiresAuthentication` (pbx), identity rpc handler tests | unit |
| **Reading operations refuse an unauthenticated caller** | `TestGetCall_RefusesUnauthenticated` | **e2e, fault-injected** |
| **A caller cannot name a tenant that is not their own** | `TestGetCall_RefusesAnotherTenant` | **e2e, fault-injected** |
| Obtaining a token needs no token | `TestExemptProcedure_IsOnlyAuthenticateUser`; the walking-skeleton e2e obtains a real token | unit, e2e |

Both new e2e guards were proven to fail before being trusted: with the
interceptor unmounted and the app rebuilt, `TestGetCall_RefusesUnauthenticated`
reported

```
unauthenticated GetCall returned 404, want 401 — the auth interceptor is
not mounted on TelephonyService
```

and `TestGetCall_RefusesAnotherTenant` failed alongside it. Remounting
returned both to green.

## Why the guards exist at all

The archived `identity-api` change recorded task 3.2 as *"Verify
`TelephonyService` remains unmounted from the interceptor … asserted by
test, so an accidental cutover fails loudly here."*

**No such test existed.** Every unit test still passed after the
interceptor was mounted, which could not be true if anything asserted the
old behaviour. The guard was believed to exist and did not, which is how
a documented "temporary" concession survived two changes unchallenged.

That is the finding worth carrying forward: a task ticked with "asserted
by test" is worth exactly as much as the test, and this change verified
its own guards by breaking them rather than repeating the claim.

## No open gaps

Every scenario in the modified requirement has a passing test. The
tenant-mismatch guard deliberately asserts `400` rather than any
non-`200`: a `404` would mean the request reached the store and merely
found nothing, which is a weaker property than being refused before it
got there.

## Scope added during apply

Task 3.4 was not in the proposed plan. Task 1.2's survey found that no
*script* calls `GetCall`, but `docs/uat/walking-skeleton.md` documents a
**manual** UC-03 step that does. Shipping the cutover without updating it
would have sent the next operator debugging the platform instead of the
document. Added as a task with its origin recorded rather than absorbed
silently.
