import { useEffect, useRef, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";

// FOCUSABLE lists what Tab can land on inside a modal.
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  // labelledBy is the id of the element naming the modal (its heading).
  labelledBy: string;
  describedBy?: string;
  // initialFocus is where focus starts. For a destructive confirmation it
  // is the safe choice (Cancel), so a stray Enter never deletes.
  initialFocus?: RefObject<HTMLElement | null>;
  className?: string;
  children: ReactNode;
}

// Modal is the console's one modal primitive, used by the confirmation
// dialog and the small-screen navigation drawer (HLD-19 §7). Hand-built
// rather than <dialog> so its behaviour is the same in every browser and in
// tests: while open it
//  - makes the rest of the page inert (no focus, no clicks, hidden from
//    assistive technology),
//  - keeps Tab and Shift+Tab inside itself,
//  - closes on Escape,
//  - and returns focus to whatever opened it when it closes.
export function Modal({ open, onClose, labelledBy, describedBy, initialFocus, className, children }: ModalProps) {
  const panel = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const app = document.getElementById("root");
    app?.setAttribute("inert", "");
    app?.setAttribute("aria-hidden", "true");

    const first = initialFocus?.current ?? panel.current?.querySelector<HTMLElement>(FOCUSABLE) ?? panel.current;
    first?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab" || !panel.current) return;
      const items = Array.from(panel.current.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const firstItem = items[0]!;
      const lastItem = items[items.length - 1]!;
      if (e.shiftKey && document.activeElement === firstItem) {
        e.preventDefault();
        lastItem.focus();
      } else if (!e.shiftKey && document.activeElement === lastItem) {
        e.preventDefault();
        firstItem.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      app?.removeAttribute("inert");
      app?.removeAttribute("aria-hidden");
      opener?.focus();
    };
  }, [open, initialFocus]);

  if (!open) return null;
  return createPortal(
    <div className="modal-layer">
      <div className="modal-backdrop" onClick={() => onCloseRef.current()} aria-hidden="true" />
      <div
        ref={panel}
        className={className ?? "modal"}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        tabIndex={-1}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}
