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
