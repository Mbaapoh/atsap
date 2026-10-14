import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { TextField } from "../../components/TextField";
import { passwordProblem, PASSWORD_MAX, PASSWORD_MIN, useSessionController } from "./adapter";
import { AuthLayout } from "./AuthLayout";
import { throttleMessage } from "./copy";
import { useThrottle } from "./useThrottle";

// readFragmentSecret reads the link's secret from the URL fragment, which
// browsers never send to a server. Pure, so a repeated render reads the
// same value.
function readFragmentSecret(): string {
  return decodeURIComponent(window.location.hash.replace(/^#/, ""));
}

// ResetPasswordPage redeems an emailed reset link (/reset#secret) or an
// invitation (/invite#secret). The secret is taken out of the address bar
// as soon as the page has it, so it is not left in history, a bookmark or
// a screenshot (console brief §6 rule 3).
export function ResetPasswordPage({ purpose }: { purpose: "reset" | "invite" }) {
  const controller = useSessionController();
  const [secret] = useState(readFragmentSecret);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [failure, setFailure] = useState<{ title: string; expired: boolean } | null>(null);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const throttle = useThrottle();

  useEffect(() => {
    if (window.location.hash) {
      window.history.replaceState(window.history.state, "", window.location.pathname);
    }
  }, []);

  const title = purpose === "invite" ? "Set up your account" : "Choose a new password";

  if (!secret) {
    return (
      <AuthLayout title={title}>
        <Alert tone="warning" title="This link is incomplete">
          <p>Open the link from your email again, or request a new one.</p>
        </Alert>
        <p className="auth-footer">
          <Link to="/forgot-password">Request a new link</Link>
        </p>
      </AuthLayout>
    );
  }

  if (done) {
    return (
      <AuthLayout title="Password set" stepKey="done">
        <Alert tone="success" title="Your password is set">
          <p>Every earlier session has been signed out. Sign in with your new password.</p>
        </Alert>
        <p className="auth-footer">
          <Link to="/sign-in">Sign in</Link>
        </p>
      </AuthLayout>
    );
  }

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const problem = passwordProblem(password);
    const mismatch = password !== confirm ? "The passwords do not match." : null;
    setPasswordError(problem);
    setConfirmError(problem ? null : mismatch);
    if (problem || mismatch) return;
    setBusy(true);
    try {
      await controller.completePasswordReset(secret, password);
      setDone(true);
    } catch (err) {
      const e = toApiError(err);
      throttle.hold(e);
      // The server answers an unusable link with INVALID_ARGUMENT — the
      // code it also uses for a password out of bounds. The length was
      // checked above with the server's own rule (code points, 8 to 64),
      // so here it can only be the link.
      if (e.kind === "invalid" || e.kind === "unauthenticated") setFailure({ title: "This link has expired or was already used", expired: true });
      else setFailure({ title: e.kind === "throttled" ? throttleMessage(e) : e.message, expired: false });
    } finally {
      setPassword("");
      setConfirm("");
      setBusy(false);
    }
  }

  return (
    <AuthLayout title={title}>
      {failure ? (
        <Alert tone="danger" title={failure.title} focusOnMount>
          {failure.expired ? (
            <p>
              <Link to="/forgot-password">Request a new link</Link>
            </p>
          ) : null}
        </Alert>
      ) : null}
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <TextField
          label="New password"
          type="password"
          autoComplete="new-password"
          hint={`${PASSWORD_MIN} to ${PASSWORD_MAX} characters. A passphrase of a few words is easy to remember.`}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={passwordError}
          required
        />
        <TextField
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          error={confirmError}
          required
        />
        <div className="form-actions">
          <button type="submit" className="button button-primary" disabled={busy || throttle.blocked}>
            Set password
          </button>
        </div>
      </form>
    </AuthLayout>
  );
}
