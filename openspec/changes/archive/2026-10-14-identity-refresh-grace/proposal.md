## Why

When a refresh does not reach the browser, the session ends. The server rotates the refresh secret, but the browser keeps the previous one if the response is lost: a reload, a closed tab or a dropped connection while the call is in flight. Its next refresh presents the previous secret. D-62 reads that as a replay: the session is revoked and a security warning is logged, so a person who presses F5 at the wrong moment is signed out. The console's rig e2e found this and reproduces it on every run (`console/e2e/auth.rig.spec.ts`, marked expected-fail). PROGRESS.md records it as a High open gap that must be closed before the pilot.

## What Changes

- **Grace window.** A refresh that presents the *immediately previous* secret within a short window after it was rotated away is treated as a retry, not a replay. The server rotates again and issues a fresh secret and access token. The secret whose response was lost becomes unusable, and the session continues.
  - The window is 30 seconds, measured from the original rotation. A retry does not extend it.
  - The value is an assumption recorded for owner review. It is in line with the industry: Okta's default grace period is 30 seconds, and Auth0 offers a configurable reuse interval.
- **Replay detection is unchanged outside the window.** Presenting the previous secret after the window has closed still revokes the whole session. A secret older than the previous one, or one never issued, is still refused without revoking.
- **Retry is recorded.** A retry within the window is written as an auth event (`session.refresh_retried`) and logged at INFO, not WARN, so a burst of them stays visible to an operator without raising false theft alarms.
- **Session record.** Sessions record when their previous secret was rotated away (`previous_rotated_at`), in a new migration. A retry leaves it unchanged, which is what keeps the window from sliding.
- **Decision.** D-62 is amended to state the window and its rationale.
- **Acceptance test.** The console's known-defect test becomes the acceptance test: its expected-fail marker is removed and it must pass.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `identity/sessions`: the requirement "Refreshing rotates the secret and detects reuse" gains the grace window. Presenting the immediately previous secret within 30 seconds of its rotation re-rotates the session instead of revoking it; outside the window, reuse still revokes.

## Impact

- **Domain:** `api/internal/identity/domain/account.go` (`Session` gains `PreviousRotatedAt`, `Refresh` gains the retry outcome, a new `RefreshGraceWindow` constant, a new auth event kind).
- **Storage:**
  - `api/internal/identity/postgres/accounts.go` (scan and persist `previous_rotated_at`);
  - `api/internal/identity/memstore` (same, for unit tests);
  - migration `0011_session_refresh_grace` (nullable column; existing sessions start with none, so their first reuse still revokes as today).
- **Application:** `api/internal/identity/application/accounts.go` `RefreshSession` (records the retry event and logs at INFO).
- **API:** no proto or wire change. RefreshSession answers a retry exactly as it answers a normal refresh.
- **Console:** no code change. `console/e2e/auth.rig.spec.ts` drops `test.fail`.
- **Docs:** D-62 amendment, LLD-02 §6 (sessions), PROGRESS.md (the open gap closes).
- **Security:** someone who has copied the previous secret can use it within 30 seconds of its rotation, whereas today that attempt revokes the session. If they do:
  - they take over the session, and the person's browser is signed out (its secret is no longer current);
  - the takeover is not detected as a replay.

  The exposure is bounded:
  - the window does not slide;
  - the secret must already have been stolen (it is HttpOnly, SameSite=Strict and scoped to AuthService);
  - every use is recorded as an auth event;
  - the person can end every session by changing their password.

  Outside the window, D-62's protection is unchanged.
