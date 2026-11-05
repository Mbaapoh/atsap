import { create as createMessage } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import {
  CreateFlowRequestSchema,
  DeleteFlowRequestSchema,
  FlowConfigSchema,
  PublishFlowRequestSchema,
  RollbackFlowRequestSchema,
} from "../../api/gen/atsapbx/v1/pbx_pb";
import { useSession } from "../auth/adapter";

export interface FlowConfig {
  openExtensionId: string;
  closedExtensionId: string;
  businessStartMinute: number;
  businessEndMinute: number;
  prompt: string;
}

export interface Flow {
  id: string;
  number: string;
  name: string;
  activeVersion: number;
  config: FlowConfig;
}

export interface FlowVersion {
  version: number;
  config: FlowConfig;
  publishedAt: Date | null;
  publishedBy: string;
}

export interface Holiday { day: string; name: string }

function configOf(c: { openExtensionId: string; closedExtensionId: string; businessStartMinute: number; businessEndMinute: number; prompt: string } | undefined): FlowConfig {
  return {
    openExtensionId: c?.openExtensionId ?? "",
    closedExtensionId: c?.closedExtensionId ?? "",
    businessStartMinute: c?.businessStartMinute ?? 480,
    businessEndMinute: c?.businessEndMinute ?? 1080,
    prompt: c?.prompt ?? "",
  };
}

export function useFlows() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<Flow[], ApiError>({
    queryKey: ["flows", tenantId],
    queryFn: () =>
      callApi(async () => {
        const { flows } = await pbx.listFlows({ tenantId });
        return flows.map((f) => ({
          id: f.flowId, number: f.number, name: f.name, activeVersion: f.activeVersion, config: configOf(f.config ? {
            openExtensionId: f.config.openExtensionId, closedExtensionId: f.config.closedExtensionId,
            businessStartMinute: f.config.businessStartMinute, businessEndMinute: f.config.businessEndMinute, prompt: f.config.prompt,
          } : undefined),
        }));
      }),
    enabled: tenantId !== "",
  });
}

export interface FlowInput { number: string; name: string; config: FlowConfig }

export function useFlowChanges() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: ["flows", tenantId] });
  const create = useMutation<void, ApiError, FlowInput>({
    mutationFn: (input) =>
      callApi(async () => {
        await pbx.createFlow(createMessage(CreateFlowRequestSchema, {
          tenantId, number: input.number, name: input.name,
          config: createMessage(FlowConfigSchema, configWire(input.config)),
        }));
      }),
    onSuccess: refetch,
  });
  const publish = useMutation<void, ApiError, { id: string; config: FlowConfig }>({
    mutationFn: ({ id, config }) =>
      callApi(async () => {
        await pbx.publishFlow(createMessage(PublishFlowRequestSchema, {
          tenantId, flowId: id, config: createMessage(FlowConfigSchema, configWire(config)),
        }));
      }),
    onSuccess: refetch,
  });
  const rollback = useMutation<void, ApiError, { id: string; version: number }>({
    mutationFn: ({ id, version }) => callApi(async () => {
      await pbx.rollbackFlow(createMessage(RollbackFlowRequestSchema, { tenantId, flowId: id, version }));
    }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (id) => callApi(async () => {
      await pbx.deleteFlow(createMessage(DeleteFlowRequestSchema, { tenantId, flowId: id }));
    }),
    onSuccess: refetch,
  });
  return { create: create.mutateAsync, publish: publish.mutateAsync, rollback: rollback.mutateAsync, remove: remove.mutateAsync };
}

function configWire(c: FlowConfig) {
  return {
    openExtensionId: c.openExtensionId,
    closedExtensionId: c.closedExtensionId,
    businessStartMinute: c.businessStartMinute,
    businessEndMinute: c.businessEndMinute,
    prompt: c.prompt,
  };
}

export function useFlowVersions(id: string | null) {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<FlowVersion[], ApiError>({
    queryKey: ["flow-versions", tenantId, id],
    queryFn: () =>
      callApi(async () => {
        const { versions } = await pbx.listFlowVersions({ tenantId, flowId: id ?? "" });
        return versions.map((v) => ({
          version: v.version,
          config: configOf(v.config ? {
            openExtensionId: v.config.openExtensionId, closedExtensionId: v.config.closedExtensionId,
            businessStartMinute: v.config.businessStartMinute, businessEndMinute: v.config.businessEndMinute, prompt: v.config.prompt,
          } : undefined),
          publishedAt: v.publishedAt ? timestampDate(v.publishedAt) : null,
          publishedBy: v.publishedBy,
        }));
      }),
    enabled: tenantId !== "" && id !== null,
    placeholderData: keepPreviousData,
  });
}

export function useHolidays() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  return useQuery<Holiday[], ApiError>({
    queryKey: ["holidays", tenantId],
    queryFn: () => callApi(async () => (await pbx.listHolidays({ tenantId })).holidays.map((h) => ({ day: h.day, name: h.name }))),
    enabled: tenantId !== "",
  });
}

export function useHolidayChanges() {
  const { pbx } = useApiClients();
  const tenantId = useSession().access?.tenantId ?? "";
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: ["holidays", tenantId] });
  const add = useMutation<void, ApiError, Holiday>({
    mutationFn: (h) => callApi(async () => { await pbx.addHoliday({ tenantId, day: h.day, name: h.name }); }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (day) => callApi(async () => { await pbx.deleteHoliday({ tenantId, day }); }),
    onSuccess: refetch,
  });
  return { add: add.mutateAsync, remove: remove.mutateAsync };
}
