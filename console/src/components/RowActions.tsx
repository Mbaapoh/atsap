import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";

export interface RowAction {
  label: string;
  onSelect: () => void;
  // destructive actions are styled as such and listed last.
  destructive?: boolean;
  disabled?: boolean;
}

// RowActions is a row's action menu (the WAI-ARIA menu button pattern):
// the button names the row ("Actions for 1001"); Enter, Space or Arrow
// Down opens the menu on its first item; arrows move, Home/End jump,
// Escape closes and returns focus to the button, and choosing an item or
// clicking elsewhere closes it.
export function RowActions({ label, actions }: { label: string; actions: RowAction[] }) {
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLUListElement>(null);
  const id = useId();
  const items = () => Array.from(menu.current?.querySelectorAll<HTMLButtonElement>("button:not([disabled])") ?? []);

  useEffect(() => {
    if (!open) return;
    items()[0]?.focus();
    const onDown = (e: MouseEvent) => {
      if (!menu.current?.contains(e.target as Node) && !button.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  function close(restoreFocus: boolean) {
    setOpen(false);
    if (restoreFocus) button.current?.focus();
  }

  function onMenuKey(e: KeyboardEvent) {
    const list = items();
    const at = list.indexOf(document.activeElement as HTMLButtonElement);
    const move = (to: number) => {
      e.preventDefault();
      list[(to + list.length) % list.length]?.focus();
    };
    if (e.key === "ArrowDown") move(at + 1);
    else if (e.key === "ArrowUp") move(at - 1);
    else if (e.key === "Home") move(0);
    else if (e.key === "End") move(list.length - 1);
    else if (e.key === "Escape") {
      e.preventDefault();
      close(true);
    } else if (e.key === "Tab") close(false);
  }

  return (
    <div className="row-actions-menu">
      <button
        ref={button}
        type="button"
        className="button"
        aria-label={`Actions for ${label}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        Actions
      </button>
      {open ? (
        <ul ref={menu} id={id} role="menu" aria-label={`Actions for ${label}`} className="menu" onKeyDown={onMenuKey}>
          {[...actions.filter((a) => !a.destructive), ...actions.filter((a) => a.destructive)].map((a) => (
            <li key={a.label} role="none">
              <button
                type="button"
                role="menuitem"
                className={a.destructive ? "menu-item menu-item-danger" : "menu-item"}
                disabled={a.disabled}
                onClick={() => {
                  close(true);
                  a.onSelect();
                }}
              >
                {a.label}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
