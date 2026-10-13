import { useEffect, useId, useState, type ReactNode } from "react";
import { useBlocker } from "react-router";
import { Alert } from "./Alert";
import { Modal } from "./Modal";

export interface SecretField {
  label: string;
  value: string;
  // sipUsername marks the one place the console may show an engine
  // endpoint id: an extension's SIP username is its endpoint id by
  // Asterisk's design (pbx/domain/credential.go), and a person configuring
  // a phone must be given it. The exposure scan allows the pattern inside
  // [data-sip-username] only (console-admin-screens D3).
  sipUsername?: boolean;
}

interface OneTimeSecretProps {
  open: boolean;
  title: string;
  // intro says what the values are and that they will not be shown again.
  intro: ReactNode;
  fields: SecretField[];
  onDone: () => void;
}

// OneTimeSecret shows values the server returns exactly once — an
// extension's SIP secret, a temporary password — until the person says
// they have saved them (console brief §6 rule 8). Until then it cannot be
// dismissed, in-app navigation is blocked, and leaving the page asks first.
// The values live in the caller's state only and go when it closes.
export function OneTimeSecret({ open, title, intro, fields, onDone }: OneTimeSecretProps) {
  const titleId = useId();
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);
  // The panel holds the page for as long as it is open: a navigation tried
  // meanwhile waits, and continues once the person has pressed Done.
  const holding = open;

  // Block in-app navigation while the panel is open.
  const blocker = useBlocker(holding);
  useEffect(() => {
    if (blocker.state === "blocked" && !holding) blocker.proceed();
  }, [blocker, holding]);

  // Ask before the browser leaves or reloads the page while open.
  useEffect(() => {
    if (!holding) return;
    const onUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", onUnload);
    return () => window.removeEventListener("beforeunload", onUnload);
  }, [holding]);

  useEffect(() => {
    if (!open) {
      setSaved(false);
      setCopied(null);
    }
  }, [open]);

  async function copy(field: SecretField) {
    try {
      await navigator.clipboard.writeText(field.value);
      setCopied(field.label);
    } catch {
      setCopied(null);
    }
  }

  return (
    <Modal open={open} onClose={() => undefined} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        {title}
      </h2>
      <div className="modal-body">
        {intro}
        {blocker.state === "blocked" ? (
          <Alert tone="warning" title="Save these details first">
            <p>They cannot be shown again. Copy them, confirm below, then continue.</p>
          </Alert>
        ) : null}
        <dl className="secret-fields">
          {fields.map((f) => (
            <div key={f.label} className="secret-field">
              <dt>{f.label}</dt>
              <dd>
                <code className="secret-value" {...(f.sipUsername ? { "data-sip-username": "" } : {})}>
                  {f.value}
                </code>
                <button type="button" className="button" onClick={() => void copy(f)} aria-label={`Copy ${f.label}`}>
                  {copied === f.label ? "Copied" : "Copy"}
                </button>
              </dd>
            </div>
          ))}
        </dl>
        <label className="secret-ack">
          <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} /> I have saved these details
        </label>
      </div>
      <div className="modal-actions">
        <button type="button" className="button button-primary" disabled={!saved} onClick={onDone}>
          Done
        </button>
      </div>
    </Modal>
  );
}
