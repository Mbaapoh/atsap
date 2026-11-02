import { useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { usePermissions } from "../auth/permissions";
import { useExtensionDirectory } from "../extensions/adapter";
import { usePlaceCall, useRecentDestinations } from "./adapter";
import { DIALLED } from "./copy";
import { useCallSession } from "./session";
import type { ExtensionEntry } from "../extensions/adapter";

// The New call entry point is in the top bar and on Call history (D-79,
// design D6). It renders the button only; the dialog mounts on open so no
// extra reads happen on ordinary pages.

// lastUsed is deliberately in memory only: which extension this
// administrator used last is presentation state, not configuration.
let lastUsed: string | null = null;

export function NewCallAction({ label = "New call" }: { label?: string }) {
  const { canAdminister } = usePermissions();
  const { session, phase } = useCallSession();
  const [open, setOpen] = useState(false);
  if (!canAdminister) return null;

  const inCall = session !== null && phase !== "ended";
  return (
    <>
      <button
        type="button"
        className="button button-primary new-call-button"
        onClick={() => setOpen(true)}
        disabled={inCall}
        title={inCall ? "A call is in progress" : undefined}
      >
        {label}
      </button>
      {open && !inCall ? <NewCallDialog onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function extensionLabel(e: ExtensionEntry): string {
  const name = e.name ? ` · ${e.name}` : "";
  return e.registration === "registered" ? `${e.number}${name}` : `${e.number}${name} — No device registered`;
}

function NewCallDialog({ onClose }: { onClose: () => void }) {
  const titleId = useId();
  const selectId = useId();
  const { canAdminister } = usePermissions();
  const directory = useExtensionDirectory(canAdminister);
  const recents = useRecentDestinations();
  const place = usePlaceCall();
  const session = useCallSession();
  const extensions = directory.data ?? [];
  const registered = extensions.filter((e) => e.registration === "registered");
  const [search, setSearch] = useState("");
  const [from, setFrom] = useState<string>(() => (lastUsed && registered.some((e) => e.id === lastUsed) ? lastUsed : ""));
  const [destination, setDestination] = useState("");
  const [numberError, setNumberError] = useState<string | undefined>();
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // The directory may arrive after the dialog opens; choose the first
  // registered extension once, unless the person already picked one.
  const chosen = extensions.find((e) => e.id === from) ?? (from === "" ? registered[0] : undefined);
  const chosenId = chosen?.id ?? "";
  const canCall = chosen?.registration === "registered";

  const needle = search.trim().toLowerCase();
  const filtered = needle ? extensions.filter((e) => e.number.toLowerCase().includes(needle) || e.name.toLowerCase().includes(needle)) : extensions;
  // Keep the chosen option visible even when the search excludes it.
  const options = chosen && !filtered.some((e) => e.id === chosen.id) ? [chosen, ...filtered] : filtered;

  function append(digit: string) {
    setDestination((d) => d + digit);
  }

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
      const callId = await place.mutateAsync({ extensionId: chosen.id, destination: dialled });
      lastUsed = chosen.id;
      session.start({ callId, from: { id: chosen.id, number: chosen.number, name: chosen.name }, destination: dialled, startedAt: new Date() });
      onClose();
    } catch (err) {
      setFailure(err instanceof Error ? err.message : "The call could not be placed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={() => (busy ? undefined : onClose())} labelledBy={titleId} className="modal new-call-dialog">
      <h2 id={titleId} className="modal-title">
        New call
      </h2>
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          {directory.isError ? <Alert tone="danger" title="Extensions could not be loaded">Reload the page and try again.</Alert> : null}
          {directory.isPending ? <p role="status">Loading extensions…</p> : null}
          {!directory.isPending && registered.length === 0 ? (
            <Alert tone="info" title="No extension has a registered device">
              <p>Register a phone to an extension first. Only an extension with a device can place a call.</p>
            </Alert>
          ) : null}

          <label className="field-label" htmlFor={selectId}>
            From extension
          </label>
          <input
            type="search"
            className="input"
            aria-label="Search extensions"
            placeholder="Search by number or name"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            autoComplete="off"
          />
          <select
            id={selectId}
            className="input"
            value={chosenId}
            onChange={(e) => setFrom(e.target.value)}
            aria-describedby={`${selectId}-hint`}
          >
            {options.length === 0 ? <option value="">No extensions</option> : null}
            {options.map((e) => (
              <option key={e.id} value={e.id} disabled={e.registration !== "registered"}>
                {extensionLabel(e)}
              </option>
            ))}
          </select>
          <p className="field-hint" id={`${selectId}-hint`}>
            Your phone rings first. Answer it to be connected.
          </p>

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

          <div className="dialer-grid">
            <div className="dialpad" role="group" aria-label="Dial pad">
              {["1", "2", "3", "4", "5", "6", "7", "8", "9", "+", "0"].map((key) => (
                <button key={key} type="button" className="button dialpad-key" onClick={() => append(key)} aria-label={`Add ${key}`}>
                  {key}
                </button>
              ))}
              <button type="button" className="button dialpad-key" onClick={() => setDestination((d) => d.slice(0, -1))} aria-label="Delete last digit">
                ⌫
              </button>
            </div>
            <div className="recents" role="group" aria-label="Recent numbers">
              <p className="field-label">Recent numbers</p>
              {recents.data?.length ? (
                <ul className="recents-list">
                  {recents.data.map((r) => (
                    <li key={r.number}>
                      <button type="button" className="link-button" onClick={() => setDestination(r.number)}>
                        {r.number}
                      </button>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="field-hint">Numbers you call appear here.</p>
              )}
            </div>
          </div>
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

// hangupText is the fixed copy for a failed row hang-up (the session bar
// has its own error surface).
export function hangupText(err: unknown): string {
  const e = toApiError(err);
  if (e.kind === "forbidden") return "Your role cannot end this call.";
  if (e.kind === "not-found") return "This call no longer exists. Reload the list.";
  return "The call could not be ended. Try again.";
}
