import { useId, useRef, type ReactNode } from "react";
import { Modal } from "./Modal";

interface ConfirmDialogProps {
  open: boolean;
  // title is a question naming the action and the resource, e.g.
  // "Delete route +44?" (CONSOLE-DESIGN: confirmations name what they
  // affect).
  title: string;
  children?: ReactNode;
  // confirmLabel is an explicit verb — "Delete route", "Apply licence" —
  // never "OK" or "Yes".
  confirmLabel: string;
  destructive?: boolean;
  // busy disables both buttons while the request runs, so the action
  // cannot be sent twice.
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

// ConfirmDialog asks before an action that cannot be undone. Focus starts
// on Cancel for a destructive action, so a stray Enter changes nothing.
export function ConfirmDialog({ open, title, children, confirmLabel, destructive = false, busy = false, onConfirm, onCancel }: ConfirmDialogProps) {
  const titleId = useId();
  const bodyId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  return (
    <Modal
      open={open}
      onClose={() => {
        if (!busy) onCancel();
      }}
      labelledBy={titleId}
      describedBy={children ? bodyId : undefined}
      initialFocus={destructive ? cancelRef : confirmRef}
    >
      <h2 id={titleId} className="modal-title">
        {title}
      </h2>
      {children ? (
        <div id={bodyId} className="modal-body">
          {children}
        </div>
      ) : null}
      <div className="modal-actions">
        <button ref={cancelRef} type="button" className="button" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button
          ref={confirmRef}
          type="button"
          className={destructive ? "button button-danger" : "button button-primary"}
          onClick={onConfirm}
          disabled={busy}
          aria-busy={busy || undefined}
        >
          {confirmLabel}
        </button>
      </div>
    </Modal>
  );
}
