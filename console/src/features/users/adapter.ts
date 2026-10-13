import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
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

export interface NewUser {
  email: string;
  displayName: string;
  role: string;
  // initialPassword is optional: without it a new person is invited by
  // email; it is never applied to a person who already has an account.
  initialPassword: string;
}

// useAddUser adds a membership. The initial password goes to the server
// and nowhere else: the mutation is reset as soon as it settles and never
// cached (gcTime 0), as with the licence key.
export function useAddUser() {
  const { identity } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const mutation = useMutation<void, ApiError, NewUser>({
    mutationFn: (u) =>
      callApi(async () => {
        await identity.provisionPrincipal({
          tenantId,
          email: u.email,
          displayName: u.displayName,
          role: u.role,
          password: u.initialPassword,
        });
      }),
    gcTime: 0,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: userKeys.all(tenantId) }),
  });
  const { mutateAsync, reset } = mutation;
  const add = useCallback(
    async (u: NewUser) => {
      try {
        await mutateAsync(u);
      } finally {
        reset();
      }
    },
    [mutateAsync, reset],
  );
  return { add };
}

// useRoleChanges grants and revokes roles on a membership. Every change is
// confirmed in the UI first and the list is refetched afterwards; nothing
// is applied optimistically (console brief: never optimistically revoke).
export function useRoleChanges() {
  const { identity } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: userKeys.all(tenantId) });
  const grant = useMutation<void, ApiError, { principalId: string; grant: RoleGrant }>({
    mutationFn: ({ principalId, grant: g }) =>
      callApi(async () => {
        await identity.grantRole({ tenantId, principalId, role: g.role, scope: g.scope });
      }),
    onSuccess: refetch,
  });
  const revoke = useMutation<void, ApiError, { principalId: string; grant: RoleGrant }>({
    mutationFn: ({ principalId, grant: g }) =>
      callApi(async () => {
        await identity.revokeRole({ tenantId, principalId, role: g.role, scope: g.scope });
      }),
    onSuccess: refetch,
  });
  return { grant: grant.mutateAsync, revoke: revoke.mutateAsync };
}
