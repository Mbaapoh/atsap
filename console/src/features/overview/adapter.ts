import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { TrunkHealth, type TrunkHealthReason } from "../../api/gen/atsapbx/v1/pbx_pb";
import { useSession } from "../auth/adapter";
import { healthRefreshMs } from "../telephony/adapter";

// The overview feature adapter: the setup checklist's facts and the
// compact recent-call and trunk-health summaries (CONSOLE-DESIGN
// "Overview"). Every value comes from a public API read for the current
// Tenant; nothing is estimated, and a fact that cannot be read is reported
// as unknown, never as done or not done.

// TERMINATED is the Call lifecycle's final state as the server names it
// (PRD §11.1); the overview filters on it and shows states verbatim.
const TERMINATED = "Terminated";

// RECENT_CALLS is how many calls the overview lists; COMPLETED_WINDOW is
// how many recent ended calls it searches for one that was answered.
export const RECENT_CALLS = 5;
export const COMPLETED_WINDOW = 50;

export interface RecentCall {
  id: string;
  direction: string;
  state: string;
  from: string;
  to: string;
  startedAt: Date | null;
  durationSeconds: number;
}

export interface TrunkHealthRow {
  id: string;
  name: string;
  health: TrunkHealth;
  healthReason: TrunkHealthReason;
  healthObservedAt: Date | null;
}

const overviewKeys = {
  all: (tenantId: string) => ["overview", tenantId] as const,
  fact: (tenantId: string, name: string) => [...overviewKeys.all(tenantId), name] as const,
};

function useTenantId(): string {
  return useSession().access?.tenantId ?? "";
}

// useUserCount reads how many people (up to two) are members of the
// Tenant: the checklist needs to know only whether anyone besides the
// first administrator has been added.
export function useUserCount() {
  const { identity } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<number, ApiError>({
    queryKey: overviewKeys.fact(tenantId, "users"),
    queryFn: () => callApi(async () => (await identity.listPrincipals({ tenantId, limit: 2 })).principals.length),
    enabled: tenantId !== "",
  });
}

export function useHasExtension() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<boolean, ApiError>({
    queryKey: overviewKeys.fact(tenantId, "extensions"),
    queryFn: () => callApi(async () => (await pbx.listExtensions({ tenantId, limit: 1 })).extensions.length > 0),
    enabled: tenantId !== "",
  });
}

// useTrunks returns the Tenant's trunks with the health the platform has
// observed, for both the checklist and the health summary.
export function useTrunks() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<TrunkHealthRow[], ApiError>({
    queryKey: overviewKeys.fact(tenantId, "trunks"),
    queryFn: () =>
      callApi(async () =>
        (await pbx.listTrunks({ tenantId, limit: 50 })).trunks.map((t) => ({
          id: t.trunkId,
          name: t.name,
          health: t.health,
          healthReason: t.healthReason,
          healthObservedAt: t.healthObservedAt ? timestampDate(t.healthObservedAt) : null,
        })),
      ),
    enabled: tenantId !== "",
    refetchInterval: (query) => healthRefreshMs(query.state.data),
  });
}

export function useHasRoute() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<boolean, ApiError>({
    queryKey: overviewKeys.fact(tenantId, "routes"),
    queryFn: () => callApi(async () => (await pbx.listRoutes({ tenantId, limit: 1 })).routes.length > 0),
    enabled: tenantId !== "",
  });
}

// useHasCompletedCall reports whether any of the most recent ended calls
// was answered before it ended: a call that actually connected.
export function useHasCompletedCall() {
  const { telephony } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<boolean, ApiError>({
    queryKey: overviewKeys.fact(tenantId, "completed-call"),
    queryFn: () =>
      callApi(async () =>
        (await telephony.listCalls({ tenantId, state: TERMINATED, limit: COMPLETED_WINDOW })).calls.some((c) => c.answeredAt !== undefined),
      ),
    enabled: tenantId !== "",
  });
}

export function useRecentCalls() {
  const { telephony } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<RecentCall[], ApiError>({
    queryKey: overviewKeys.fact(tenantId, "recent-calls"),
    queryFn: () =>
      callApi(async () =>
        (await telephony.listCalls({ tenantId, limit: RECENT_CALLS })).calls.map((c) => ({
          id: c.callId,
          direction: c.direction,
          state: c.state,
          from: c.sourceNumber,
          to: c.destNumber,
          startedAt: c.startedAt ? timestampDate(c.startedAt) : null,
          durationSeconds: c.durationSeconds,
        })),
      ),
    enabled: tenantId !== "",
  });
}
