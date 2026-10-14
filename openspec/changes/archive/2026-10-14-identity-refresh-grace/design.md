## Context

The refresh logic lives in the identity domain's `Session.Refresh` (`api/internal/identity/domain/account.go`). It is pure: the store locks the session row (`SELECT … FOR UPDATE` on `refresh_digest = $1 OR previous_digest = $1`), applies the function, and persists the result even when it reports an error, so that a revocation is stored. A session keeps two generations of digest: current and previous. Presenting the previous one today always means `RefreshReused`, which revokes the session. Nothing records *when* the previous secret was retired, so there is nothing to measure a window against. See proposal.md for why this matters.

## Goals / Non-Goals

**Goals:**
- A refresh whose response was lost can be retried once the browser comes back, without ending the session or raising a security warning.
- Replay detection is unchanged outside a short, non-sliding window.
- The rule stays in the pure domain function, with no change to the API or the console.

**Non-Goals:**
- Making the window configurable at run time. It is a domain constant, as the idle and absolute lifetimes are.
- Detecting theft inside the window by other signals, such as source address or user agent. That would be a separate change if wanted.
- Changing how the console refreshes. It already serializes refreshes across tabs (HLD-19 §4); this fixes the case it cannot control: a response lost on the way back.

## Decisions

### 1. Grace on the immediately previous generation only, measured from its retirement

`Refresh` gains a case before the reuse case. If the presented digest equals `PreviousDigest` and `now < PreviousRotatedAt + RefreshGraceWindow`, the outcome is `RefreshRetried`:
- `RefreshDigest` becomes `newDigest`;
- `PreviousDigest` and `PreviousRotatedAt` are left as they are;
- idle expiry is extended as for a normal rotation.

A normal rotation sets `PreviousDigest` to the old current digest and `PreviousRotatedAt` to now.

- *Why the previous generation only:* a lost response loses exactly one rotation. Accepting older secrets would need a history of digests and would widen the exposure for no recovery benefit.
- *Why not slide the window on each retry:* a sliding window would let a copied secret be refreshed indefinitely by retrying every 29 seconds. Measuring from retirement caps the exposure at one window per rotation.
- *Why rotate again rather than return the lost secret:* the server stores only digests, so it cannot resend the lost secret. A fresh secret also makes the lost one, which might sit in an intercepted response, unusable.

**Alternative considered: no rotation for the first N seconds after issue (Auth0's "reuse interval" style).** Rejected because it changes every refresh, not only the failure case, and leaves two secrets fully valid at once.

**Alternative considered: fix only on the client** (persist the new secret before navigating away). Rejected because the cookie is HttpOnly and is set by the server's response; if the response is lost, nothing on the client can recover it.

### 2. The window is 30 seconds

This covers a reload, a reconnecting network or a browser restoring a tab, all of which come back within seconds. It matches Okta's default refresh grace period. It is short against the 15-minute access token and 30-minute idle lifetime. It is an assumption for owner review; changing it changes one constant and the spec's number.

### 3. Record the retry; log at INFO

- The application layer maps `RefreshRetried` to a successful refresh.
- It writes `EventSessionRefreshRetried` (`session.refresh_retried`) to `auth_events`, the same table as sign-ins and revocations.
- It logs at INFO with the session and user ids.

The existing WARN stays reserved for real reuse. `auth_events.kind` is free text (`VARCHAR(50)`), so no constraint changes.

### 4. Migration

`0011_session_refresh_grace` adds `previous_rotated_at TIMESTAMPTZ NULL` to `sessions`. It is NULL for existing rows, and a NULL means "no window": the first reuse of an existing session revokes, exactly as today. The down migration drops the column. The store's session columns, scan and update include it, and the in-memory store mirrors it for unit tests.

## Risks / Trade-offs

- **[Risk] A thief holding the previous secret within the window takes over the session.** The person's browser is then refused on its next refresh, and nothing is flagged as a replay.
  → **Mitigation:**
  - the window is short and does not slide;
  - the secret is HttpOnly, SameSite=Strict and path-scoped, so stealing it already implies a compromised browser or TLS;
  - every retry is an auth event an operator can query;
  - the person is returned to sign-in and can end every session by changing their password.
  The proposal records this trade-off for the owner.
- **[Risk] A clock jump on the server lengthens or shortens the window.** → Times come from the same clock that rotates, and the comparison is against a stored timestamp; a jump affects only the one window in flight.
- **[Risk] Two retries race within the window.** → The row lock serializes them. The second presents the same previous digest, is also within the window, and rotates again; the last response wins the cookie, and the session stays valid.

## Migration Plan

1. Apply migration 0011. The column is additive and nullable, so the running app is unaffected.
2. Deploy the app. Existing sessions get a window from their next normal rotation onward.
3. Rollback: deploy the previous app; the column is ignored. Run the down migration if wanted. Behaviour returns to revoke-on-any-reuse.

## Architecture compliance

**PASS.** The change stays inside the identity bounded context:
- the domain function, its two stores, the application service, and one migration;
- no new port, no cross-context import, no proto change, no new dependency;
- the console consumes the unchanged public API (D-43).
