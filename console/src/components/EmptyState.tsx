import type { ReactNode } from "react";

// EmptyState says what will appear here and how to add the first item —
// never a blank table.
export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <p className="empty-title">{title}</p>
      {children ? <div className="empty-text">{children}</div> : null}
      {action ? <div className="empty-action">{action}</div> : null}
    </div>
  );
}
