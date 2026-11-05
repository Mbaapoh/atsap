import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { server } from "../../test/msw";
import { renderApp, signedIn, tenantA } from "../../test/session";

const BASE = "http://localhost:3000/atsapbx.v1.PbxService/";
const EXT_URL = BASE + "ListExtensions";
const flow = {
  flowId: "f1", number: "4420", name: "Main line", activeVersion: 2,
  config: { openExtensionId: "x1", closedExtensionId: "x2", businessStartMinute: 480, businessEndMinute: 1080 },
};

function ext(id: string, number: string) {
  return { extensionId: id, number, displayName: number, authUsername: "e_" + id, deviceType: "DEVICE_TYPE_SIP" };
}

describe("call flows", () => {
  it("lists a flow and creates a new one", async () => {
    let body: unknown;
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [ext("x1", "1001"), ext("x2", "1002")] })),
      http.post(BASE + "CreateFlow", async ({ request }) => { body = await request.json(); return HttpResponse.json({ flow }); }),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    const table = within(await screen.findByRole("table", { name: "Call flows of this tenant" }));
    expect(table.getByRole("row", { name: /4420/ })).toHaveTextContent("Main line");
    expect(table.getByRole("row", { name: /4420/ })).toHaveTextContent("1001");
    await user.click(screen.getByRole("button", { name: "Add flow" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add a call flow" }));
    await user.type(dialog.getByLabelText("DID number"), "4421");
    await user.type(dialog.getByLabelText("Name"), "Sales line");
    await user.selectOptions(dialog.getByLabelText("Open destination"), "x1");
    await user.clear(dialog.getByLabelText("Open from (UTC)"));
    await user.type(dialog.getByLabelText("Open from (UTC)"), "09:00");
    await user.clear(dialog.getByLabelText("Open until (UTC)"));
    await user.type(dialog.getByLabelText("Open until (UTC)"), "17:00");
    await user.type(dialog.getByLabelText("Prompt (optional)"), "welcome");
    await user.click(dialog.getByRole("button", { name: "Create flow" }));
    await waitFor(() => expect(body).toMatchObject({ number: "4421", name: "Sales line", config: { openExtensionId: "x1" } }));
  });

  it("publishes a version and rolls back an older one", async () => {
    let published: unknown;
    let rolledBack: unknown;
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [ext("x1", "1001"), ext("x2", "1002")] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
      http.post(BASE + "PublishFlow", async ({ request }) => { published = await request.json(); return HttpResponse.json({ flow }); }),
      http.post(BASE + "ListFlowVersions", () => HttpResponse.json({ versions: [
        { version: 2, config: flow.config, publishedAt: "2026-11-04T10:00:00Z" },
        { version: 1, config: flow.config, publishedAt: "2026-11-03T10:00:00Z" },
      ] })),
      http.post(BASE + "RollbackFlow", async ({ request }) => { rolledBack = await request.json(); return HttpResponse.json({ flow }); }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    const row = await screen.findByRole("row", { name: /4420/ });
    await user.click(within(row).getByRole("button", { name: "Publish…" }));
    const publish = within(screen.getByRole("dialog", { name: "Publish Main line" }));
    await user.selectOptions(publish.getByLabelText("Closed destination"), "x2");
    await user.click(publish.getByRole("button", { name: "Publish version" }));
    await waitFor(() => expect(published).toMatchObject({ flowId: "f1", config: { closedExtensionId: "x2" } }));

    await user.click(within(row).getByRole("button", { name: "Versions" }));
    const versions = within(await screen.findByRole("dialog", { name: "Versions of Main line" }));
    await user.click(await versions.findByRole("button", { name: "Roll back" }));
    await waitFor(() => expect(rolledBack).toMatchObject({ flowId: "f1", version: 1 }));
  });

  it("adds and removes a holiday", async () => {
    let added: unknown;
    let removed: unknown;
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [{ day: "2026-12-25", name: "Christmas" }] })),
      http.post(BASE + "AddHoliday", async ({ request }) => { added = await request.json(); return HttpResponse.json({ holidays: [{ day: "2026-12-25", name: "Christmas" }] }); }),
      http.post(BASE + "DeleteHoliday", async ({ request }) => { removed = await request.json(); return HttpResponse.json({ holidays: [] }); }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    await user.type(await screen.findByLabelText("Day"), "2026-12-25");
    await user.type(screen.getByLabelText("Name"), "Christmas");
    await user.click(screen.getByRole("button", { name: "Add holiday" }));
    await waitFor(() => expect(added).toMatchObject({ day: "2026-12-25", name: "Christmas" }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(removed).toMatchObject({ day: "2026-12-25" }));
  });
});

