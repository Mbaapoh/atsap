import { useState } from "react";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { OneTimeSecret } from "../../components/OneTimeSecret";
import { useToast } from "../../components/Toast";
import { useMembershipStatus, useResetPassword, type UserRow } from "./adapter";
import { temporaryPassword } from "./temporaryPassword";

function displayName(u: UserRow): string {
  return u.name || u.email;
}

// DisableDialog confirms disabling a membership. Re-enabling is not
// destructive and needs no confirmation (it is done from the row menu).
export function DisableDialog({ user, onClose }: { user: UserRow | null; onClose: () => void }) {
  const { setActive } = useMembershipStatus();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const name = user ? displayName(user) : "";

  async function onConfirm() {
    if (!user) return;
    setBusy(true);
    setFailure(null);
    try {
      await setActive({ principalId: user.principalId, active: false });
      toast.show(`${name} disabled in this tenant.`);
      onClose();
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      setFailure(e.kind === "forbidden" ? "Your role cannot disable people in this tenant." : e.kind === "conflict" ? `${name} is this tenant's last active administrator and cannot be disabled.` : e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={user !== null}
      title={`Disable ${name} in this tenant?`}
      confirmLabel="Disable"
      destructive
      busy={busy}
      onConfirm={() => void onConfirm()}
      onCancel={() => {
        setFailure(null);
        onClose();
      }}
    >
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : <p>{name} can no longer sign in to this tenant. Their other tenants are not affected.</p>}
    </ConfirmDialog>
  );
}

// ResetPasswordFlow confirms, generates a temporary password, sets it, and
// shows it once for the administrator to pass on. If the server refuses,
// the generated password is discarded and never shown.
export function ResetPasswordFlow({ user, onClose }: { user: UserRow | null; onClose: () => void }) {
  const { resetPassword } = useResetPassword();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ name: string; password: string } | null>(null);
  const name = user ? displayName(user) : "";

  async function onConfirm() {
    if (!user) return;
    setBusy(true);
    setFailure(null);
    const password = temporaryPassword();
    try {
      await resetPassword(user.principalId, password);
      setIssued({ name, password });
      onClose();
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      setFailure(e.kind === "forbidden" ? "Your role cannot reset this person's password. A platform administrator can." : e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <ConfirmDialog
        open={user !== null}
        title={`Reset ${name}'s password?`}
        confirmLabel="Reset password"
        destructive
        busy={busy}
        onConfirm={() => void onConfirm()}
        onCancel={() => {
          setFailure(null);
          onClose();
        }}
      >
        {failure ? (
          <Alert tone="danger" title={failure} focusOnMount />
        ) : (
          <p>{name} is signed out everywhere and must choose a new password at their next sign-in. You will be shown a temporary password to give them.</p>
        )}
      </ConfirmDialog>
      <OneTimeSecret
        open={issued !== null}
        title={`Temporary password for ${issued?.name ?? ""}`}
        intro={<p>Give this to {issued?.name} privately. It is shown only now; they must replace it when they sign in.</p>}
        fields={issued ? [{ label: "Temporary password", value: issued.password }] : []}
        onDone={() => setIssued(null)}
      />
    </>
  );
}
