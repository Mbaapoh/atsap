import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { CloseIcon, ToneIcon } from "./icons";

type ToastTone = "success" | "info" | "warning";

interface ToastItem {
  id: number;
  tone: ToastTone;
  message: string;
}

interface ToastApi {
  // show announces a completed mutation in a few plain words — "Extension
  // 1001 created" — never a secret, a licence key or a raw error
  // (HLD-19 §4). Failures are page-level alerts, not toasts.
  show: (message: string, tone?: ToastTone) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

// TOAST_MS is how long a toast stays. It pauses while the pointer or
// focus is on it, so nobody loses a message they are reading.
export const TOAST_MS = 6000;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);
  const dismiss = useCallback((id: number) => setItems((all) => all.filter((t) => t.id !== id)), []);
  const show = useCallback((message: string, tone: ToastTone = "success") => {
    const id = nextId.current++;
    setItems((all) => [...all, { id, tone, message }]);
  }, []);
  const api = useMemo(() => ({ show }), [show]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-region" role="status" aria-live="polite" aria-label="Notifications">
        {items.map((t) => (
          <ToastView key={t.id} item={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function ToastView({ item, onDismiss }: { item: ToastItem; onDismiss: (id: number) => void }) {
  const [paused, setPaused] = useState(false);
  useEffect(() => {
    if (paused) return;
    const timer = setTimeout(() => onDismiss(item.id), TOAST_MS);
    return () => clearTimeout(timer);
  }, [paused, item.id, onDismiss]);
  return (
    <div
      className={`toast toast-${item.tone}`}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      <ToneIcon tone={item.tone} />
      <p className="toast-message">{item.message}</p>
      <button type="button" className="icon-button" onClick={() => onDismiss(item.id)} aria-label="Dismiss notification">
        <CloseIcon />
      </button>
    </div>
  );
}

export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) {
    throw new Error("useToast must be used inside ToastProvider");
  }
  return api;
}
