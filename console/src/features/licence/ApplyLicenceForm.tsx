import { useEffect, useId, useState, type FormEvent } from "react";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { useToast } from "../../components/Toast";
import { useApplyLicence } from "./adapter";

// REVEAL_MS is how long a revealed key stays readable before it is masked
// again (console brief §6 rule 9: an explicit, temporary reveal).
export const REVEAL_MS = 30_000;

// ApplyLicenceForm takes a licence key and applies it after an explicit
// confirmation. The key is masked unless the person reveals it, lives in
// this form's state only, and is cleared after every attempt; it never
// appears in a toast, an error, a URL or a log.
export function ApplyLicenceForm() {
  const { apply } = useApplyLicence();
  const toast = useToast();
  const id = useId();
  const [key, setKey] = useState("");
  const [revealed, setRevealed] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [keyError, setKeyError] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    if (!revealed) return;
    const timer = window.setTimeout(() => setRevealed(false), REVEAL_MS);
    return () => window.clearTimeout(timer);
  }, [revealed]);

  function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    if (key.trim() === "") {
      setKeyError("Paste the licence key you were sent.");
      return;
    }
    setKeyError(null);
    setRevealed(false);
    setConfirming(true);
  }

  async function onConfirm() {
    setBusy(true);
    try {
      await apply(key.trim());
      setConfirming(false);
      toast.show("Licence applied.");
    } catch (err) {
      setConfirming(false);
      const e = err instanceof ApiError ? err : new ApiError("unknown");
      if (e.kind === "invalid") setKeyError("This key did not verify. Check that you pasted all of it, and that it was issued for this installation.");
      else if (e.kind === "forbidden") setFailure("Only a platform administrator can apply a licence.");
      else setFailure(e.message);
    } finally {
      setKey("");
      setBusy(false);
    }
  }

  const errorId = keyError ? `${id}-error` : undefined;
  return (
    <section aria-labelledby={`${id}-title`} className="account-section">
      <h2 id={`${id}-title`}>Apply licence</h2>
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      <form onSubmit={onSubmit} noValidate>
        <div className="field">
          <label className="field-label" htmlFor={`${id}-key`}>
            Licence key
          </label>
          <p className="field-hint" id={`${id}-hint`}>
            The key takes effect at once. Calls in progress continue.
          </p>
          <div className="secret-input">
            <input
              id={`${id}-key`}
              className="input"
              type={revealed ? "text" : "password"}
              autoComplete="off"
              spellCheck={false}
              autoCapitalize="off"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              aria-describedby={[`${id}-hint`, errorId].filter(Boolean).join(" ")}
              aria-invalid={keyError ? true : undefined}
            />
            <button type="button" className="button" aria-pressed={revealed} onClick={() => setRevealed((r) => !r)} disabled={key === ""}>
              {revealed ? "Hide" : "Show"}
            </button>
          </div>
          {keyError ? (
            <p className="field-error" id={errorId}>
              {keyError}
            </p>
          ) : null}
        </div>
        <div className="form-actions">
          <button type="submit" className="button button-primary" disabled={busy}>
            Apply licence
          </button>
        </div>
      </form>
      <ConfirmDialog
        open={confirming}
        title="Apply this licence?"
        confirmLabel="Apply licence"
        busy={busy}
        onConfirm={() => void onConfirm()}
        onCancel={() => setConfirming(false)}
      >
        <p>It replaces the licence in force as soon as it verifies. Calls in progress are not interrupted.</p>
      </ConfirmDialog>
    </section>
  );
}
