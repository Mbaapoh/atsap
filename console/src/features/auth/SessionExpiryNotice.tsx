import { useEffect, useState } from "react";
import { Alert } from "../../components/Alert";
import { useSession, useSessionController } from "./adapter";

// WARN_BEFORE_MS is how long before the session's end the shell warns.
export const WARN_BEFORE_MS = 5 * 60_000;
const TICK_MS = 15_000;

function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(id);
  }, []);
  return now;
}

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

// SessionExpiryNotice tells the person, before it happens, that their
// session is about to end, and offers to keep it. The server decides: a
// renewal that cannot extend the session (its absolute limit) leaves the
// warning in place, and once the time has passed the console asks the
// server rather than assuming — a refused renewal is the session ending,
// and the person is returned to sign-in with that explanation.
export function SessionExpiryNotice() {
  const session = useSession();
  const controller = useSessionController();
  const now = useNow();
  const [busy, setBusy] = useState(false);
  const endsAt = session.sessionExpiresAt?.getTime();
  const passed = endsAt !== undefined && now >= endsAt;

  useEffect(() => {
    if (passed) void controller.refresh();
  }, [passed, controller]);

  if (endsAt === undefined || passed || endsAt - now > WARN_BEFORE_MS) return null;
  return (
    <Alert tone="warning" title={`Your session ends at ${timeFormat.format(endsAt)}`}>
      <p>Save your work. After that you will need to sign in again.</p>
      <p>
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={() => {
            setBusy(true);
            void controller.refresh().finally(() => setBusy(false));
          }}
        >
          Stay signed in
        </button>
      </p>
    </Alert>
  );
}
