import { expect, test, type Page } from "@playwright/test";
import { mockSession } from "./support/session";

// console-click-to-call: Place a call and Hang up on Call history, in a
// real browser against a stateful mock of the public API.

const REGISTERED = { extensionId: "e-1001", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_REGISTERED" };
const UNREGISTERED = { extensionId: "e-1002", number: "1002", displayName: "Lab", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_NOT_REGISTERED" };
const ENDED = { callId: "old", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+44207", startedAt: "2026-10-12T10:00:00Z", durationSeconds: 30, terminationReason: "normal clearing" };

interface MockedCall {
  callId: string;
  direction: string;
  state: string;
  sourceNumber: string;
  destNumber: string;
  startedAt: string;
  durationSeconds?: number;
  terminationReason?: string;
}

async function mockCalls(page: Page) {
  const state = { calls: [{ ...ENDED }] as MockedCall[], placed: [] as Record<string, unknown>[], hung: [] as Record<string, unknown>[] };
  await page.route("**/atsapbx.v1.PbxService/ListExtensions", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ extensions: [REGISTERED, UNREGISTERED] }) }));
  await page.route("**/atsapbx.v1.TelephonyService/ListCalls", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ calls: state.calls }) }));
  await page.route("**/atsapbx.v1.PbxService/PlaceCall", (route) => {
    const body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
    state.placed.push(body);
    const call: MockedCall = {
      callId: "new",
      direction: "INTERNAL",
      state: "Presenting",
      sourceNumber: "1001",
      destNumber: String(body.destination),
      startedAt: new Date().toISOString(),
    };
    state.calls = [call, ...state.calls];
    return route.fulfill({ contentType: "application/json", body: JSON.stringify({ callId: "new" }) });
  });
  await page.route("**/atsapbx.v1.PbxService/HangupCall", (route) => {
    const body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
    state.hung.push(body);
    state.calls = state.calls.map((c) => (c.callId === body.callId ? { ...c, state: "Terminated", terminationReason: "api_hangup" } : c));
    return route.fulfill({ contentType: "application/json", body: "{}" });
  });
  return state;
}

for (const viewport of [
  { label: "desktop", width: 1280, height: 800 },
  { label: "phone", width: 390, height: 844 },
]) {
  test.describe(viewport.label, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("place a call from a registered extension, then hang it up from its row", async ({ page }) => {
      await mockSession(page, ["TENANT_ADMIN"]);
      const state = await mockCalls(page);
      await page.goto("/calls");

      const table = page.getByRole("table", { name: "Calls of this tenant" });
      await expect(table.getByRole("row", { name: /\+44207/ })).toContainText("Terminated");

      // The dialog names the registered extension and disables the other.
      await page.getByRole("button", { name: "Place a call" }).click();
      const dialog = page.getByRole("dialog", { name: "Place a call" });
      const from = dialog.getByLabel("From extension");
      await expect(from.locator("option")).toHaveText(["1001 · Reception", "1002 · Lab — No device registered"]);
      await expect(from.locator("option").nth(1)).toBeDisabled();
      const overflow = await dialog.evaluate((d) => d.scrollWidth - d.clientWidth);
      expect(overflow).toBe(0);

      await dialog.getByLabel("Number to call").fill("+237222000111");
      await dialog.getByRole("button", { name: "Call" }).click();

      await expect(page.getByText("Calling +237222000111 — answer extension 1001 to connect.")).toBeVisible();
      await expect(dialog).toHaveCount(0);
      expect(state.placed).toEqual([{ tenantId: "11111111-1111-4111-8111-111111111111", extensionId: "e-1001", destination: "+237222000111" }]);

      // The new call is at the top of the list, ringing.
      const placed = table.getByRole("row").nth(1);
      await expect(placed).toContainText("+237222000111");
      await expect(placed).toContainText("Presenting");

      await placed.getByRole("button", { name: "Actions for call to +237222000111" }).click();
      await page.getByRole("menuitem", { name: "Hang up…" }).click();
      await page.getByRole("dialog", { name: "Hang up the call with +237222000111?" }).getByRole("button", { name: "Hang up" }).click();

      await expect(page.getByText("Call ended.")).toBeVisible();
      expect(state.hung).toEqual([{ tenantId: "11111111-1111-4111-8111-111111111111", callId: "new" }]);
      await expect(table.getByRole("row").nth(1)).toContainText("Terminated");
    });
  });
}
