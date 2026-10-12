import { useState } from "react";
import { Alert } from "../../components/Alert";
import { PageHeader } from "../../components/PageHeader";
import { useSession, useSessionController } from "./adapter";

// AccountPage: signing out today; changing the password arrives with
// console-foundation task 2.2.
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
  );
}
