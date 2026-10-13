import { act, screen, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import { routes } from "../../app/routes";
import { PAGE_SIZE } from "../../components/Pager";
import { OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, tenantA } from "../../test/session";
import { REGISTRATION_REFRESH_MS } from "./adapter";

// console-admin-screens task 3.1: the extensions list (HLD-18 A3).

export const ENGINE_ID = "e_0123456789abcdef0123456789abcdef";
export const ext1001 = { extensionId: "x1", number: "1001", displayName: "Reception", authUsername: ENGINE_ID, deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_REGISTERED" };
export const ext1002 = { extensionId: "x2", number: "1002", displayName: "Kitchen", authUsername: "e_fedcba9876543210fedcba9876543210", deviceType: "DEVICE_TYPE_WEBRTC", registration: "REGISTRATION_STATUS_NOT_REGISTERED" };
export const ext1003 = { extensionId: "x3", number: "1003", displayName: "Spare", authUsername: "e_00000000000000000000000000000003", deviceType: "DEVICE_TYPE_SIP" };

describe("extensions list", () => {
  it("shows number, name, device and observed registration, for the session's tenant", async () => {
    const bodies: unknown[] = [];
    server.use(http.post(OVERVIEW_URLS.extensions, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json({ extensions: [ext1001, ext1002, ext1003] });
    }));
    renderApp(routes, "/extensions");
    const table = within(await screen.findByRole("table", { name: "Extensions of this tenant" }));
    expect(table.getByRole("row", { name: /1001/ })).toHaveTextContent("Reception");
    expect(table.getByRole("row", { name: /1001/ })).toHaveTextContent("Registered");
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("Not registered");
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("Browser phone (WebRTC)");
    // A status the server did not report is Unknown, never "Not registered".
    expect(table.getByRole("row", { name: /1003/ })).toHaveTextContent("Unknown");
    expect(bodies[0]).toEqual({ tenantId: tenantA.tenantId, limit: PAGE_SIZE });
  });

  it("never renders a SIP username in the list", async () => {
    server.use(http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ extensions: [ext1001, ext1002] })));
    renderApp(routes, "/extensions");
    await screen.findByRole("table", { name: "Extensions of this tenant" });
    expect(document.body.innerHTML).not.toMatch(/e_[0-9a-f]{32}/);
  });

  it("re-reads registration while open", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let reads = 0;
    server.use(http.post(OVERVIEW_URLS.extensions, () => {
      reads++;
      return HttpResponse.json({ extensions: [{ ...ext1001, registration: reads > 1 ? "REGISTRATION_STATUS_NOT_REGISTERED" : "REGISTRATION_STATUS_REGISTERED" }] });
    }));
    renderApp(routes, "/extensions");
    const row = await screen.findByRole("row", { name: /1001/ });
    expect(row).toHaveTextContent("Registered");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REGISTRATION_REFRESH_MS + 100);
    });
    expect(await screen.findByText("Not registered")).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("an empty tenant is told how to start", async () => {
    renderApp(routes, "/extensions");
    expect(await screen.findByText("No extensions yet")).toBeInTheDocument();
  });

  it("a refusal reads as console copy", async () => {
    server.use(http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ code: "permission_denied", message: "x" }, { status: 403 })));
    renderApp(routes, "/extensions");
    expect(await screen.findByText("Your role cannot see this tenant's extensions.")).toBeInTheDocument();
  });
});

