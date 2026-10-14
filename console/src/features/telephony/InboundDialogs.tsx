import { useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import type { ExtensionEntry } from "../extensions/adapter";
import { useInboundChanges, type InboundRow } from "./adapter";

// INBOUND_NUMBER is the server's rule (pbx/domain ValidateDestination):
// digits, an optional leading + that is not stored, at most 32 digits.
export const INBOUND_NUMBER = /^\+?[0-9]{1,32}$/;

export function extensionLabel(e: ExtensionEntry | undefined): string {
  if (!e) return "an extension not shown";
  return e.name ? `${e.number} · ${e.name}` : e.number;
}

function failureText(err: unknown): string {
  const e = toApiError(err);
  if (e.kind === "forbidden") return "Your role cannot change inbound numbers.";
  if (e.kind === "not-found") return "That extension or number no longer exists. Reload and try again.";
  if (e.kind === "invalid") return "Some details are not valid. Check the number.";
  return e.message;
}

// AssignNumberDialog points one of the tenant's carrier numbers (a DID) at
// an extension. The carrier must already deliver the number to a trunk;
// this only says which extension rings.
type AssignNumberDialogProps = { open: boolean; extensions: ExtensionEntry[]; onClose: () => void };

// The form is mounted only while open, so each opening starts empty with
// the first extension chosen — and a refetch of the extension list while
// someone types no longer resets what they typed.
export function AssignNumberDialog(props: AssignNumberDialogProps) {
  if (!props.open) return null;
  return <AssignNumberForm {...props} />;
}

function AssignNumberForm({ open, extensions, onClose }: AssignNumberDialogProps) {
  const titleId = useId();
  const selectId = useId();
  const { create } = useInboundChanges();
  const toast = useToast();
  const [number, setNumber] = useState("");
  const [extensionId, setExtensionId] = useState(extensions[0]?.id ?? "");
  const [error, setError] = useState<string | undefined>();
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);


  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const n = number.trim();
    if (!INBOUND_NUMBER.test(n)) {
      setError("Use digits only, with an optional leading +, like +237222000111.");
      return;
    }
    setError(undefined);
    setBusy(true);
    try {
      await create({ number: n, extensionId });
      toast.show(`${n} now rings ${extensionLabel(extensions.find((e) => e.id === extensionId))}.`);
      onClose();
    } catch (err) {
      if (toApiError(err).kind === "exists") setError("This number is already assigned in this tenant. Remove it first to move it.");
      else setFailure(failureText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open={open} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Assign an inbound number
      </h2>
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField
            label="Number"
            inputMode="tel"
            autoComplete="off"
            spellCheck={false}
            hint="The public number your carrier sends calls to. A leading + is optional and not stored."
            value={number}
            onChange={(e) => setNumber(e.target.value)}
            error={error}
            required
          />
          <div className="field">
            <label className="field-label" htmlFor={selectId}>
              Rings extension
            </label>
            <select id={selectId} className="input" value={extensionId} onChange={(e) => setExtensionId(e.target.value)}>
              {extensions.map((e) => (
                <option key={e.id} value={e.id}>
                  {extensionLabel(e)}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy}>
            Assign number
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function RemoveNumberDialog({ target, extension, onClose }: { target: InboundRow | null; extension: string; onClose: () => void }) {
  const { remove } = useInboundChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!target) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove(target.id);
      toast.show(`${target.number} removed.`);
      onClose();
    } catch (err) {
      setFailure(failureText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={target !== null}
      title={`Remove ${target?.number ?? ""}?`}
      confirmLabel="Remove number"
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
        <p>Calls to {target?.number} stop ringing extension {extension}. The extension itself keeps working.</p>
      )}
    </ConfirmDialog>
  );
}
