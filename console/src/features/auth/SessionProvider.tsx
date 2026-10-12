import { useEffect, type ReactNode } from "react";
import { SessionContext, type SessionController } from "./adapter";

// SessionProvider makes the session available and, on load, tries the
// refresh cookie once to restore it.
export function SessionProvider({ controller, children }: { controller: SessionController; children: ReactNode }) {
  useEffect(() => {
    void controller.restore();
  }, [controller]);
  return <SessionContext.Provider value={controller}>{children}</SessionContext.Provider>;
}
