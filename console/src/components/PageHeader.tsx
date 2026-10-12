import type { ReactNode } from "react";

// PageHeader is every screen's title row: the page heading the shell
// focuses after navigation, an optional description, and the screen's one
// primary action (CONSOLE-DESIGN "Keep the primary page action in the page
// header").
export function PageHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <header className="page-header">
      <div className="page-header-text">
        <h1 id="page-title" tabIndex={-1}>
          {title}
        </h1>
        {description ? <p className="page-description">{description}</p> : null}
      </div>
      {action ? <div className="page-action">{action}</div> : null}
    </header>
  );
}
