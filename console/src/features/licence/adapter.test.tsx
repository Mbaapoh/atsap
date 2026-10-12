import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { ApiProvider } from "../../api/ApiProvider";
import { createApiClients } from "../../api/clients";
import { ApiError } from "../../api/errors";
import { CapacityLevel, LicenseCondition, LicenseTier } from "../../api/gen/atsapbx/v1/licensing_pb";
import { createApiTransport } from "../../api/transport";
import { server } from "../../test/msw";
import { toLicenceSummary, useLicenceStatus } from "./adapter";

const base = "http://localhost:3000";
const statusPath = `${base}/atsapbx.v1.LicensingService/GetLicenseStatus`;

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const clients = createApiClients(createApiTransport({ baseUrl: base }));
  return (
    <QueryClientProvider client={queryClient}>
      <ApiProvider clients={clients}>{children}</ApiProvider>
    </QueryClientProvider>
  );
}

describe("licence adapter", () => {
  it("returns the server's condition, tier and capacity as a view model", async () => {
    server.use(
      http.post(statusPath, () =>
        HttpResponse.json({
          status: {
            state: "LICENSE_STATE_VALID",
            edition: "standard",
            channels: 16,
            channelsInUse: 13,
            maxTenants: 0,
            maxExtensions: 200,
            expiresAt: "2027-01-31T00:00:00Z",
            condition: "LICENSE_CONDITION_CAPACITY_WARNING",
            tier: "LICENSE_TIER_COMMERCIAL",
            capacity: "CAPACITY_LEVEL_WARNING",
          },
        }),
      ),
    );
    const { result } = renderHook(() => useLicenceStatus(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({
      condition: LicenseCondition.CAPACITY_WARNING,
      tier: LicenseTier.COMMERCIAL,
      capacity: CapacityLevel.WARNING,
      edition: "standard",
      channels: 16,
      channelsInUse: 13,
      maxTenants: null,
      maxExtensions: 200,
      expiresAt: new Date("2027-01-31T00:00:00Z"),
      lastConfirmedAt: null,
    });
  });

  it("hands components an ApiError with console copy, not the server's text", async () => {
    server.use(http.post(statusPath, () => HttpResponse.json({ code: "unavailable", message: "ari: asterisk:8088 refused" }, { status: 503 })));
    const { result } = renderHook(() => useLicenceStatus(), { wrapper });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(result.current.error).toBeInstanceOf(ApiError);
    expect(result.current.error?.kind).toBe("unavailable");
    expect(result.current.error?.message).not.toMatch(/ari|asterisk|8088/);
  });

  it("reads a missing status as unknown, never as valid", () => {
    expect(toLicenceSummary(undefined).condition).toBe(LicenseCondition.UNSPECIFIED);
  });
});