describe("flow edges", () => {
  it("reports a load failure and an empty versions list", async () => {
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
    );
    const first = renderApp(routes, "/flows");
    expect(await screen.findByText("Call flows could not be loaded")).toBeInTheDocument();
    first.unmount();

    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [] })),
      http.post(BASE + "ListFlowVersions", () => HttpResponse.json({ versions: [] })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    const row = await screen.findByRole("row", { name: /4420/ });
    await user.click(within(row).getByRole("button", { name: "Versions" }));
    expect(await screen.findByText("No versions.")).toBeInTheDocument();
  });

  it("deletes a flow after confirmation and reports failures", async () => {
    let deleted = false;
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [] })),
      http.post(BASE + "DeleteFlow", () => { deleted = true; return HttpResponse.json({}); }),
      http.post(BASE + "CreateFlow", () => HttpResponse.json({ code: "invalid_argument", message: "no" }, { status: 400 })),
      http.post(BASE + "PublishFlow", () => HttpResponse.json({ code: "invalid_argument", message: "no" }, { status: 400 })),
      http.post(BASE + "AddHoliday", () => HttpResponse.json({ code: "invalid_argument", message: "no" }, { status: 400 })),
    );
    Object.defineProperty(window, "confirm", { configurable: true, value: () => true });
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    const row = await screen.findByRole("row", { name: /4420/ });
    await user.click(within(row).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleted).toBe(true));

    await user.click(screen.getByRole("button", { name: "Add flow" }));
    const add = within(screen.getByRole("dialog", { name: "Add a call flow" }));
    await user.type(add.getByLabelText("DID number"), "1");
    await user.type(add.getByLabelText("Name"), "x");
    await user.click(add.getByRole("button", { name: "Create flow" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    await user.click(within(row).getByRole("button", { name: "Publish…" }));
    const pub = within(screen.getByRole("dialog", { name: "Publish Main line" }));
    await user.click(pub.getByRole("button", { name: "Publish version" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    await user.type(screen.getByLabelText("Day"), "2026-12-25");
    await user.click(screen.getByRole("button", { name: "Add holiday" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });
});

describe("flow failure paths", () => {
  it("cancels a delete, reports delete/rollback/holiday failures, and closes dialogs", async () => {
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [{ day: "2026-12-25", name: "Christmas" }] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [] })),
      http.post(BASE + "DeleteFlow", () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
      http.post(BASE + "ListFlowVersions", () => HttpResponse.json({ versions: [
        { version: 2, config: flow.config, publishedAt: "2026-11-04T10:00:00Z" },
        { version: 1, config: flow.config, publishedAt: "2026-11-03T10:00:00Z" },
      ] })),
      http.post(BASE + "RollbackFlow", () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
      http.post(BASE + "DeleteHoliday", () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
    );
    Object.defineProperty(window, "confirm", { configurable: true, value: () => false });
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    const row = await screen.findByRole("row", { name: /4420/ });
    await user.click(within(row).getByRole("button", { name: "Delete" }));
    Object.defineProperty(window, "confirm", { configurable: true, value: () => true });
    await user.click(within(row).getByRole("button", { name: "Delete" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    await user.click(within(row).getByRole("button", { name: "Versions" }));
    const versions = within(await screen.findByRole("dialog", { name: "Versions of Main line" }));
    await user.click(await versions.findByRole("button", { name: "Roll back" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    await user.click(versions.getByRole("button", { name: "Close" }));

    await user.click(screen.getByRole("button", { name: "Remove" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Add flow" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog", { name: "Add a call flow" })).not.toBeInTheDocument();
  });
});

describe("flow defaults and read-only", () => {
  it("renders a flow without config and a version without a date", async () => {
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [{ flowId: "f2", number: "4422", name: "Bare", activeVersion: 1 }] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [{ day: "2026-12-25", name: "" }] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [] })),
      http.post(BASE + "ListFlowVersions", () => HttpResponse.json({ versions: [{ version: 1, config: {} }] })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/flows");
    expect(await screen.findByRole("row", { name: /4422/ })).toHaveTextContent("08:00–18:00");
    expect(screen.getByRole("row", { name: /2026-12-25/ })).toHaveTextContent("—");
    await user.click(within(screen.getByRole("row", { name: /4422/ })).getByRole("button", { name: "Versions" }));
    const versions = within(await screen.findByRole("dialog", { name: "Versions of Bare" }));
    expect(await versions.findByRole("row", { name: /v1/ })).toHaveTextContent("—");
  });

  it("is read-only for an agent", async () => {
    server.use(
      http.post(BASE + "ListFlows", () => HttpResponse.json({ flows: [flow] })),
      http.post(BASE + "ListHolidays", () => HttpResponse.json({ holidays: [] })),
      http.post(EXT_URL, () => HttpResponse.json({ extensions: [] })),
    );
    renderApp(routes, "/flows", signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }] }));
    await screen.findByRole("row", { name: /4420/ });
    expect(screen.queryByRole("button", { name: "Add flow" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Publish…" })).not.toBeInTheDocument();
  });
});
