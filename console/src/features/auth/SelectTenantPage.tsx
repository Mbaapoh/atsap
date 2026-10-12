import { useState } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router";
import { ApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { useSession, useSessionController } from "./adapter";
import { AuthLayout } from "./AuthLayout";
import { roleLabel } from "./copy";

// SelectTenantPage chooses the Tenant to act in: after signing in with
// several memberships, and whenever the person switches. The server
// issues a token for the chosen Tenant and stays the judge of every
// request made with it (console brief §6 rule 5).
export function SelectTenantPage() {
  const session = useSession();
  const controller = useSessionController();
  const navigate = useNavigate();
  const location = useLocation();
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  if (session.status === "restoring") {
    return (
      <AuthLayout title="Choose a tenant">
        <p role="status">Checking for an existing session…</p>
      </AuthLayout>
    );
  }
  if (session.status === "signed-out" || session.status === "no-access") return <Navigate to="/sign-in" replace />;

  const current = session.access?.tenantId;
  const from = (location.state as { from?: string } | null)?.from;

  async function choose(tenantId: string) {
    setFailure(null);
    setBusy(tenantId);
    try {
      await controller.chooseTenant(tenantId);
      // A switch lands on the overview: the page left behind belonged to
      // the other Tenant.
      await navigate(tenantId === current && from ? from : "/", { replace: true });
    } catch (err) {
      setFailure(err instanceof ApiError && err.kind === "forbidden" ? "You can no longer act in that tenant." : (err as Error).message);
      setBusy(null);
    }
  }

  return (
    <AuthLayout title="Choose a tenant">
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      <p>You are a member of more than one tenant. Choose where to work; you can switch later.</p>
      <ul className="choice-list">
        {session.memberships.map((m) => (
          <li key={m.tenantId}>
            <button type="button" className="button" disabled={busy !== null} aria-current={m.tenantId === current ? "true" : undefined} onClick={() => void choose(m.tenantId)}>
              <span>{m.tenantName}</span>
              <span className="choice-detail">
                {m.tenantId === current ? "Current · " : ""}
                {m.roles.map(roleLabel).join(", ")}
              </span>
            </button>
          </li>
        ))}
      </ul>
      {session.status === "signed-in" ? (
        <p className="auth-footer">
          <Link to="/">Stay in {session.memberships.find((m) => m.tenantId === current)?.tenantName ?? "the current tenant"}</Link>
        </p>
      ) : null}
    </AuthLayout>
  );
}
