# UAT — Sign in with Google (D-67, D-83)

Live Google cannot run in CI (the client secret and a real account are
not available), so this procedure is the release smoke test.

## Setup

1. Create a Google OAuth client (Web application) with the exact
   redirect URI `https://<console>/auth/google`.
2. Set `GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET` and
   `GOOGLE_OIDC_REDIRECT_URL` in the deployment secret store, then
   restart the app. The client secret must not be committed or logged.
3. Add a person by email in the console and keep the invitation mail.

## Cases

1. **Invitation with Google.** Open `/invite#…` from the mail, choose
   **Accept with Google instead**, and use the invited Google account.
   Expect the console to open signed in, with no password set; Accounts
   shows the Google link after the callback.
2. **Wrong Google account.** Repeat with a different Google account.
   Expect the neutral "email or password" refusal, and the invitation
   still works with the right account.
3. **Sign-in.** Sign out, choose **Continue with Google** (shown before
   an email is typed), and use the linked account. Expect the console.
4. **Unlinked account.** Use a Google account that was never invited or
   linked. Expect the same refusal as a wrong password; no person or
   membership is created.
5. **Link from Account.** Signed in with a password, open Account,
   enter the wrong current password (refused), then the right one, and
   link Google. Sign out and sign in with Google.
6. **Replay.** Re-open a used callback URL from browser history. Expect
   a refusal, not a second session.
7. **Disabled.** Remove the client configuration and restart. Expect no
   Google button and password sign-in unchanged.
