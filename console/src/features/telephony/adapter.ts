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
  routes: (tenantId: string, page: number) => [...telephonyKeys.all(tenantId), "routes", page] as const,
  inbound: (tenantId: string) => [...telephonyKeys.all(tenantId), "inbound"] as const,
  emergency: (tenantId: string) => [...telephonyKeys.all(tenantId), "emergency"] as const,
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

export interface RouteRow {
  id: string;
  prefix: string;
  trunkId: string;
  costMicros: bigint;
  priority: number;
}

export interface RouteInput {
  prefix: string;
  trunkId: string;
  costMicros: bigint;
  priority: number;
}

export function useRoutes(page: number) {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<RouteRow[], ApiError>({
    queryKey: telephonyKeys.routes(tenantId, page),
    queryFn: () =>
      callApi(async () =>
        (await pbx.listRoutes({ tenantId, limit: PAGE_SIZE, offset: page * PAGE_SIZE })).routes.map((r) => ({
          id: r.routeId,
          prefix: r.prefixPattern,
          trunkId: r.trunkId,
          costMicros: r.costPerMinuteMicros,
          priority: r.priority,
        })),
      ),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
  });
}

// useRouteChanges creates and deletes routes. There is no update in the
// API, so a change is a replace: create the new route, then delete the
// old one — in that order, so calls are never left without a route.
export function useRouteChanges() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: telephonyKeys.all(tenantId) });
  const create = useMutation<void, ApiError, RouteInput>({
    mutationFn: (r) =>
      callApi(async () => {
        await pbx.createRoute({ tenantId, prefixPattern: r.prefix, trunkId: r.trunkId, costPerMinuteMicros: r.costMicros, priority: r.priority });
      }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        await pbx.deleteRoute({ tenantId, routeId: id });
      }),
    onSuccess: refetch,
  });
  return { create: create.mutateAsync, remove: remove.mutateAsync };
}

export interface InboundRow {
  id: string;
  number: string;
  extensionId: string;
}

// useInboundNumbers lists the tenant's DIDs. The API does not page them.
// Deleting an extension removes its numbers on the server, so this is
// re-read whenever the page is opened.
export function useInboundNumbers() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<InboundRow[], ApiError>({
    queryKey: telephonyKeys.inbound(tenantId),
    queryFn: () =>
      callApi(async () =>
        (await pbx.listInboundNumbers({ tenantId })).numbers.map((n) => ({ id: n.inboundNumberId, number: n.number, extensionId: n.extensionId })),
      ),
    enabled: tenantId !== "",
  });
}

export function useInboundChanges() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: telephonyKeys.inbound(tenantId) });
  const create = useMutation<void, ApiError, { number: string; extensionId: string }>({
    mutationFn: (n) =>
      callApi(async () => {
        await pbx.createInboundNumber({ tenantId, number: n.number, extensionId: n.extensionId });
      }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        await pbx.deleteInboundNumber({ tenantId, inboundNumberId: id });
      }),
    onSuccess: refetch,
  });
  return { create: create.mutateAsync, remove: remove.mutateAsync };
}

export interface EmergencyProfile {
  // country is "" when none was ever set.
  country: string;
  // inCatalogue is false when no country is set or it names none the
  // platform knows: the tenant must enter its own numbers.
  inCatalogue: boolean;
  numbers: { number: string; builtIn: boolean }[];
}

export function useEmergencyProfile() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<EmergencyProfile, ApiError>({
    queryKey: telephonyKeys.emergency(tenantId),
    queryFn: () =>
      callApi(async () => {
        const p = await pbx.getEmergencyProfile({ tenantId });
        return {
          country: p.countrySet ? p.countryCode : "",
          inCatalogue: p.inCatalogue,
          numbers: p.numbers.map((n) => ({ number: n.number, builtIn: n.builtIn })),
        };
      }),
    enabled: tenantId !== "",
  });
}

export function useEmergencyChanges() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: telephonyKeys.emergency(tenantId) });
  const setCountry = useMutation<void, ApiError, string>({
    mutationFn: (countryCode) =>
      callApi(async () => {
        await pbx.setEmergencyProfile({ tenantId, countryCode });
      }),
    onSuccess: refetch,
  });
  const add = useMutation<void, ApiError, string>({
    mutationFn: (number) =>
      callApi(async () => {
        await pbx.createEmergencyNumber({ tenantId, number });
      }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (number) =>
      callApi(async () => {
        await pbx.deleteEmergencyNumber({ tenantId, number });
      }),
    onSuccess: refetch,
  });
  return { setCountry: setCountry.mutateAsync, add: add.mutateAsync, remove: remove.mutateAsync };
}
