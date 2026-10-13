import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { INBOUND_URL, OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, signedIn, tenantA } from "../../test/session";

// console-admin-screens task 4.1: trunks (HLD-18 A4).

const api = (p: string) => `http://localhost:3000/atsapbx.v1.PbxService/${p}`;
export const carrierA = { trunkId: "t1", name: "Carrier A", host: "sip.carrier-a.example", port: 5060, authUsername: "acct-77", hasSecret: true, channelLimit: 30, priority: 1, health: "TRUNK_HEALTH_HEALTHY" };
export const carrierB = { trunkId: "t2", name: "Carrier B", host: "198.51.100.9", channelLimit: 10, priority: 2, health: "TRUNK_HEALTH_DEGRADED" };
const TRUNK_SECRET = "carrier-secret-value-1";

function trunks(list = [carrierA, carrierB]) {
  return http.post(OVERVIEW_URLS.trunks, () => HttpResponse.json({ trunks: list }));
}

describe("trunks", () => {
  it("lists carriers with authentication and observed health, never a secret", async () => {
    server.use(trunks());
    renderApp(routes, "/trunks");
    const table = within(await screen.findByRole("table", { name: "Trunks of this tenant" }));
    const a = table.getByRole("row", { name: /Carrier A/ });
    expect(a).toHaveTextContent("sip.carrier-a.example");
    expect(a).toHaveTextContent("Username and secret");
    expect(a).toHaveTextContent("Healthy");
    const b = table.getByRole("row", { name: /Carrier B/ });
    expect(b).toHaveTextContent("By IP address");
    expect(b).toHaveTextContent("5060"); // port 0 is the SIP default
    expect(b).toHaveTextContent("Degraded");
    expect(table.queryByRole("textbox")).not.toBeInTheDocument(); // health is never an input
  });

  it("adds a trunk with exactly what was entered; the secret needs a username", async () => {
    let body: unknown;
    server.use(trunks([]), http.post(api("CreateTrunk"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ trunk: {} });
    }));
    const user = userEvent.setup();
    const { queryClient } = renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Add trunk" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add a trunk" }));
    await user.type(dialog.getByLabelText("Name"), "Carrier C");
    await user.type(dialog.getByLabelText("Host"), "sip:carrier-c.example:5060");
    await user.type(dialog.getByLabelText("Channel limit"), "8");
    await user.type(dialog.getByLabelText("Secret (optional)"), TRUNK_SECRET);
    await user.click(dialog.getByRole("button", { name: "Add trunk" }));
    expect(dialog.getByLabelText("Host")).toHaveAccessibleDescription(/no sip:, port or user/);
    expect(dialog.getByLabelText("Secret (optional)")).toHaveAccessibleDescription(/A secret needs a username/);
    expect(body).toBeUndefined();

    await user.clear(dialog.getByLabelText("Host"));
    await user.type(dialog.getByLabelText("Host"), "carrier-c.example");
    await user.type(dialog.getByLabelText("Username (optional)"), "acct-9");
    // The secret typed before is kept through a validation error.
    await user.click(dialog.getByRole("button", { name: "Add trunk" }));
    expect(await screen.findByText("Trunk Carrier C added.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, name: "Carrier C", host: "carrier-c.example", authUsername: "acct-9", secret: TRUNK_SECRET, channelLimit: 8 });
    expect(document.body.textContent).not.toContain(TRUNK_SECRET);
    expect(JSON.stringify(queryClient.getMutationCache().getAll().map((m) => m.state.variables))).not.toContain(TRUNK_SECRET);
  });

  it("editing with an empty secret keeps the stored one (it is not sent)", async () => {
    let body: Record<string, unknown> = {};
    server.use(trunks(), http.post(api("UpdateTrunk"), async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({ trunk: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for trunk Carrier A" }));
    await user.click(screen.getByRole("menuitem", { name: "Edit…" }));
    const dialog = within(screen.getByRole("dialog", { name: "Edit trunk Carrier A" }));
    expect(dialog.getByLabelText("Secret (optional)")).toHaveValue("");
    expect(dialog.getByLabelText("Secret (optional)")).toHaveAccessibleDescription(/A secret is stored. Leave empty to keep it/);
    await user.clear(dialog.getByLabelText("Channel limit"));
    await user.type(dialog.getByLabelText("Channel limit"), "40");
    await user.click(dialog.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Trunk Carrier A updated.")).toBeInTheDocument();
    expect(body).toMatchObject({ tenantId: tenantA.tenantId, trunkId: "t1", channelLimit: 40, authUsername: "acct-77" });
    expect(body).not.toHaveProperty("secret");
  });

  it("deleting says its routes go too", async () => {
    let body: unknown;
    server.use(trunks(), http.post(api("DeleteTrunk"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for trunk Carrier B" }));
    await user.click(screen.getByRole("menuitem", { name: "Delete trunk…" }));
    const confirm = within(screen.getByRole("dialog", { name: "Delete trunk Carrier B?" }));
    expect(confirm.getByText(/Every outbound route that uses it is deleted too/)).toBeInTheDocument();
    await user.click(confirm.getByRole("button", { name: "Delete trunk" }));
    expect(await screen.findByText("Trunk Carrier B deleted.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, trunkId: "t2" });
  });

  it("a non-administrator sees the trunks but no controls", async () => {
    server.use(trunks());
    renderApp(routes, "/trunks", signedIn({ memberships: [{ ...tenantA, roles: ["SUPERVISOR"] }] }));
    await screen.findByRole("table", { name: "Trunks of this tenant" });
    expect(screen.queryByRole("button", { name: "Add trunk" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for/ })).not.toBeInTheDocument();
  });
});

// console-admin-screens task 4.2: outbound routes (HLD-18 A4).

const routeA = { routeId: "r1", prefixPattern: "+237", trunkId: "t1", costPerMinuteMicros: "15000", priority: 1 };
const routeB = { routeId: "r2", prefixPattern: "+2376", trunkId: "t2", costPerMinuteMicros: "0", priority: 0 };

function routeList(list = [routeA, routeB]) {
  return http.post(OVERVIEW_URLS.routes, () => HttpResponse.json({ routes: list }));
}

describe("outbound routes", () => {
  it("lists prefix, trunk name, stored cost and priority; there is no Edit route", async () => {
    server.use(trunks(), routeList());
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    const table = within(await screen.findByRole("table", { name: "Outbound routes of this tenant" }));
    const a = table.getByRole("row", { name: /\+237 Carrier A/ });
    expect(a).toHaveTextContent("0.015");
    expect(a).toHaveTextContent("1");
    expect(table.getByRole("row", { name: /\+2376 Carrier B/ })).toHaveTextContent("0");
    expect(table.queryByText("t1")).not.toBeInTheDocument(); // ids are never shown
    await user.click(screen.getByRole("button", { name: "Actions for route +237" }));
    const items = screen.getAllByRole("menuitem").map((m) => m.textContent);
    expect(items).toEqual(["Replace…", "Delete route…"]);
    expect(screen.queryByRole("menuitem", { name: /Edit/ })).not.toBeInTheDocument();
  });

  it("adds a route with exactly what was entered; overlap is explained, never blocked", async () => {
    let body: unknown;
    server.use(trunks(), routeList([routeA]), http.post(api("CreateRoute"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ route: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await screen.findByRole("table", { name: "Outbound routes of this tenant" });
    await user.click(screen.getByRole("button", { name: "Add route" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add an outbound route" }));
    await user.type(dialog.getByLabelText("Prefix"), "sip:+23769");
    await user.type(dialog.getByLabelText("Cost per minute (optional)"), "0.1234567");
    await user.click(dialog.getByRole("button", { name: "Add route" }));
    expect(dialog.getByLabelText("Prefix")).toHaveAccessibleDescription(/Use digits and X/);
    expect(dialog.getByLabelText("Cost per minute (optional)")).toHaveAccessibleDescription(/up to 6 decimals/);
    expect(body).toBeUndefined();

    await user.clear(dialog.getByLabelText("Prefix"));
    await user.type(dialog.getByLabelText("Prefix"), "+23769");
    await user.clear(dialog.getByLabelText("Cost per minute (optional)"));
    await user.type(dialog.getByLabelText("Cost per minute (optional)"), "0.02");
    await user.selectOptions(dialog.getByLabelText("Trunk"), "Carrier B");
    await user.clear(dialog.getByLabelText("Priority"));
    await user.type(dialog.getByLabelText("Priority"), "3");
    expect(dialog.getByText(/overlaps \+237 \(Carrier A\)/)).toHaveTextContent(/healthy trunk are tried before those on a degraded one; then the longest matching prefix comes first, then the lower priority, then the lower stored cost/);
    await user.click(dialog.getByRole("button", { name: "Add route" }));
    expect(await screen.findByText("Route +23769 added.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, prefixPattern: "+23769", trunkId: "t2", costPerMinuteMicros: "20000", priority: 3 });
  });

  it("replace is confirmed, then creates the new route before deleting the old", async () => {
    const order: string[] = [];
    let created: unknown;
    server.use(
      trunks(),
      routeList(),
      http.post(api("CreateRoute"), async ({ request }) => {
        created = await request.json();
        order.push("create");
        return HttpResponse.json({ route: {} });
      }),
      http.post(api("DeleteRoute"), async ({ request }) => {
        order.push(`delete ${((await request.json()) as { routeId: string }).routeId}`);
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for route +237" }));
    await user.click(screen.getByRole("menuitem", { name: "Replace…" }));
    const dialog = within(screen.getByRole("dialog", { name: "Replace route +237" }));
    expect(dialog.getByLabelText("Prefix")).toHaveValue("+237");
    expect(dialog.getByLabelText("Cost per minute (optional)")).toHaveValue("0.015");
    await user.selectOptions(dialog.getByLabelText("Trunk"), "Carrier B");
    await user.click(dialog.getByRole("button", { name: "Continue" }));
    expect(order).toEqual([]);
    const confirm = within(screen.getByRole("dialog", { name: "Replace route +237?" }));
    expect(confirm.getByText(/added first, then the old one is deleted/)).toBeInTheDocument();
    await user.click(confirm.getByRole("button", { name: "Replace route" }));
    expect(await screen.findByText("Route +237 replaced.")).toBeInTheDocument();
    expect(order).toEqual(["create", "delete r1"]);
    expect(created).toEqual({ tenantId: tenantA.tenantId, prefixPattern: "+237", trunkId: "t2", costPerMinuteMicros: "15000", priority: 1 });
  });

  it("a replace whose delete fails says the new route exists and the old must go", async () => {
    server.use(
      trunks(),
      routeList(),
      http.post(api("CreateRoute"), () => HttpResponse.json({ route: {} })),
      http.post(api("DeleteRoute"), () => HttpResponse.json({ code: "internal", message: "pq: stasis-out" }, { status: 500 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for route +237" }));
    await user.click(screen.getByRole("menuitem", { name: "Replace…" }));
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(screen.getByRole("button", { name: "Replace route" }));
    expect(await screen.findByText(/The new route was added, but the old one could not be deleted/)).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/pq|stasis/);
  });

  it("deletes a route after confirmation", async () => {
    let body: unknown;
    server.use(trunks(), routeList(), http.post(api("DeleteRoute"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for route +2376" }));
    await user.click(screen.getByRole("menuitem", { name: "Delete route…" }));
    const confirm = within(screen.getByRole("dialog", { name: "Delete route +2376?" }));
    expect(confirm.getByText(/no longer use Carrier B/)).toBeInTheDocument();
    await user.click(confirm.getByRole("button", { name: "Delete route" }));
    expect(await screen.findByText("Route +2376 deleted.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, routeId: "r2" });
  });

  it("without trunks, adding a route is not offered", async () => {
    server.use(trunks([]), routeList([]));
    renderApp(routes, "/trunks");
    expect(await screen.findByText("No outbound routes yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add route" })).toBeDisabled();
    expect(screen.getByText(/Add a trunk first/)).toBeInTheDocument();
  });

  it("a failed load is console copy; a non-administrator gets no controls", async () => {
    server.use(trunks(), http.post(OVERVIEW_URLS.routes, () => HttpResponse.json({ code: "internal", message: "ps_endpoints" }, { status: 500 })));
    const { unmount } = renderApp(routes, "/trunks");
    expect(await screen.findByText("Routes could not be loaded")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("ps_endpoints");
    unmount();
    server.use(routeList());
    renderApp(routes, "/trunks", signedIn({ memberships: [{ ...tenantA, roles: ["SUPERVISOR"] }] }));
    await screen.findByRole("table", { name: "Outbound routes of this tenant" });
    expect(screen.queryByRole("button", { name: "Add route" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for route/ })).not.toBeInTheDocument();
  });
});

// console-admin-screens task 4.3: inbound numbers (HLD-18 A4).

const reception = { extensionId: "x1", number: "1001", displayName: "Reception", authUsername: "e_0123456789abcdef0123456789abcdef" };
const kitchen = { extensionId: "x2", number: "1002", displayName: "" };
const did1 = { inboundNumberId: "d1", number: "237222000111", extensionId: "x1" };
const did2 = { inboundNumberId: "d2", number: "237222000112", extensionId: "x2" };

function inbound(list: object[] = [did1, did2], extensions: object[] = [reception, kitchen]) {
  return [
    http.post(INBOUND_URL, () => HttpResponse.json({ numbers: list })),
    http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ extensions })),
  ];
}

describe("inbound numbers", () => {
  it("names each assignment's extension by number and display name, never an id or SIP username", async () => {
    server.use(...inbound());
    renderApp(routes, "/trunks");
    const table = within(await screen.findByRole("table", { name: "Inbound numbers of this tenant" }));
    expect(await table.findByRole("row", { name: /237222000111 1001 · Reception/ })).toBeInTheDocument();
    expect(table.getByRole("row", { name: /237222000112 1002/ })).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/x1|x2|e_[0-9a-f]{32}/);
  });

  it("reads every extension, page by page, to name them", async () => {
    const offsets: number[] = [];
    const many = Array.from({ length: 200 }, (_, i) => ({ extensionId: `p${i}`, number: String(2000 + i), displayName: `Desk ${i}` }));
    server.use(
      http.post(INBOUND_URL, () => HttpResponse.json({ numbers: [{ ...did1, extensionId: "late" }] })),
      http.post(OVERVIEW_URLS.extensions, async ({ request }) => {
        const { offset = 0, limit } = (await request.json()) as { offset?: number; limit: number };
        if (limit !== 200) return HttpResponse.json({});
        offsets.push(offset);
        return HttpResponse.json({ extensions: offset === 0 ? many : [{ extensionId: "late", number: "3000", displayName: "Annex" }] });
      }),
    );
    renderApp(routes, "/trunks");
    const table = within(await screen.findByRole("table", { name: "Inbound numbers of this tenant" }));
    expect(await table.findByText("3000 · Annex")).toBeInTheDocument();
    expect(offsets).toEqual([0, 200]);
  });

  it("assigns a number to the chosen extension, exactly as entered", async () => {
    let body: unknown;
    server.use(...inbound([]), http.post(api("CreateInboundNumber"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ inboundNumber: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    expect(await screen.findByText("No inbound numbers yet")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Assign number" }));
    const dialog = within(screen.getByRole("dialog", { name: "Assign an inbound number" }));
    expect(dialog.getAllByRole("option").map((o) => o.textContent)).toEqual(["1001 · Reception", "1002"]);
    await user.type(dialog.getByLabelText("Number"), "+237 222");
    await user.click(dialog.getByRole("button", { name: "Assign number" }));
    expect(dialog.getByLabelText("Number")).toHaveAccessibleDescription(/Use digits only/);
    expect(body).toBeUndefined();
    await user.clear(dialog.getByLabelText("Number"));
    await user.type(dialog.getByLabelText("Number"), "+237222000113");
    await user.selectOptions(dialog.getByLabelText("Rings extension"), "1002");
    await user.click(dialog.getByRole("button", { name: "Assign number" }));
    expect(await screen.findByText("+237222000113 now rings 1002.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, number: "+237222000113", extensionId: "x2" });
  });

  it("a number already assigned is explained at the field", async () => {
    server.use(...inbound(), http.post(api("CreateInboundNumber"), () => HttpResponse.json({ code: "already_exists", message: "inbound number taken" }, { status: 409 })));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await screen.findByRole("table", { name: "Inbound numbers of this tenant" });
    await user.click(await screen.findByRole("button", { name: "Assign number" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.type(dialog.getByLabelText("Number"), "237222000111");
    await user.click(dialog.getByRole("button", { name: "Assign number" }));
    expect(await dialog.findByText("This number is already assigned in this tenant. Remove it first to move it.")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("inbound number taken");
  });

  it("removal is confirmed and names the extension that stops ringing", async () => {
    let body: unknown;
    server.use(...inbound(), http.post(api("DeleteInboundNumber"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await screen.findByText("1001 · Reception");
    await user.click(screen.getByRole("button", { name: "Actions for number 237222000111" }));
    await user.click(screen.getByRole("menuitem", { name: "Remove number…" }));
    const confirm = within(screen.getByRole("dialog", { name: "Remove 237222000111?" }));
    expect(confirm.getByText("Calls to 237222000111 stop ringing extension 1001 · Reception. The extension itself keeps working.")).toBeInTheDocument();
    expect(confirm.getByRole("button", { name: "Cancel" })).toHaveFocus();
    expect(body).toBeUndefined();
    await user.click(confirm.getByRole("button", { name: "Remove number" }));
    expect(await screen.findByText("237222000111 removed.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, inboundNumberId: "d1" });
  });

  it("without extensions there is nothing to assign; a non-administrator gets no controls", async () => {
    server.use(...inbound([], []));
    const { unmount } = renderApp(routes, "/trunks");
    expect(await screen.findByText(/Add an extension first/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Assign number" })).toBeDisabled();
    unmount();
    server.use(...inbound());
    renderApp(routes, "/trunks", signedIn({ memberships: [{ ...tenantA, roles: ["SUPERVISOR"] }] }));
    await screen.findByText("1001 · Reception");
    expect(screen.queryByRole("button", { name: "Assign number" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for number/ })).not.toBeInTheDocument();
  });

  it("a failed load is console copy", async () => {
    server.use(http.post(INBOUND_URL, () => HttpResponse.json({ code: "permission_denied", message: "ps_endpoints" }, { status: 403 })));
    renderApp(routes, "/trunks");
    expect(await screen.findByText("Your role cannot see this tenant's inbound numbers.")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("ps_endpoints");
  });
});
