import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import { routes } from "../../app/routes";
import { server } from "../../test/msw";
import { renderApp } from "../../test/session";

const LIST = "http://localhost:3000/atsapbx.v1.ReportingService/ListCallRecords";
const EXPORT = "http://localhost:3000/atsapbx.v1.ReportingService/ExportCallRecords";

const record = {
  callId: "c1", direction: "OUTBOUND", sourceNumber: "1001", destNumber: "+4420",
  startedAt: "2026-11-04T10:00:00Z", answeredAt: "2026-11-04T10:00:01Z", endedAt: "2026-11-04T10:01:00Z",
  durationSeconds: 59, terminationReason: "normal_clearing",
  participants: [{ participantId: "p1", role: "CALLER", extensionId: "x1", principalId: "pr1", billableSeconds: 59, usageSeconds: 59 }],
};

describe("reports", () => {
  it("lists terminated records with the person and exports CSV", async () => {
    let listBody: unknown;
    let exportBody: unknown;
    server.use(
      http.post(LIST, async ({ request }) => {
        listBody = await request.json();
        return HttpResponse.json({ records: [record] });
      }),
      http.post(EXPORT, async ({ request }) => {
        exportBody = await request.json();
        return HttpResponse.json({ csv: btoa("call_id,direction\nc1,OUTBOUND\n") });
      }),
    );
    const create = vi.fn<() => string>(() => "blob:report");
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: create });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn<() => void>() });
    const user = userEvent.setup();
    renderApp(routes, "/reports");
    const table = within(await screen.findByRole("table", { name: "Call records of this tenant" }));
    const row = table.getByRole("row", { name: /1001/ });
    expect(row).toHaveTextContent("+4420");
    expect(row).toHaveTextContent(/CALLER/);
    expect(listBody).toMatchObject({ tenantId: "11111111-1111-4111-8111-111111111111", limit: 50 });

    await user.selectOptions(screen.getByLabelText("Direction"), "OUTBOUND");
    await user.type(screen.getByLabelText("Number"), "+4420");
    await user.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(listBody).toMatchObject({ filter: { direction: "OUTBOUND", number: "+4420" } }));

    await user.click(screen.getByRole("button", { name: "Export CSV" }));
    await waitFor(() => expect(exportBody).toBeDefined());
    expect(create).toHaveBeenCalled();
  });

  it("explains an empty report", async () => {
    server.use(http.post(LIST, () => HttpResponse.json({ records: [] })));
    renderApp(routes, "/reports");
    expect(await screen.findByText("No terminated calls match these filters.")).toBeInTheDocument();
  });
});

describe("report edges", () => {
  const EXT_URL = "http://localhost:3000/atsapbx.v1.PbxService/ListExtensions";
  const PEOPLE_URL = "http://localhost:3000/atsapbx.v1.IdentityService/ListPrincipals";

  it("maps extension and person names and handles an empty participant list", async () => {
    server.use(
      http.post(LIST, () => HttpResponse.json({ records: [
        record,
        { callId: "c2", direction: "INBOUND", sourceNumber: "+4420", destNumber: "1002", participants: [] },
      ] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [{ extensionId: "x1", number: "1001", displayName: "Reception", authUsername: "e_x", deviceType: "DEVICE_TYPE_SIP" }] })),
      http.post(PEOPLE_URL, () => HttpResponse.json({ principals: [{ principal: { principalId: "pr1", displayName: "Ana", email: "ana@example.test", status: "PRINCIPAL_STATUS_ACTIVE" }, roleBindings: [] }] })),
    );
    renderApp(routes, "/reports");
    const table = within(await screen.findByRole("table", { name: "Call records of this tenant" }));
    expect(table.getByRole("row", { name: /1001/ })).toHaveTextContent(/CALLER: 1001 · Ana · ana@example.test/);
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("—");
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("—");
  });

  it("reports load and export failures", async () => {
    server.use(http.post(LIST, () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })));
    const first = renderApp(routes, "/reports");
    expect(await screen.findByText("Something went wrong. Try again, and contact your administrator if it continues.")).toBeInTheDocument();
    first.unmount();

    server.use(
      http.post(LIST, () => HttpResponse.json({ records: [record] })),
      http.post(EXPORT, () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/reports");
    await screen.findByRole("table", { name: "Call records of this tenant" });
    await user.click(screen.getByRole("button", { name: "Export CSV" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong. Try again, and contact your administrator if it continues.");
  });

  it("sends date, extension and person filters and pages", async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.post(LIST, async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ records: Array.from({ length: 50 }, (_, i) => ({ ...record, callId: `c${i}` })) });
      }),
      http.post("http://localhost:3000/atsapbx.v1.PbxService/ListExtensions", () => HttpResponse.json({ extensions: [{ extensionId: "x1", number: "1001", displayName: "Reception", authUsername: "e_x", deviceType: "DEVICE_TYPE_SIP" }] })),
      http.post("http://localhost:3000/atsapbx.v1.IdentityService/ListPrincipals", () => HttpResponse.json({ principals: [{ principal: { principalId: "pr1", displayName: "Ana", email: "ana@example.test", status: "PRINCIPAL_STATUS_ACTIVE" }, roleBindings: [] }] })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/reports");
    await user.type(await screen.findByLabelText("From"), "2026-11-01");
    await user.type(screen.getByLabelText("To"), "2026-11-04");
    await user.selectOptions(screen.getByLabelText("Extension"), "x1");
    await user.selectOptions(screen.getByLabelText("Person"), "pr1");
    await user.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(body).toMatchObject({
      filter: { extensionId: "x1", principalId: "pr1" },
    }));
    expect(body?.filter).toHaveProperty("from");
    expect(body?.filter).toHaveProperty("to");
    await user.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(body).toMatchObject({ offset: 50 }));
  });
});
