import { useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { passwordProblem, PASSWORD_MAX, PASSWORD_MIN, useSession, useSessionController } from "./adapter";
import { throttleMessage } from "./copy";
import { useThrottle } from "./useThrottle";

// ChangePasswordForm sets a new password, given the current one. The
// server ends every other session of the person; this one continues. For
// a must-change password it is the way into the rest of the console. The
// password values live in this form's state only and are cleared after
// every attempt.
export function ChangePasswordForm() {
  const session = useSession();
  const controller = useSessionController();
  const toast = useToast();
  const navigate = useNavigate();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<{ current?: string; next?: string; confirm?: string }>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const throttle = useThrottle();
  const currentRef = useRef<HTMLInputElement>(null);
  const mustChange = session.passwordChangeRequired;

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const found = {
      current: current === "" ? "Enter your current password." : undefined,
      next: passwordProblem(next) ?? (next === current && next !== "" ? "Choose a password different from the current one." : undefined),
      confirm: undefined as string | undefined,
    };
    if (!found.next && next !== confirm) found.confirm = "The passwords do not match.";
    setErrors(found);
    if (found.current || found.next || found.confirm) return;

    setBusy(true);
    try {
      await controller.changePassword(current, next);
      toast.show("Password changed. Your other sessions have been signed out.");
      setErrors({});
      if (mustChange) await navigate("/", { replace: true });
    } catch (err) {
      const e = toApiError(err);
      throttle.hold(e);
      if (e.kind === "unauthenticated") {
        setErrors({ current: "The current password is not correct." });
        currentRef.current?.focus();
      } else if (e.kind === "invalid") {
        setErrors({ next: `Use ${PASSWORD_MIN} to ${PASSWORD_MAX} characters.` });
      } else {
        setFailure(e.kind === "throttled" ? throttleMessage(e) : e.message);
      }
    } finally {
      setCurrent("");
      setNext("");
      setConfirm("");
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby="change-password-title" className="account-section">
      <h2 id="change-password-title">{mustChange ? "Choose your own password" : "Change password"}</h2>
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      <form onSubmit={onSubmit} noValidate>
        <TextField
          label={mustChange ? "Temporary password" : "Current password"}
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          error={errors.current}
          inputRef={currentRef}
          required
        />
        <TextField
          label="New password"
          type="password"
          autoComplete="new-password"
          hint={`${PASSWORD_MIN} to ${PASSWORD_MAX} characters. A passphrase of a few words is easy to remember.`}
          value={next}
          onChange={(e) => setNext(e.target.value)}
          error={errors.next}
          required
        />
        <TextField
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          error={errors.confirm}
          required
        />
        <div className="form-actions">
          <button type="submit" className="button button-primary" disabled={busy || throttle.blocked} aria-busy={busy || undefined}>
            Change password
          </button>
        </div>
      </form>
    </section>
  );
}
