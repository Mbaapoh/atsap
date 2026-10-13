import { useEffect, useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { useEmergencyChanges } from "./adapter";
import { INBOUND_NUMBER } from "./InboundDialogs";

// COUNTRY is ISO 3166-1 alpha-2, upper case (pbx/domain ValidateCountryCode).
const COUNTRY = /^[A-Z]{2}$/;

function failureText(err: unknown): string {
  const e = toApiError(err);
  if (e.kind === "forbidden") return "Your role cannot change emergency settings.";
  if (e.kind === "invalid") return "Some details are not valid. Check them and try again.";
  return e.message;
}

// One small form shared by "set country" and "add number": a single field,
// checked here, then sent as typed (trimmed).
function SingleFieldDialog({
  open,
  title,
  label,
  hint,
  submitLabel,
  initial,
  normalize,
  check,
  send,
  explain,
  onClose,
}: {
  open: boolean;
  title: string;
  label: string;
  hint: string;
  submitLabel: string;
  initial: string;
  normalize: (v: string) => string;
  check: (v: string) => string | undefined;
  send: (v: string) => Promise<string>;
  explain: (err: unknown) => { field?: string; failure?: string };
  onClose: () => void;
}) {
  const titleId = useId();
  const toast = useToast();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | undefined>();
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setValue(initial);
    setError(undefined);
    setFailure(null);
  }, [open, initial]);

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const v = normalize(value);
    const problem = check(v);
    setError(problem);
    if (problem) return;
    setBusy(true);
    try {
      toast.show(await send(v));
      onClose();
    } catch (err) {
      const { field, failure: f } = explain(err);
      if (field) setError(field);
      else setFailure(f ?? failureText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open={open} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        {title}
      </h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField label={label} autoComplete="off" spellCheck={false} hint={hint} value={value} onChange={(e) => setValue(e.target.value)} error={error} required />
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy}>
            {submitLabel}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function CountryDialog({ open, current, onClose }: { open: boolean; current: string; onClose: () => void }) {
  const { setCountry } = useEmergencyChanges();
  return (
    <SingleFieldDialog
      open={open}
      title={current ? "Change the emergency country" : "Set the emergency country"}
      label="Country code"
      hint="Two letters, like CM or US. That country's emergency numbers join 112 from the next call on."
      submitLabel="Save country"
      initial={current}
      normalize={(v) => v.trim().toUpperCase()}
      check={(v) => (COUNTRY.test(v) ? undefined : "Use the two-letter ISO country code, like CM.")}
      send={async (v) => {
        await setCountry(v);
        return `Emergency country set to ${v}.`;
      }}
      explain={(err) => (toApiError(err).kind === "invalid" ? { field: "That is not a two-letter ISO country code." } : {})}
      onClose={onClose}
    />
  );
}

export function AddEmergencyNumberDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { add } = useEmergencyChanges();
  return (
    <SingleFieldDialog
      open={open}
      title="Add an emergency number"
      label="Number"
      hint="A number your country's emergency services answer that is not already listed. Calls to it are never refused by the licence."
      submitLabel="Add number"
      initial=""
      normalize={(v) => v.trim()}
      check={(v) => (INBOUND_NUMBER.test(v) ? undefined : "Use digits only, with an optional leading +, like 1500.")}
      send={async (v) => {
        await add(v);
        return `Emergency number ${v} added.`;
      }}
      explain={(err) => {
        const kind = toApiError(err).kind;
        if (kind === "conflict") return { field: "This number is already a built-in emergency number." };
        if (kind === "invalid") return { field: "Use digits only, with an optional leading +, like 1500." };
        return {};
      }}
      onClose={onClose}
    />
  );
}

export function RemoveEmergencyNumberDialog({ number, onClose }: { number: string | null; onClose: () => void }) {
  const { remove } = useEmergencyChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!number) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove(number);
      toast.show(`Emergency number ${number} removed.`);
      onClose();
    } catch (err) {
      const kind = toApiError(err).kind;
      setFailure(
        kind === "conflict"
          ? "This number is built in and cannot be removed."
          : kind === "not-found"
            ? "This number is no longer in the list. Reload to see the current set."
            : failureText(err),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={number !== null}
      title={`Remove emergency number ${number ?? ""}?`}
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
        <p>Calls to {number} are no longer treated as emergency calls: the licence and calling rules apply to them like any other call.</p>
      )}
    </ConfirmDialog>
  );
}
