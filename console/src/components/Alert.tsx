import { useEffect, useRef, type ReactNode } from "react";
import { ToneIcon, type Tone } from "./icons";

interface AlertProps {
  tone: Exclude<Tone, "neutral">;
  title: string;
  children?: ReactNode;
  // focusOnMount moves focus to the alert when it appears: an error
  // summary after a failed request is where keyboard and screen-reader
  // users must land (HLD-19 §7).
  focusOnMount?: boolean;
}

// Alert is a page-level message. Danger and warning interrupt (role
// "alert"); success and info wait their turn (role "status"). Its text is
// always written by the console, never a raw server or engine error.
export function Alert({ tone, title, children, focusOnMount = false }: AlertProps) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focusOnMount) ref.current?.focus();
  }, [focusOnMount]);
  const urgent = tone === "danger" || tone === "warning";
  return (
    <div ref={ref} className={`alert alert-${tone}`} role={urgent ? "alert" : "status"} tabIndex={focusOnMount ? -1 : undefined}>
      <ToneIcon tone={tone} />
      <div className="alert-body">
        <p className="alert-title">{title}</p>
        {children ? <div className="alert-text">{children}</div> : null}
      </div>
    </div>
  );
}
