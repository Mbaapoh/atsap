import { Link } from "react-router";
import { useSession } from "./adapter";

// TenantIndicator names the Tenant every action applies to, in the top
// bar, with a way to switch when the person has more than one.
export function TenantIndicator() {
  const session = useSession();
  const current = session.memberships.find((m) => m.tenantId === session.access?.tenantId);
  if (!current) return null;
  return (
    <span className="tenant-indicator">
      <span className="visually-hidden">Tenant: </span>
      <strong>{current.tenantName}</strong>
      {session.memberships.length > 1 ? (
        <>
          {" "}
          {/* The visible word starts the spoken name (WCAG 2.5.3). */}
          <Link to="/select-tenant" aria-label="Switch tenant">
            Switch
          </Link>
        </>
      ) : null}
    </span>
  );
}
