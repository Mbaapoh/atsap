import { useId, useState, type FormEvent } from "react";
import { ApiError } from "../../api/errors";
import { DeviceType } from "../../api/gen/atsapbx/v1/pbx_pb";
import { Alert } from "../../components/Alert";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useCreateExtension, type Credentials } from "./adapter";
import { deviceLabel } from "./copy";

// The server's own bounds (pbx/domain/extension.go), checked here first so
// the form can say what is wrong; the server still decides.
const NUMBER = /^[0-9]{2,20}$/;
const NAME_MAX = 100;

export function AddExtensionDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (c: Credentials) => void }) {
  const titleId = useId();
  const deviceId = useId();
  const { create } = useCreateExtension();
  const [number, setNumber] = useState("");
  const [name, setName] = useState("");
  const [device, setDevice] = useState<DeviceType>(DeviceType.SIP);
  const [errors, setErrors] = useState<{ number?: string; name?: string }>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function close() {
    setNumber("");
    setName("");
    setDevice(DeviceType.SIP);
    setErrors({});
    setFailure(null);
    onClose();
  }

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const found = {
      number: NUMBER.test(number) ? undefined : "Use 2 to 20 digits, like 1001.",
      name: !name.trim() ? "Enter a name, like Reception." : name.trim().length > NAME_MAX ? `Use at most ${NAME_MAX} characters.` : undefined,
    };
    setErrors(found);
    if (found.number || found.name) return;
    setBusy(true);
    try {
      const credentials = await create({ number, name: name.trim(), device });
      close();
      onCreated(credentials);
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      if (e.kind === "exists") setErrors({ number: `Extension ${number} already exists in this tenant.` });
      else if (e.kind === "conflict") setFailure("The licence allows no more extensions. Free one up or extend the licence.");
      else if (e.kind === "forbidden") setFailure("Your role cannot add extensions.");
      else if (e.kind === "invalid") setFailure("Some details are not valid. Check the number and name.");
      else setFailure(e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open={open} onClose={() => (busy ? undefined : close())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Add an extension
      </h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField label="Number" inputMode="numeric" autoComplete="off" value={number} onChange={(e) => setNumber(e.target.value.trim())} error={errors.number} required />
          <TextField label="Name" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors.name} required />
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
          <button type="button" className="button" onClick={close} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy} aria-busy={busy || undefined}>
            Add extension
          </button>
        </div>
      </form>
    </Modal>
  );
}
