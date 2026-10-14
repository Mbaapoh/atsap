import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useSession } from "./adapter";

// RequireSession lets a signed-in person with a chosen Tenant through and
// sends everyone else to the step they are missing. It decides what to
// show, never what is allowed: the server checks every request (console
// brief §6 rule 6).
export function RequireSession({ children }: { children: ReactNode }) {
  const session = useSession();
  const location = useLocation();
  const from = location.pathname + location.search;

  switch (session.status) {
    case "restoring":
      return (
        <main className="auth-layout">
          <p role="status">Loading the console…</p>
        </main>
      );
    case "signed-out":
    case "no-access":
      return <Navigate to="/sign-in" replace state={{ from }} />;
    case "choosing-tenant":
      return <Navigate to="/select-tenant" replace state={{ from }} />;
    case "signed-in":
      // A must-change password reaches only the account page, where it
      // can be changed or the person can sign out (D-64).
      if (session.passwordChangeRequired && location.pathname !== "/account") {
        return <Navigate to="/account" replace />;
      }
      return <>{children}</>;
    default:
      // Unreachable: every session state is handled above, which the
      // switch-exhaustiveness lint rule keeps true as states are added.
      return null;
  }
}
