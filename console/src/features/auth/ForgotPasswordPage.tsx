import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { TextField } from "../../components/TextField";
import { useSessionController } from "./adapter";
import { AuthLayout } from "./AuthLayout";
import { throttleMessage } from "./copy";
import { useThrottle } from "./useThrottle";

// ForgotPasswordPage requests a reset link. The answer is the same for
// every address, so it cannot be used to find out who has an account.
export function ForgotPasswordPage() {
  const controller = useSessionController();
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const throttle = useThrottle();

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    if (!/^[^\s@]+@[^\s@]+$/.test(email.trim())) {
      setEmailError("Enter an email address, like name@example.com.");
      return;
    }
    setEmailError(null);
    setBusy(true);
    try {
      await controller.requestPasswordReset(email.trim());
      setSent(true);
    } catch (err) {
      const e = toApiError(err);
      throttle.hold(e);
      if (e.kind === "invalid") setEmailError("Enter an email address, like name@example.com.");
      else setFailure(e.kind === "throttled" ? throttleMessage(e) : e.message);
    } finally {
      setBusy(false);
    }
  }

  if (sent) {
    return (
      <AuthLayout title="Check your email" stepKey="sent">
        <p>If that address can sign in with a password, a reset link is on its way. The link works once, for 30 minutes.</p>
        <p className="auth-footer">
          <Link to="/sign-in">Back to sign in</Link>
        </p>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Reset your password">
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      <p>Enter your email and we will send you a link to choose a new password.</p>
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <TextField label="Email" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} error={emailError} required />
        <div className="form-actions">
          <button type="submit" className="button button-primary" disabled={busy || throttle.blocked}>
            Send reset link
          </button>
        </div>
      </form>
      <p className="auth-footer">
        <Link to="/sign-in">Back to sign in</Link>
      </p>
    </AuthLayout>
  );
}
