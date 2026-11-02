import { useId, useState, type FormEvent } from "react";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import type { ExtensionEntry } from "../extensions/adapter";
import { useHangupCall, usePlaceCall, type CallRow } from "./adapter";
import { DIALLED } from "./copy";

// The form is mounted only while open, so each opening starts clean and a
// background extension refresh cannot reset what someone typed.
export function PlaceCallDialog({ open, extensions, onClose }: { open: boolean; extensions: ExtensionEntry[]; onClose: () => void }) {
  if (!open) return null;
  return <PlaceCallForm extensions={extensions} onClose={onClose} />;
}

function extensionOption(e: ExtensionEntry): string {
  const name = e.name ? ` · ${e.name}` : "";
  return e.registration === "registered" ? `${e.number}${name}` : `${e.number}${name} — No device registered`;
}

function PlaceCallForm({ extensions, onClose }: { extensions: ExtensionEntry[]; onClose: () => void }) {
  const titleId = useId();
  const extensionId = useId();
  const place = usePlaceCall();
  const toast = useToast();
  const firstRegistered = extensions.find((e) => e.registration === "registered");
  const [from, setFrom] = useState(firstRegistered?.id ?? extensions[0]?.id ?? "");
  const [destination, setDestination] = useState("");
  const [numberError, setNumberError] = useState<string | undefined>();
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const chosen = extensions.find((e) => e.id === from);
  const canCall = chosen?.registration === "registered";

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const dialled = destination.trim();
    if (!DIALLED.test(dialled)) {
      setNumberError("Use digits with an optional leading +, like +237222000111.");
      return;
    }
    setNumberError(undefined);
    if (!chosen || chosen.registration !== "registered") {
      setFailure("Choose an extension with a registered device. Register a phone to it first.");
      return;
    }
    setBusy(true);
    try {
      await place.mutateAsync({ extensionId: chosen.id, destination: dialled });
      toast.show(`Calling ${dialled} — answer extension ${chosen.number} to connect.`);
      onClose();
    } catch (err) {
      setFailure(err instanceof Error ? err.message : "The call could not be placed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Place a call
      </h2>
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          {!firstRegistered ? (
            <Alert tone="info" title="No extension has a registered device">
              <p>Register a phone to an extension first. Only an extension with a device can place a call.</p>
            </Alert>
          ) : null}
          <div className="field">
            <label className="field-label" htmlFor={extensionId}>
              From extension
            </label>
            <select id={extensionId} className="input" value={from} onChange={(e) => setFrom(e.target.value)}>
              {extensions.map((e) => (
                <option key={e.id} value={e.id} disabled={e.registration !== "registered"}>
                  {extensionOption(e)}
                </option>
              ))}
            </select>
            <p className="field-hint">The platform rings this extension first; the number is dialled only when it answers.</p>
          </div>
          <TextField
            label="Number to call"
            inputMode="tel"
            autoComplete="off"
            spellCheck={false}
            hint="The digits as dialled: an extension number, or a number your outbound routes reach."
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            error={numberError}
            required
          />
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy || !canCall}>
            Call
          </button>
        </div>
      </form>
    </Modal>
  );
}

// HangupCallDialog asks before ending a call in front of everyone on it.
export function HangupCallDialog({ call, onClose }: { call: CallRow | null; onClose: () => void }) {
  const hangup = useHangupCall();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!call) return;
    setBusy(true);
    setFailure(null);
    try {
      await hangup.mutateAsync(call.id);
      toast.show("Call ended.");
      onClose();
    } catch (err) {
      setFailure(err instanceof Error ? err.message : "The call could not be ended.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={call !== null}
      title={call ? `Hang up the call with ${call.to || call.from || "this party"}?` : "Hang up this call?"}
      confirmLabel="Hang up"
      destructive
      busy={busy}
      onConfirm={() => void onConfirm()}
      onCancel={onClose}
    >
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      <p>Everyone on the call is disconnected.</p>
    </ConfirmDialog>
  );
}
