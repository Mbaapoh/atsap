import { useEffect, useId, useState, type FormEvent } from "react";
import { ApiError } from "../../api/errors";
import { DeviceType } from "../../api/gen/atsapbx/v1/pbx_pb";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { useExtensionChanges, type Credentials, type ExtensionRow } from "./adapter";
import { deviceLabel } from "./copy";

function failureText(err: unknown, action: string): string {
  const e = err instanceof ApiError ? err : new ApiError("unknown");
  if (e.kind === "forbidden") return `Your role cannot ${action} extensions.`;
  if (e.kind === "not-found") return "This extension no longer exists. It may have been removed.";
  return e.message;
}

// EditExtensionDialog changes the name and device type. The number is the
// extension's identity in this tenant and is not edited (UpdateExtension
// carries no number).
export function EditExtensionDialog({ extension, onClose }: { extension: ExtensionRow | null; onClose: () => void }) {
  const titleId = useId();
  const deviceId = useId();
  const { update } = useExtensionChanges();
  const toast = useToast();
  const [name, setName] = useState("");
  const [device, setDevice] = useState<DeviceType>(DeviceType.SIP);
  const [nameError, setNameError] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (extension) {
      setName(extension.name);
      setDevice(extension.device === DeviceType.UNSPECIFIED ? DeviceType.SIP : extension.device);
      setNameError(null);
      setFailure(null);
    }
  }, [extension]);

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    if (!extension) return;
    const trimmed = name.trim();
    if (!trimmed || trimmed.length > 100) {
      setNameError(!trimmed ? "Enter a name, like Reception." : "Use at most 100 characters.");
      return;
    }
    setBusy(true);
    setFailure(null);
    try {
      await update({ id: extension.id, name: trimmed, device });
      toast.show(`Extension ${extension.number} updated.`);
      onClose();
    } catch (err) {
      setFailure(failureText(err, "change"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open={extension !== null} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Edit extension {extension?.number}
      </h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField label="Name" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={nameError} required />
          <div className="field">
            <label className="field-label" htmlFor={deviceId}>
              Device
            </label>
            <select id={deviceId} className="input" value={device} onChange={(e) => setDevice(Number(e.target.value) as DeviceType)}>
              <option value={DeviceType.SIP}>{deviceLabel[DeviceType.SIP]}</option>
              <option value={DeviceType.WEBRTC}>{deviceLabel[DeviceType.WEBRTC]}</option>
            </select>
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy}>
            Save changes
          </button>
        </div>
      </form>
    </Modal>
  );
}

// RotateSecretDialog confirms issuing a new SIP password; the old one stops
// working at once, so the phone needs the new one to register again.
export function RotateSecretDialog({ extension, onClose, onRotated }: { extension: ExtensionRow | null; onClose: () => void; onRotated: (c: Credentials) => void }) {
  const { rotate } = useExtensionChanges();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!extension) return;
    setBusy(true);
    setFailure(null);
    try {
      const credentials = await rotate(extension.id);
      onClose();
      onRotated(credentials);
    } catch (err) {
      setFailure(failureText(err, "rotate passwords of"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={extension !== null}
      title={`Rotate the SIP password of extension ${extension?.number ?? ""}?`}
      confirmLabel="Rotate password"
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
        <p>The current password stops working at once. The phone stops registering until it is given the new one, which you will be shown once.</p>
      )}
    </ConfirmDialog>
  );
}

// DeleteExtensionDialog confirms deleting. Deprovisioning is immediate,
// and the server removes the extension's inbound numbers with it.
export function DeleteExtensionDialog({ extension, onClose }: { extension: ExtensionRow | null; onClose: () => void }) {
  const { remove } = useExtensionChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!extension) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove(extension.id);
      toast.show(`Extension ${extension.number} deleted.`);
      onClose();
    } catch (err) {
      setFailure(failureText(err, "delete"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={extension !== null}
      title={`Delete extension ${extension?.number ?? ""}?`}
      confirmLabel="Delete extension"
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
        <p>Its phone stops working at once, and any inbound numbers assigned to it are removed. This cannot be undone.</p>
      )}
    </ConfirmDialog>
  );
}
