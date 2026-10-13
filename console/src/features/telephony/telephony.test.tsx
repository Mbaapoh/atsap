import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { OVERVIEW_URLS, server } from "../../test/msw";
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
