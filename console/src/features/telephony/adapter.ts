import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import type { TrunkHealth } from "../../api/gen/atsapbx/v1/pbx_pb";
import { PAGE_SIZE } from "../../components/Pager";
import { useSession } from "../auth/adapter";

// The telephony feature adapter (HLD-18 A4): trunks, outbound routes,
// inbound numbers and the emergency profile, through PbxService for the
// session's Tenant. A trunk's secret is write-only: it is sent, never read
// back (the API returns only whether one is stored).

export interface TrunkRow {
  id: string;
  name: string;
  host: string;
  port: number;
  authUsername: string;
  hasSecret: boolean;
  channelLimit: number;
  priority: number;
  health: TrunkHealth;
}

export interface TrunkInput {
  name: string;
  host: string;
  port: number;
  authUsername: string;
  // secret: on create, the carrier's secret if any; on edit, empty keeps
  // the stored one and non-empty replaces it.
  secret: string;
  channelLimit: number;
  priority: number;
}

export function useTenantId(): string {
  return useSession().access?.tenantId ?? "";
}

export const telephonyKeys = {
  all: (tenantId: string) => ["telephony", tenantId] as const,
  trunks: (tenantId: string, page: number) => [...telephonyKeys.all(tenantId), "trunks", page] as const,
};

export function useTrunks(page: number) {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<TrunkRow[], ApiError>({
    queryKey: telephonyKeys.trunks(tenantId, page),
    queryFn: () =>
      callApi(async () =>
        (await pbx.listTrunks({ tenantId, limit: PAGE_SIZE, offset: page * PAGE_SIZE })).trunks.map((t) => ({
          id: t.trunkId,
          name: t.name,
          host: t.host,
          port: t.port,
          authUsername: t.authUsername,
          hasSecret: t.hasSecret,
          channelLimit: t.channelLimit,
          priority: t.priority,
          health: t.health,
        })),
      ),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
  });
}

// useTrunkChanges creates, edits and deletes trunks. The trunk secret is
// sent and never kept: the mutations hold no cache (gcTime 0), and every
// change refetches everything telephony, since deleting a trunk also
// deletes its routes.
export function useTrunkChanges() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: telephonyKeys.all(tenantId) });
  const create = useMutation<void, ApiError, TrunkInput>({
    mutationFn: (t) =>
      callApi(async () => {
        await pbx.createTrunk({ tenantId, ...t });
      }),
    gcTime: 0,
    onSuccess: refetch,
  });
  const update = useMutation<void, ApiError, { id: string; input: TrunkInput }>({
    mutationFn: ({ id, input }) =>
      callApi(async () => {
        await pbx.updateTrunk({ tenantId, trunkId: id, ...input });
      }),
    gcTime: 0,
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        await pbx.deleteTrunk({ tenantId, trunkId: id });
      }),
    onSuccess: refetch,
  });
  const forget = <T,>(fn: (v: T) => Promise<void>, reset: () => void) => async (v: T) => {
    try {
      await fn(v);
    } finally {
      reset();
    }
  };
  return {
    create: forget(create.mutateAsync, create.reset),
    update: forget(update.mutateAsync, update.reset),
    remove: remove.mutateAsync,
  };
}
