import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { PAGE_SIZE } from "../../components/Pager";
import { useSession } from "../auth/adapter";
import { range, type CallFilters } from "./filters";

// The call-history feature adapter (HLD-18 A5). A read-only view of
// ListCalls; the call id is kept only as a row key. CallSummary carries
// no extension, so none is shown (gap G3).

export interface CallRow {
  id: string;
  direction: string;
  state: string;
  from: string;
  to: string;
  startedAt: Date | null;
  durationSeconds: number;
  terminationReason: string;
}

export function useCallHistory(filters: CallFilters) {
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
          durationSeconds: c.durationSeconds,
          terminationReason: c.terminationReason,
        }));
      }),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
  });
}
