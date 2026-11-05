import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { useSession } from "../auth/adapter";

export interface ReportFilters {
  from: string;
  to: string;
  direction: string;
  extensionId: string;
  principalId: string;
  number: string;
  page: number;
}

export const blankReportFilters: ReportFilters = {
  from: "", to: "", direction: "", extensionId: "", principalId: "", number: "", page: 0,
};

export interface ReportParticipant {
  id: string;
  role: string;
  extensionId: string;
  principalId: string;
  billableSeconds: number;
  usageSeconds: number;
}

export interface CallRecord {
  id: string;
  direction: string;
  from: string;
  to: string;
  startedAt: Date | null;
  answeredAt: Date | null;
  endedAt: Date | null;
  durationSeconds: number;
  reason: string;
  participants: ReportParticipant[];
}

export const PAGE_SIZE = 50;

function wireFilter(f: ReportFilters) {
  return {
    from: f.from ? timestampFromDate(new Date(`${f.from}T00:00:00Z`)) : undefined,
    to: f.to ? timestampFromDate(new Date(`${f.to}T23:59:59Z`)) : undefined,
    direction: f.direction,
    extensionId: f.extensionId,
    principalId: f.principalId,
    number: f.number,
  };
}

export function useCallRecords(filters: ReportFilters) {
  const { reporting } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<CallRecord[], ApiError>({
    queryKey: ["reports", tenantId, filters],
    queryFn: () =>
      callApi(async () => {
        const { records } = await reporting.listCallRecords({
          tenantId,
          filter: wireFilter(filters),
          limit: PAGE_SIZE,
          offset: filters.page * PAGE_SIZE,
        });
        return records.map((r) => ({
          id: r.callId,
          direction: r.direction,
          from: r.sourceNumber,
          to: r.destNumber,
          startedAt: r.startedAt ? new Date(Number(r.startedAt.seconds) * 1000) : null,
          answeredAt: r.answeredAt ? new Date(Number(r.answeredAt.seconds) * 1000) : null,
          endedAt: r.endedAt ? new Date(Number(r.endedAt.seconds) * 1000) : null,
          durationSeconds: r.durationSeconds,
          reason: r.terminationReason,
          participants: r.participants.map((p) => ({
            id: p.participantId, role: p.role, extensionId: p.extensionId,
            principalId: p.principalId, billableSeconds: p.billableSeconds, usageSeconds: p.usageSeconds,
          })),
        }));
      }),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
  });
}

export function useExportCallRecords() {
  const { reporting } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useMutation<Blob, ApiError, ReportFilters>({
    mutationFn: (filters) =>
      callApi(async () => {
        const { csv } = await reporting.exportCallRecords({ tenantId, filter: wireFilter(filters) });
        return new Blob([new TextDecoder().decode(csv)], { type: "text/csv;charset=utf-8" });
      }),
  });
}