describe("creating an extension", () => {
  const CREATE = "http://localhost:3000/atsapbx.v1.PbxService/CreateExtension";
  const SECRET = "KZXW6YTBOI4TCMRTGQ2DKNRXHA4TS2LB";

  async function openAndFill(user: ReturnType<typeof import("@testing-library/user-event").default.setup>, number = "1004", name = "Lobby") {
    await user.click(await screen.findByRole("button", { name: "Add extension" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add an extension" }));
    if (number) await user.type(dialog.getByLabelText("Number"), number);
    if (name) await user.type(dialog.getByLabelText("Name"), name);
    return dialog;
  }

  it("creates once, then shows server, SIP username and password once", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    let body: unknown;
    let lists = 0;
    server.use(
      http.post(OVERVIEW_URLS.extensions, () => {
        lists++;
        return HttpResponse.json({ extensions: [ext1001] });
      }),
      http.post(CREATE, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ extension: { ...ext1001, extensionId: "x4", number: "1004", displayName: "Lobby", authUsername: ENGINE_ID }, secret: SECRET });
      }),
    );
    const user = userEvent.setup();
    const { queryClient } = renderApp(routes, "/extensions");
    const dialog = await openAndFill(user);
    await user.click(dialog.getByRole("button", { name: "Add extension" }));

    const panel = within(await screen.findByRole("dialog", { name: "Extension 1004 created" }));
    expect(body).toEqual({ tenantId: tenantA.tenantId, number: "1004", displayName: "Lobby", deviceType: "DEVICE_TYPE_SIP" });
    expect(panel.getByText("localhost")).toBeInTheDocument();
    expect(panel.getByText("5060")).toBeInTheDocument();
    expect(panel.getByText("UDP")).toBeInTheDocument();
    expect(panel.getByText(SECRET)).toBeInTheDocument();
    const username = panel.getByText(ENGINE_ID);
    expect(username).toHaveAttribute("data-sip-username");
    expect(lists).toBeGreaterThan(1);

    await user.click(panel.getByRole("checkbox", { name: "I have saved these details" }));
    await user.click(panel.getByRole("button", { name: "Done" }));
    expect(document.body.textContent).not.toContain(SECRET);
    expect(JSON.stringify(queryClient.getMutationCache().getAll().map((m) => m.state.data))).not.toContain(SECRET);
    expect(JSON.stringify(queryClient.getQueryCache().getAll().map((q) => q.state.data))).not.toContain(SECRET);
    expect(window.location.href).not.toContain(SECRET);
  });

  it("says when the SIP server address is not configured", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    const { REGISTRAR_URL } = await import("../../test/msw");
    server.use(
      http.post(REGISTRAR_URL, () => HttpResponse.json({ configured: false })),
      http.post(CREATE, () => HttpResponse.json({ extension: { ...ext1001, number: "1004" }, secret: SECRET })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/extensions");
    const dialog = await openAndFill(user);
    await user.click(dialog.getByRole("button", { name: "Add extension" }));
    const panel = within(await screen.findByRole("dialog", { name: "Extension 1004 created" }));
    expect(panel.getByText("The SIP server address is not configured")).toBeInTheDocument();
    expect(panel.queryByText("SIP server", { selector: "dt" })).not.toBeInTheDocument();
  });

  it("tells a taken number from a full licence", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    let reply = { code: "already_exists", message: "extension number already exists in this tenant" };
    server.use(http.post(CREATE, () => HttpResponse.json(reply, { status: reply.code === "already_exists" ? 409 : 400 })));
    const user = userEvent.setup();
    renderApp(routes, "/extensions");
    const dialog = await openAndFill(user, "1001");
    await user.click(dialog.getByRole("button", { name: "Add extension" }));
    expect(await dialog.findByText("Extension 1001 already exists in this tenant.")).toBeInTheDocument();

    reply = { code: "failed_precondition", message: "licence entitlement exceeded: extension limit reached" };
    await user.clear(dialog.getByLabelText("Number"));
    await user.type(dialog.getByLabelText("Number"), "1099");
    await user.click(dialog.getByRole("button", { name: "Add extension" }));
    expect(await dialog.findByText("The licence allows no more extensions. Free one up or extend the licence.")).toBeInTheDocument();
    expect(screen.queryByText(/entitlement exceeded/)).not.toBeInTheDocument();
  });

  it("checks the number and name before sending", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    let sent = 0;
    server.use(http.post(CREATE, () => {
      sent++;
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/extensions");
    const dialog = await openAndFill(user, "1a", "");
    await user.click(dialog.getByRole("button", { name: "Add extension" }));
    expect(dialog.getByLabelText("Number")).toHaveAccessibleDescription("Use 2 to 20 digits, like 1001.");
    expect(dialog.getByLabelText("Name")).toHaveAccessibleDescription("Enter a name, like Reception.");
    expect(sent).toBe(0);
  });

  it("is offered to administrators only", async () => {
    const { signedIn } = await import("../../test/session");
    renderApp(routes, "/extensions", signedIn({ memberships: [{ ...tenantA, roles: ["SUPERVISOR"] }] }));
    await screen.findByRole("heading", { level: 1, name: "Extensions" });
    expect(screen.queryByRole("button", { name: "Add extension" })).not.toBeInTheDocument();
  });
});
