import { useId, useState, type FormEvent } from "react";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { passwordProblem } from "../auth/adapter";
import { usePermissions } from "../auth/permissions";
import { useAddUser } from "./adapter";

const EMAIL = /^[^\s@]+@[^\s@]+$/;

// AddUserDialog adds a person to this tenant with one role. The copy never
// says whether the email already has an account elsewhere: the same
// words, and the same confirmation, apply either way (D-61).
export function AddUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const titleId = useId();
  const roleId = useId();
  const { grantableRoles } = usePermissions();
  const { add } = useAddUser();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState("AGENT");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<{ email?: string; name?: string; password?: string }>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function reset() {
    setEmail("");
    setName("");
    setRole("AGENT");
    setPassword("");
    setErrors({});
    setFailure(null);
  }

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const found = {
      email: EMAIL.test(email.trim()) ? undefined : "Enter an email address, like name@example.com.",
      name: name.trim() ? undefined : "Enter the person's name.",
      password: password ? (passwordProblem(password) ?? undefined) : undefined,
    };
    setErrors(found);
    if (found.email || found.name || found.password) return;
    setBusy(true);
    try {
      await add({ email: email.trim(), displayName: name.trim(), role, initialPassword: password });
      toast.show(`${name.trim()} added to this tenant.`);
      reset();
      onClose();
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      if (e.kind === "exists") setErrors({ email: "This person is already a member of this tenant." });
      else if (e.kind === "invalid") setFailure("Some details are not valid. Check the email address and password.");
      else if (e.kind === "forbidden") setFailure("Your role cannot add people to this tenant.");
      else setFailure(e.message);
    } finally {
      setPassword("");
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={() => {
        if (busy) return;
        reset();
        onClose();
      }}
      labelledBy={titleId}
    >
      <h2 id={titleId} className="modal-title">
        Add a user
      </h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField label="Email" type="email" autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} error={errors.email} required />
          <TextField label="Name" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors.name} required />
          <div className="field">
            <label className="field-label" htmlFor={roleId}>
              Role
            </label>
            <select id={roleId} className="input" value={role} onChange={(e) => setRole(e.target.value)}>
              {grantableRoles.map((r) => (
                <option key={r.role} value={r.role}>
                  {r.label} — {r.description}
                </option>
              ))}
            </select>
          </div>
          <TextField
            label="Initial password (optional)"
            type="password"
            autoComplete="new-password"
            hint="Leave empty to invite a new person by email. Someone who already has an account keeps their own password."
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            error={errors.password}
          />
        </div>
        <div className="modal-actions">
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={() => {
              reset();
              onClose();
            }}
          >
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy} aria-busy={busy || undefined}>
            Add user
          </button>
        </div>
      </form>
    </Modal>
  );
}
