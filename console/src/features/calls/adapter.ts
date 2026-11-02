import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { PAGE_SIZE } from "../../components/Pager";
import { useSession } from "../auth/adapter";
import { placeCallError } from "./copy";
import { range, type CallFilters } from "./filters";

// The call-history feature adapter (HLD-18 A5). A read-only list plus the
// placement and hang-up actions (D-79); the call id is a row key and the
// session's own identifier, never shown. CallSummary carries no extension,
// so none is shown (gap G3).

export interface CallSnapshot {
  state: string;
  startedAt: Date | null;
  answeredAt: Date | null;
  endedAt: Date | null;
  reason: string;
  callerAnswered: boolean;
}

export interface CallRow {
  id: string;
  direction: string;
  state: string;
  from: string;
  to: string;
  startedAt: Date | null;
  answeredAt: Date | null;
  durationSeconds: number;
  terminationReason: string;
}

// callKeys is the whole call history, so a placed or hung-up call
// refreshes every filtered view.
export const callKeys = {
  all: (tenantId: string) => ["calls", tenantId] as const,
};

export function useCallHistory(filters: CallFilters, live = false) {
  const { telephony } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<CallRow[], ApiError>({
    queryKey: ["calls", tenantId, filters],
    queryFn: () =>
      callApi(async () => {
        const { from, to } = range(filters);
        const { calls } = await telephony.listCalls({
          tenantId,
          limit: PAGE_SIZE,
          offset: filters.page * PAGE_SIZE,
          direction: filters.direction,
          state: filters.state,
          startedFrom: from ? timestampFromDate(from) : undefined,
          startedTo: to ? timestampFromDate(to) : undefined,
        });
        return calls.map((c) => ({
          id: c.callId,
          direction: c.direction,
          state: c.state,
          from: c.sourceNumber,
          to: c.destNumber,
          startedAt: c.startedAt ? timestampDate(c.startedAt) : null,
          answeredAt: c.answeredAt ? timestampDate(c.answeredAt) : null,
          durationSeconds: c.durationSeconds,
          terminationReason: c.terminationReason,
        }));
      }),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
    // History follows the server only while a call is in flight (D-79);
    // otherwise it stays an on-demand read.
    refetchInterval: live ? 1000 : false,
  });
}

// useCallSnapshot is the live read behind the call session (D-79). It
// polls every second while the call is not Terminated; React Query pauses
// it while the tab is hidden.
export function useCallSnapshot(callId: string | null) {
  const { telephony } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<CallSnapshot, ApiError>({
    queryKey: ["call-snapshot", tenantId, callId],
    queryFn: async () => {
      const call = await callApi(() => telephony.getCall({ tenantId, callId: callId ?? "" }));
      const caller = call.participants.find((p) => p.role === "CALLER");
      return {
        state: call.state,
        startedAt: call.startedAt ? timestampDate(call.startedAt) : null,
        answeredAt: call.answeredAt ? timestampDate(call.answeredAt) : null,
        endedAt: call.endedAt ? timestampDate(call.endedAt) : null,
        reason: call.terminationReason,
        callerAnswered: caller?.state === "Connected",
      };
    },
    enabled: tenantId !== "" && callId !== null,
    refetchInterval: (q) => (q.state.data?.state === "Terminated" ? false : 1000),
    refetchIntervalInBackground: false,
    retry: false,
  });
}

// useRecentDestinations offers the last distinct dialled numbers to the
// dialer. It is mounted only with the dialog, so the normal pages make no
// extra read.
export function useRecentDestinations() {
  const { telephony } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<{ number: string; at: Date | null }[], ApiError>({
    queryKey: [...callKeys.all(tenantId), "recent"],
    queryFn: () =>
      callApi(async () => {
        const { calls } = await telephony.listCalls({ tenantId, limit: 50 });
        const seen = new Map<string, Date | null>();
        for (const c of calls) {
          if (c.direction === "INBOUND" || !c.destNumber) continue;
          if (seen.has(c.destNumber)) continue;
          seen.set(c.destNumber, c.startedAt ? timestampDate(c.startedAt) : null);
          if (seen.size === 5) break;
        }
        return [...seen.entries()].map(([number, at]) => ({ number, at }));
      }),
    enabled: tenantId !== "",
    staleTime: 30_000,
  });
}

// usePlaceCall places a call from one of the tenant's extensions: the
// extension rings first, and only its answer reaches the destination
// (console-click-to-call D2, D-78). Refusals arrive in console words.
export function usePlaceCall() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  const queryClient = useQueryClient();
  return useMutation<string, Error, { extensionId: string; destination: string }>({
    mutationFn: async ({ extensionId, destination }) => {
      try {
        const { callId } = await pbx.placeCall({ tenantId, extensionId, destination });
        return callId;
      } catch (err) {
        throw placeCallError(err);
      }
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: callKeys.all(tenantId) }),
  });
}

// useHangupCall ends a call in progress. Ending one that already ended
// succeeds, so a stale row is harmless.
export function useHangupCall() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  const queryClient = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (callId) =>
      callApi(async () => {
        await pbx.hangupCall({ tenantId, callId });
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: callKeys.all(tenantId) }),
  });
}
