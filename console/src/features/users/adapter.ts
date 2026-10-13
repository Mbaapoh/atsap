import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { PrincipalStatus, type ListedPrincipal } from "../../api/gen/atsapbx/v1/identity_pb";
import { PAGE_SIZE } from "../../components/Pager";
import { useSession } from "../auth/adapter";

// The users feature adapter (HLD-18 A2): the tenant's memberships and their
// roles, read and changed through IdentityService for the session's Tenant
// only. A "user" on this screen is a membership of this tenant: the
// person's other tenants are not shown (D-61).

export type MembershipStatus = "active" | "disabled" | "unknown";

export interface RoleGrant {
  role: string;
  scope: string;
}

export interface UserRow {
  principalId: string;
  name: string;
  email: string;
  status: MembershipStatus;
  roles: RoleGrant[];
}

function statusOf(s: PrincipalStatus): MembershipStatus {
  if (s === PrincipalStatus.ACTIVE) return "active";
  if (s === PrincipalStatus.DISABLED) return "disabled";
  return "unknown";
}

export function toUserRow(p: ListedPrincipal): UserRow {
  const principal = p.principal;
  return {
    principalId: principal?.principalId ?? "",
    name: principal?.displayName ?? "",
    email: principal?.email ?? "",
    status: statusOf(principal?.status ?? PrincipalStatus.UNSPECIFIED),
    roles: p.roleBindings.map((b) => ({ role: b.role, scope: b.scope || "tenant" })),
  };
}

export const userKeys = {
  all: (tenantId: string) => ["users", tenantId] as const,
  page: (tenantId: string, page: number) => [...userKeys.all(tenantId), "page", page] as const,
};

export function useTenantId(): string {
  return useSession().access?.tenantId ?? "";
}

// useUsers reads one page of the tenant's memberships.
export function useUsers(page: number) {
  const { identity } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<UserRow[], ApiError>({
    queryKey: userKeys.page(tenantId, page),
    queryFn: () =>
      callApi(async () => (await identity.listPrincipals({ tenantId, limit: PAGE_SIZE, offset: page * PAGE_SIZE })).principals.map(toUserRow)),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
  });
}
