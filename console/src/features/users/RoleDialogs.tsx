import { useId, useState } from "react";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { useToast } from "../../components/Toast";
import { roleLabel, usePermissions } from "../auth/permissions";
import { useRoleChanges, type RoleGrant, type UserRow } from "./adapter";

function displayName(u: UserRow): string {
  return u.name || u.email;
}

// GrantRoleDialog offers the roles this administrator may grant that the
// person does not already hold.
export function GrantRoleDialog({ user, onClose }: { user: UserRow | null; onClose: () => void }) {
  const titleId = useId();
  const selectId = useId();
  const { grantableRoles } = usePermissions();
  const { grant } = useRoleChanges();
  const toast = useToast();
  const held = new Set(user?.roles.map((r) => r.role));
  const offered = grantableRoles.filter((r) => !held.has(r.role));
  const [role, setRole] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const chosen = offered.find((r) => r.role === role) ?? offered[0];

  async function onGrant() {
    if (!user || !chosen) return;
    setBusy(true);
    setFailure(null);
    try {
      await grant({ principalId: user.principalId, grant: { role: chosen.role, scope: chosen.scope } });
      toast.show(`${chosen.label} granted to ${displayName(user)}.`);
      setRole("");
      onClose();
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      setFailure(e.kind === "forbidden" ? "Your role cannot grant that role." : e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open={user !== null} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Grant a role to {user ? displayName(user) : ""}
      </h2>
      <div className="modal-body">
        {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
        {offered.length === 0 ? (
          <p>{user ? displayName(user) : "This person"} already holds every role you can grant.</p>
        ) : (
          <div className="field">
            <label className="field-label" htmlFor={selectId}>
              Role
            </label>
            <select id={selectId} className="input" value={chosen?.role ?? ""} onChange={(e) => setRole(e.target.value)}>
              {offered.map((r) => (
                <option key={r.role} value={r.role}>
                  {r.label} — {r.description}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>
      <div className="modal-actions">
        <button type="button" className="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        <button type="button" className="button button-primary" onClick={() => void onGrant()} disabled={busy || !chosen}>
          Grant role
        </button>
      </div>
    </Modal>
  );
}

// RevokeRoleDialog confirms revoking one role. The server refuses to
// remove a tenant's last administrator; that refusal is explained in
// plain words, and nothing changes.
export function RevokeRoleDialog({ target, onClose }: { target: { user: UserRow; grant: RoleGrant } | null; onClose: () => void }) {
  const { revoke } = useRoleChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const label = target ? roleLabel(target.grant.role) : "";
  const name = target ? displayName(target.user) : "";

  async function onConfirm() {
    if (!target) return;
    setBusy(true);
    setFailure(null);
    try {
      await revoke({ principalId: target.user.principalId, grant: target.grant });
      toast.show(`${label} revoked from ${name}.`);
      onClose();
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      setFailure(
        e.kind === "conflict"
          ? `${name} is this tenant's last administrator. Make someone else a tenant administrator first.`
          : e.kind === "forbidden"
            ? "Your role cannot revoke that role."
            : e.message,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={target !== null}
      title={`Revoke ${label} from ${name}?`}
      confirmLabel="Revoke role"
      destructive
      busy={busy}
      onConfirm={() => void onConfirm()}
      onCancel={() => {
        setFailure(null);
        onClose();
      }}
    >
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : <p>{name} loses what this role allows at once, including in sessions already open.</p>}
    </ConfirmDialog>
  );
}
