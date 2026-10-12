import { useSession } from "../auth/adapter";

// PLATFORM_ADMIN is the installation-wide role that may apply a licence
// (ApplyLicenseKey requires system scope).
const PLATFORM_ADMIN = "PLATFORM_ADMIN";

// useCanApplyLicence decides whether to show Apply licence. Showing is
// not allowing: the server checks the scope on every call (console brief
// §6 rule 6).
export function useCanApplyLicence(): boolean {
  const session = useSession();
  const current = session.memberships.find((m) => m.tenantId === session.access?.tenantId);
  return current?.roles.includes(PLATFORM_ADMIN) ?? false;
}
