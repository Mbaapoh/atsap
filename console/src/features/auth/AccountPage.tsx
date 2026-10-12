import { useState } from "react";
import { Alert } from "../../components/Alert";
import { PageHeader } from "../../components/PageHeader";
import { useSession, useSessionController } from "./adapter";
import { ChangePasswordForm } from "./ChangePasswordForm";

// AccountPage: change password and sign out (CONSOLE-DESIGN "Routes").
// With a must-change password it is the only page the console shows.
export function AccountPage() {
  const session = useSession();
  const controller = useSessionController();
  const [busy, setBusy] = useState(false);
  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Account" />
      {session.passwordChangeRequired ? (
        <Alert tone="warning" title="Change your password to continue">
          <p>Your password was set by an administrator. Choose your own before using the console.</p>
        </Alert>
      ) : null}
      <ChangePasswordForm />
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
