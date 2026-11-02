import { useEffect, useRef, useState } from "react";
import { formatDuration, reasonText, useCallSession } from "./session";

// CallSessionBar is the persistent call panel (D-79, design D1/D4/D10).
// It is rendered by the shell, so it survives every navigation. The
// heading changes with the phase and the primary action is always a
// single click; before the caller answers it is Cancel, not Hang up.
export function CallSessionBar() {
  const { session, phase, snapshot, reconnecting, actionError, busy, end, dismiss } = useCallSession();
  const bar = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => Date.now());
  const callId = session?.callId;

  // A new session takes focus so a keyboard user lands on the call, and
  // its text is announced through the live region below.
  useEffect(() => {
    if (callId) bar.current?.focus();
  }, [callId]);

  useEffect(() => {
    if (!session || phase !== "connected") return undefined;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [session, phase]);

  if (!session) return null;

  const from = session.from.number || "your extension";
  const to = session.destination;
  const ended = snapshot?.endedAt ?? new Date(now);
  const seconds = snapshot?.answeredAt ? Math.max(0, Math.floor((ended.getTime() - snapshot.answeredAt.getTime()) / 1000)) : 0;

  let heading = "Starting call";
  let detail = "Asking the platform to place it.";
  let action: "cancel" | "hangup" | "dismiss" = "cancel";
  let actionLabel = "Cancel";

  if (phase === "ringing-caller") {
    heading = `Ringing your extension ${from}`;
    detail = `Answer your phone to continue. ${to} is not dialled yet.`;
    actionLabel = "Cancel call";
  } else if (phase === "calling-destination") {
    heading = `Calling ${to}…`;
    detail = `Connected to ${from}. Waiting for ${to} to answer.`;
    action = "hangup";
    actionLabel = "Hang up";
  } else if (phase === "connected") {
    heading = `In call with ${to}`;
    detail = `${formatDuration(seconds)} · from extension ${from}`;
    action = "hangup";
    actionLabel = "Hang up";
  } else if (phase === "ending") {
    heading = "Ending call…";
    detail = "Releasing every leg.";
    action = "hangup";
    actionLabel = "Hang up";
  } else if (phase === "ended") {
    const reason = snapshot?.reason ?? "";
    if (reason === "caller_no_answer") {
      heading = `Call ended — ${from} did not answer`;
      detail = `${to} was never dialled.`;
    } else if (reason === "destination unreachable") {
      heading = `Call ended — ${to} could not be reached`;
      detail = `${from} was released.`;
    } else {
      heading = `Call ended — ${reasonText(reason)}`;
      detail = snapshot?.answeredAt ? `${formatDuration(seconds)} · from extension ${from}` : `${from} to ${to}.`;
    }
    action = "dismiss";
    actionLabel = "Dismiss";
  }

  const tone = phase === "connected" ? "connected" : phase === "ended" ? "ended" : "active";

  return (
    <div
      id="call-session-bar"
      ref={bar}
      tabIndex={-1}
      className={`call-bar call-bar-${tone}`}
      role="region"
      aria-label="Current call"
      data-phase={phase}
    >
      <div className="call-bar-icon" aria-hidden="true">
        {phase === "connected" ? "✓" : phase === "ended" ? "!" : "☎"}
      </div>
      <div className="call-bar-text" aria-live="polite">
        <strong>{heading}</strong>
        <span>{detail}</span>
        {reconnecting ? <span className="call-bar-reconnect">Reconnecting… the last known state is shown.</span> : null}
        {actionError ? (
          <span className="call-bar-error" role="alert">
            {actionError}
          </span>
        ) : null}
      </div>
      {action === "dismiss" ? (
        <button type="button" className="button" onClick={dismiss}>
          {actionLabel}
        </button>
      ) : (
        <button
          type="button"
          className={`button ${action === "cancel" ? "button-danger-outline" : "button-danger"}`}
          onClick={() => void end()}
          disabled={busy}
          aria-busy={busy || undefined}
          aria-label={action === "cancel" ? `Cancel call to ${to}` : `Hang up call with ${to}`}
        >
          {actionLabel}
        </button>
      )}
    </div>
  );
}
