import { useState } from "react";
import { useSearchParams } from "react-router";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { PageHeader } from "../../components/PageHeader";
import { TextField } from "../../components/TextField";
import { useSession, useSessionController } from "./adapter";
import { ChangePasswordForm } from "./ChangePasswordForm";

// AccountPage: change password and sign out (CONSOLE-DESIGN "Routes").
// With a must-change password it is the only page the console shows.
export function AccountPage() {
  const session = useSession();
  const controller = useSessionController();
  const [busy, setBusy] = useState(false);
  const [params] = useSearchParams();
  const [linkPassword, setLinkPassword] = useState("");
  const [linkFailure, setLinkFailure] = useState<string | null>(null);
  const [linking, setLinking] = useState(false);

  async function onLinkGoogle() {
    setLinkFailure(null);
    setLinking(true);
    try {
      window.location.assign(await controller.googleLinkURL(linkPassword));
    } catch (err) {
      setLinkFailure(toApiError(err).kind === "unauthenticated" ? "That password is not correct." : toApiError(err).message);
      setLinking(false);
    }
  }

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Account" />
      {session.passwordChangeRequired ? (
        <Alert tone="warning" title="Change your password to continue">
          <p>Your password was set by an administrator. Choose your own before using the console.</p>
        </Alert>
      ) : null}
      <ChangePasswordForm />
      <section aria-labelledby="google-title" className="account-section">
        <h2 id="google-title">Google sign-in</h2>
        {params.get("google") === "linked" ? (
          <Alert tone="success" title="Google account linked">
            <p>You can now sign in with that Google account.</p>
          </Alert>
        ) : null}
        {linkFailure ? <Alert tone="danger" title={linkFailure} /> : null}
        <p>Link a Google account to sign in with it later. Re-enter your password to confirm it is you.</p>
        <div className="field">
          <TextField label="Your password" type="password" autoComplete="current-password" value={linkPassword} onChange={(e) => setLinkPassword(e.target.value)} />
        </div>
        <div className="form-actions">
          <button type="button" className="button" disabled={linking || linkPassword === ""} onClick={() => void onLinkGoogle()}>
            Link Google account
          </button>
        </div>
      </section>
      <section aria-labelledby="sign-out-title" className="account-section">
        <h2 id="sign-out-title">Sign out</h2>
        <p>Ends this session. Sessions in other browsers stay signed in.</p>
        <div className="form-actions">
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={() => {
              setBusy(true);
              void controller.signOut().catch(() => undefined);
            }}
          >
            Sign out
          </button>
        </div>
      </section>
    </section>
  );
}
