import { expect, test, type Page } from "@playwright/test";
import { engineFindings, renderedSurface } from "./support/exposure";
import { mockSession } from "./support/session";

// console-call-session: the live call session and dialer, in a real
// browser against a stateful mock of the public API.

const REGISTERED = { extensionId: "e-1001", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_REGISTERED" };
const UNREGISTERED = { extensionId: "e-1002", number: "1002", displayName: "Lab", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_NOT_REGISTERED" };
const ENDED = { callId: "old", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+44207", startedAt: "2026-10-12T10:00:00Z", durationSeconds: 30, terminationReason: "normal clearing" };

type Phase = "ringing" | "connected" | "ended";

interface Mock {
  phase: Phase;
  placed: Record<string, unknown>[];
  hung: Record<string, unknown>[];
  calls: Record<string, unknown>[];
}

async function mockCalls(page: Page): Promise<Mock> {
  const m: Mock = { phase: "ringing", placed: [], hung: [], calls: [{ ...ENDED }] };
  await page.route("**/atsapbx.v1.PbxService/ListExtensions", (route) =>
    route.fulfill({ contentType: "application/json", body: JSON.stringify({ extensions: [REGISTERED, UNREGISTERED] }) }),
  );
  await page.route("**/atsapbx.v1.TelephonyService/ListCalls", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ calls: m.calls }) }));
  await page.route("**/atsapbx.v1.PbxService/PlaceCall", (route) => {
    const body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
    m.placed.push(body);
    m.calls = [{ callId: "new", direction: "INTERNAL", state: "Presenting", sourceNumber: "1001", destNumber: String(body.destination), startedAt: new Date().toISOString() }, ...m.calls];
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ callId: "new" }) });
  });
  await page.route("**/atsapbx.v1.TelephonyService/GetCall", (route) => {
    const state = m.phase === "connected" ? "Active" : m.phase === "ended" ? "Terminated" : "Presenting";
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        callId: "new",
        state,
        startedAt: "2026-11-02T20:00:00Z",
        answeredAt: state === "Active" || state === "Terminated" ? "2026-11-02T20:00:05Z" : undefined,
        endedAt: state === "Terminated" ? "2026-11-02T20:00:12Z" : undefined,
        terminationReason: state === "Terminated" ? "api_hangup" : undefined,
        participants: [
          { participantId: "p1", role: "CALLER", state: m.phase === "ringing" ? "Invited" : "Connected" },
          { participantId: "p2", role: "AGENT", state: state === "Active" ? "Connected" : "Invited" },
        ],
      }),
    });
  });
  await page.route("**/atsapbx.v1.PbxService/HangupCall", (route) => {
    m.hung.push((route.request().postDataJSON() ?? {}) as Record<string, unknown>);
    m.phase = "ended";
    m.calls = m.calls.map((c) => (c.callId === "new" ? { ...c, state: "Terminated", terminationReason: "api_hangup" } : c));
    return route.fulfill({ contentType: "application/json", body: "{}" });
  });
  return m;
}

async function openDialer(page: Page) {
  await page.getByRole("button", { name: "Place a call" }).click();
  return page.getByRole("dialog", { name: "New call" });
}

function mockSessionFor(page: Page) {
  return mockSession(page, ["TENANT_ADMIN"]);
}

for (const viewport of [
  { label: "desktop", width: 1280, height: 800 },
  { label: "phone", width: 390, height: 844 },
]) {
  test.describe(viewport.label, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("rings the caller first, cancels, and shows the reason", async ({ page }) => {
      await mockSessionFor(page);
      const m = await mockCalls(page);
      await page.goto("/calls");
      const dialog = await openDialer(page);
      await expect(dialog.getByLabel("From extension").locator("option")).toHaveText(["1001 · Reception", "1002 · Lab — No device registered"]);
      await expect(dialog.getByLabel("From extension").locator("option").nth(1)).toBeDisabled();
      const overflow = await dialog.evaluate((d) => d.scrollWidth - d.clientWidth);
      expect(overflow).toBe(0);

      // The dial pad types the destination.
      for (const k of ["1", "0", "0", "2"]) await dialog.getByRole("button", { name: `Add ${k}` }).click();
      await dialog.getByRole("button", { name: "Call" }).click();

      const bar = page.getByRole("region", { name: "Current call" });
      await expect(bar.getByText("Ringing your extension 1001")).toBeVisible();
      await expect(bar.getByText("Answer your phone to continue. 1002 is not dialled yet.")).toBeVisible();
      expect(engineFindings("call session", await renderedSurface(page))).toEqual([]);
      expect(m.placed).toEqual([{ tenantId: "11111111-1111-4111-8111-111111111111", extensionId: "e-1001", destination: "1002" }]);

      await bar.getByRole("button", { name: "Cancel call to 1002" }).click();
      await expect(bar.getByText("Call ended — ended from the console")).toBeVisible();
      expect(m.hung).toEqual([{ tenantId: "11111111-1111-4111-8111-111111111111", callId: "new" }]);
      await bar.getByRole("button", { name: "Dismiss" }).click();
      await expect(page.getByRole("region", { name: "Current call" })).toHaveCount(0);
    });

    test("shows a connected call and hangs up from the bar and the row", async ({ page }) => {
      await mockSessionFor(page);
      const m = await mockCalls(page);
      m.phase = "connected";
      await page.goto("/calls");
      const dialog = await openDialer(page);
      await dialog.getByLabel("Number to call").fill("1002");
      await dialog.getByRole("button", { name: "Call" }).click();

      const bar = page.getByRole("region", { name: "Current call" });
      await expect(bar.getByText("In call with 1002")).toBeVisible();
      await expect(bar.getByRole("button", { name: "Hang up call with 1002" })).toBeVisible();
      await bar.getByRole("button", { name: "Hang up call with 1002" }).click();
      await expect(bar.getByText("Call ended — ended from the console")).toBeVisible();

      // The history row for a live call also has a direct Hang up.
      m.phase = "ringing";
      await page.route("**/atsapbx.v1.TelephonyService/ListCalls", (route) =>
        route.fulfill({ contentType: "application/json", body: JSON.stringify({ calls: [{ callId: "row", direction: "INTERNAL", state: "Presenting", sourceNumber: "1001", destNumber: "1009", startedAt: new Date().toISOString() }] }) }),
      );
      await page.reload();
      const table = page.getByRole("table", { name: "Calls of this tenant" });
      await expect(table.getByRole("button", { name: "Hang up call with 1009" })).toBeVisible();
    });
  });
}
