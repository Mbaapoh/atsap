import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, tenantA } from "../../test/session";

// pbx-carrier-addresses task 4.1 (D-76): a trunk's inbound signalling
// addresses in the console.

const api = (p: string) => `http://localhost:3000/atsapbx.v1.PbxService/${p}`;
const withRanges = { trunkId: "t9", name: "Carrier R", host: "sip.carrier-r.example", port: 5060, channelLimit: 4, priority: 1, health: "TRUNK_HEALTH_HEALTHY", inboundAddresses: ["198.51.100.0/24", "203.0.113.9"] };

function trunks(list: object[]) {
  return http.post(OVERVIEW_URLS.trunks, () => HttpResponse.json({ trunks: list }));
}

describe("inbound signalling addresses", () => {
  it("are shown under the host", async () => {
    server.use(trunks([withRanges]));
    renderApp(routes, "/trunks");
    const row = await screen.findByRole("row", { name: /Carrier R/ });
    expect(row).toHaveTextContent("Also from 198.51.100.0/24, 203.0.113.9");
  });

  it("a host that cannot be a carrier, or a bad line, is refused at its field and nothing is sent", async () => {
    let sent = false;
    server.use(trunks([]), http.post(api("CreateTrunk"), () => {
      sent = true;
      return HttpResponse.json({ trunk: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Add trunk" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add a trunk" }));
    await user.type(dialog.getByLabelText("Name"), "Loop");
    await user.type(dialog.getByLabelText("Host"), "localhost");
    await user.type(dialog.getByLabelText("Channel limit"), "2");
    await user.type(dialog.getByLabelText("Inbound signalling addresses (optional)"), "203.0.113.9{Enter}10.0.0.0/8");
    await user.click(dialog.getByRole("button", { name: "Add trunk" }));
    expect(dialog.getByLabelText("Host")).toHaveAccessibleDescription(/cannot be a carrier/);
    expect(dialog.getByLabelText("Inbound signalling addresses (optional)")).toHaveAccessibleDescription(/Line 2 \(10.0.0.0\/8\) is too broad/);
    expect(sent).toBe(false);
  });

  it("are sent exactly as entered, blank lines dropped", async () => {
    let body: unknown;
    server.use(trunks([]), http.post(api("CreateTrunk"), async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ trunk: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Add trunk" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add a trunk" }));
    await user.type(dialog.getByLabelText("Name"), "Carrier R");
    await user.type(dialog.getByLabelText("Host"), "sip.carrier-r.example");
    await user.type(dialog.getByLabelText("Channel limit"), "4");
    await user.type(dialog.getByLabelText("Inbound signalling addresses (optional)"), " 203.0.113.9 {Enter}{Enter}198.51.100.0/24");
    await user.click(dialog.getByRole("button", { name: "Add trunk" }));
    expect(await screen.findByText("Trunk Carrier R added.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, name: "Carrier R", host: "sip.carrier-r.example", channelLimit: 4, inboundAddresses: ["203.0.113.9", "198.51.100.0/24"] });
  });

  it("an edit starts from the stored list and replaces it; clearing it sends none", async () => {
    const bodies: Record<string, unknown>[] = [];
    server.use(trunks([withRanges]), http.post(api("UpdateTrunk"), async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>);
      return HttpResponse.json({ trunk: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Actions for trunk Carrier R" }));
    await user.click(screen.getByRole("menuitem", { name: "Edit…" }));
    let dialog = within(screen.getByRole("dialog", { name: "Edit trunk Carrier R" }));
    const field = dialog.getByLabelText("Inbound signalling addresses (optional)");
    expect(field).toHaveValue("198.51.100.0/24\n203.0.113.9");
    await user.clear(field);
    await user.type(field, "192.0.2.0/28");
    await user.click(dialog.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Trunk Carrier R updated.")).toBeInTheDocument();
    expect(bodies[0]?.inboundAddresses).toEqual(["192.0.2.0/28"]);

    await user.click(await screen.findByRole("button", { name: "Actions for trunk Carrier R" }));
    await user.click(screen.getByRole("menuitem", { name: "Edit…" }));
    dialog = within(screen.getByRole("dialog", { name: "Edit trunk Carrier R" }));
    await user.clear(dialog.getByLabelText("Inbound signalling addresses (optional)"));
    await user.click(dialog.getByRole("button", { name: "Save changes" }));
    expect(await screen.findAllByText("Trunk Carrier R updated.")).not.toHaveLength(0);
    expect(bodies[1]).not.toHaveProperty("inboundAddresses"); // none: an empty list is not written on the wire
  });

  it("a trunk whose host resolves only to loopback says why", async () => {
    server.use(trunks([{ ...withRanges, health: "TRUNK_HEALTH_UNHEALTHY", healthReason: "TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED", healthObservedAt: "2026-11-01T10:00:00Z" }]));
    renderApp(routes, "/trunks");
    const row = await screen.findByRole("row", { name: /Carrier R/ });
    expect(row).toHaveTextContent("Unhealthy");
    expect(row).toHaveTextContent("Address not allowed");
  });
});
