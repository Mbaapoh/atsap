import { useRef, useState, type FormEvent } from "react";
import { Link, Navigate, useLocation } from "react-router";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { TextField } from "../../components/TextField";
import { useSession, useSessionController, type SignInMethodView } from "./adapter";
import { AuthLayout } from "./AuthLayout";
import { throttleMessage, WRONG_CREDENTIALS } from "./copy";
import { NoAccess } from "./NoAccess";
import { useThrottle } from "./useThrottle";

type Step = { kind: "email" } | { kind: "password" } | { kind: "sso"; providerName: string };

// returnPath is where to go after signing in: the page the guard sent the
// person away from, kept in router memory only.
function returnPath(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from;
  return typeof from === "string" && from.startsWith("/") && !from.startsWith("//") ? from : "/";
}

// SignInPage asks for an email, discovers how it signs in, then asks for
// the password (CONSOLE-DESIGN "Sign in"). The password lives in this
// component's state only, and is cleared after every attempt.
export function SignInPage() {
  const session = useSession();
  const controller = useSessionController();
  const location = useLocation();
  const [step, setStep] = useState<Step>({ kind: "email" });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [emailError, setEmailError] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const throttle = useThrottle();
  const passwordRef = useRef<HTMLInputElement>(null);

  if (session.status === "restoring") {
    return (
      <AuthLayout title="Sign in">
        <p role="status">Checking for an existing session…</p>
      </AuthLayout>
    );
  }
  if (session.status === "signed-in") return <Navigate to={returnPath(location.state)} replace />;
  if (session.status === "choosing-tenant") return <Navigate to="/select-tenant" replace state={location.state} />;
  if (session.status === "no-access") return <NoAccess />;

  function fail(err: unknown, wrongCredentials = WRONG_CREDENTIALS) {
    const e = err instanceof ApiError ? err : new ApiError("unknown");
    throttle.hold(e);
    if (e.kind === "throttled") setFailure(throttleMessage(e));
    else if (e.kind === "unauthenticated") setFailure(wrongCredentials);
    else setFailure(e.message);
  }

  async function onEmail(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const value = email.trim();
    if (!/^[^\s@]+@[^\s@]+$/.test(value)) {
      setEmailError("Enter an email address, like name@example.com.");
      return;
    }
    setEmailError(null);
    setBusy(true);
    try {
      const method: SignInMethodView = await controller.discover(value);
      setStep(method.kind === "sso" ? { kind: "sso", providerName: method.providerName } : { kind: "password" });
    } catch (err) {
      if (err instanceof ApiError && err.kind === "invalid") setEmailError("Enter an email address, like name@example.com.");
      else fail(err);
    } finally {
      setBusy(false);
    }
  }

  async function onPassword(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    setBusy(true);
    try {
      await controller.signIn(email.trim(), password);
    } catch (err) {
      fail(err);
      passwordRef.current?.focus();
    } finally {
      setPassword("");
      setBusy(false);
    }
  }

  function changeEmail() {
    setStep({ kind: "email" });
    setPassword("");
    setFailure(null);
  }

  const ended =
    session.endedReason === "expired" ? (
      <Alert tone="info" title="Your session has ended">
        <p>Sign in again to continue.</p>
      </Alert>
    ) : session.endedReason === "unavailable" ? (
      <Alert tone="warning" title="The server could not be reached">
        <p>Check your connection, then sign in.</p>
      </Alert>
    ) : null;
  const failureAlert = failure ? (
    <Alert tone="danger" title={failure} focusOnMount />
  ) : null;

  if (step.kind === "sso") {
    return (
      <AuthLayout title="Sign in" stepKey="sso">
        <Alert tone="info" title={`Your organization signs in with ${step.providerName || "its identity provider"}`}>
          <p>Single sign-on is not available in this console yet. Ask your administrator how to sign in.</p>
        </Alert>
        <div className="form-actions">
          <button type="button" className="button" onClick={changeEmail}>
            Use a different email
          </button>
        </div>
      </AuthLayout>
    );
  }

  if (step.kind === "password") {
    return (
      <AuthLayout title="Enter your password" stepKey="password">
        {failureAlert}
        <form onSubmit={onPassword} noValidate>
          <TextField label="Email" type="email" name="username" autoComplete="username" value={email} readOnly />
          <TextField
            label="Password"
            type="password"
            name="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            inputRef={passwordRef}
            required
          />
          <div className="form-actions">
            <button type="submit" className="button button-primary" disabled={busy || throttle.blocked || password === ""}>
              Sign in
            </button>
            <button type="button" className="link-button" onClick={changeEmail}>
              Use a different email
            </button>
          </div>
        </form>
        <p className="auth-footer">
          <Link to="/forgot-password">Forgot password?</Link>
        </p>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Sign in" stepKey="email">
      {ended}
      {failureAlert}
      <form onSubmit={onEmail} noValidate>
        <TextField
          label="Email"
          type="email"
          name="username"
          autoComplete="username"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={emailError}
          required
        />
        <div className="form-actions">
          <button type="submit" className="button button-primary" disabled={busy || throttle.blocked}>
            Continue
          </button>
        </div>
      </form>
      <p className="auth-footer">
        <Link to="/forgot-password">Forgot password?</Link>
      </p>
    </AuthLayout>
  );
}
