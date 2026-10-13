import { useSession } from "./adapter";

// The Phase A role catalogue (console-admin-screens design D1). There is
// no role-listing RPC: these are the roles identity's DefaultPermissions
// defines. A role the console does not know is shown by its server name,
// never hidden or renamed.

export interface RoleInfo {
  role: string;
  label: string;
  // scope is where a grant of the role applies: "tenant", or "system"
  // for the installation-wide platform administrator.
  scope: "tenant" | "system";
  description: string;
}

export const PLATFORM_ADMIN = "PLATFORM_ADMIN";
export const TENANT_ADMIN = "TENANT_ADMIN";

export const ROLE_CATALOGUE: readonly RoleInfo[] = [
  { role: "AGENT", label: "Agent", scope: "tenant", description: "Reads this tenant's calls." },
  { role: "SUPERVISOR", label: "Supervisor", scope: "tenant", description: "Reads calls and people, and can end calls." },
  { role: TENANT_ADMIN, label: "Tenant administrator", scope: "tenant", description: "Administers everything in this tenant." },
  { role: PLATFORM_ADMIN, label: "Platform administrator", scope: "system", description: "Administers the whole installation, including its licence." },
];

// roleLabel names a role in words; an unknown role shows as given.
export function roleLabel(role: string): string {
  return ROLE_CATALOGUE.find((r) => r.role === role)?.label ?? role;
}

export interface Permissions {
  // roles are the current membership's roles, as the session holds them.
  roles: string[];
  // canAdminister: show the controls that change this tenant's
  // configuration and people.
  canAdminister: boolean;
  // isPlatformAdmin: show installation-wide controls (licence) and offer
  // the platform-administrator role.
  isPlatformAdmin: boolean;
  // grantableRoles are the roles this person may be offered to grant.
  grantableRoles: readonly RoleInfo[];
}

// permissionsFor decides what to show for a set of roles. Showing is not
// allowing: the server checks every request (console brief §6 rule 6).
export function permissionsFor(roles: readonly string[]): Permissions {
  const isPlatformAdmin = roles.includes(PLATFORM_ADMIN);
  const canAdminister = isPlatformAdmin || roles.includes(TENANT_ADMIN);
  return {
    roles: [...roles],
    canAdminister,
    isPlatformAdmin,
    grantableRoles: canAdminister ? ROLE_CATALOGUE.filter((r) => r.scope === "tenant" || isPlatformAdmin) : [],
  };
}

// usePermissions reads the session's current membership; it makes no
// API call.
export function usePermissions(): Permissions {
  const session = useSession();
  const current = session.memberships.find((m) => m.tenantId === session.access?.tenantId);
  return permissionsFor(current?.roles ?? []);
}
