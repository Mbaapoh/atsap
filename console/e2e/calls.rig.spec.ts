import { expect, test, type APIRequestContext } from "@playwright/test";
import { FIXTURE_PASSWORD, forgetContact, rigAvailable, seedCalls, seedContact, seedUser, signIn, type RigUser } from "./support/rig";

// console-admin-screens: HLD-18 A5 (call history) against the real API on
// the dev rig. Call records are seeded as the telephony core stores them;
// a placed call reaching the history is the UAT's job (task 6.2).

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
let teardown: (() => Promise<void>) | undefined;
test.afterEach(async () => {
  await teardown?.();
  teardown = undefined;
  user?.cleanup();
  user = undefined;
});

test("A5: filters are sent together, kept in the address across reload and Back, and states are verbatim", async ({ page }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  seedCalls(tenantId, [
    { direction: "INBOUND", state: "Terminated", from: "+237600000001", to: "1001", startedAt: "2026-10-05T09:00:00Z", answeredAt: "2026-10-05T09:00:04Z", endedAt: "2026-10-05T09:01:05Z", reason: "normal clearing" },
    { direction: "OUTBOUND", state: "Terminated", from: "1001", to: "+442071234567", startedAt: "2026-10-06T10:00:00Z", endedAt: "2026-10-06T10:00:01Z", reason: "capacity verdict rejected the call" },
    { direction: "INBOUND", state: "Terminated", from: "+237600000002", to: "1002", startedAt: "2026-09-20T08:00:00Z", endedAt: "2026-09-20T08:00:30Z", reason: "call setup did not complete" },
    { direction: "INTERNAL", state: "Presenting", from: "1001", to: "1002", startedAt: "2026-10-06T11:00:00Z" },
  ]);

  await signIn(page, user.email);
  await page.goto("/calls");
  const table = page.getByRole("table", { name: "Calls of this tenant" });
  await expect(table.getByRole("row")).toHaveCount(5);
  await expect(table.getByRole("row", { name: /\+442071234567/ })).toContainText("capacity verdict rejected the call");
  await expect(table.getByRole("row", { name: /Internal/ })).toContainText("Presenting");

  const form = page.getByRole("form", { name: "Call filters" });
  await form.getByLabel("From").fill("2026-10-01");
  await form.getByLabel("To").fill("2026-10-31");
  await form.getByLabel("Direction").selectOption("INBOUND");
  await form.getByLabel("State").selectOption("Terminated");
  const sent = page.waitForRequest((r) => r.url().includes("atsapbx.v1.TelephonyService/ListCalls") && (r.postData() ?? "").includes("Terminated"));
  await form.getByRole("button", { name: "Apply filters" }).click();
  expect(JSON.parse((await sent).postData() ?? "{}")).toMatchObject({ direction: "INBOUND", state: "Terminated", startedFrom: expect.any(String), startedTo: expect.any(String) });
  await expect(page).toHaveURL(/\/calls\?from=2026-10-01&to=2026-10-31&direction=INBOUND&state=Terminated$/);
  await expect(table.getByRole("row")).toHaveCount(2);
  const only = table.getByRole("row", { name: /\+237600000001/ });
  for (const text of ["Inbound", "Terminated", "1:01", "normal clearing"]) await expect(only).toContainText(text);

  await page.reload();
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(form.getByLabel("Direction")).toHaveValue("INBOUND");
  await expect(form.getByLabel("From")).toHaveValue("2026-10-01");

  await form.getByLabel("State").selectOption("Presenting");
  await form.getByLabel("Direction").selectOption("");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect(table.getByRole("row", { name: /Internal/ })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/direction=INBOUND&state=Terminated$/);
  await expect(table.getByRole("row")).toHaveCount(2);

  const text = (await page.locator("main").textContent()) ?? "";
  expect(text).not.toMatch(/\bAnswered\b|\bMissed\b|\bFailed\b/);
});

// console-click-to-call task 2.1: place a real call through the console
// (the caller's extension is registered; the callee is never rung while
// the caller is still ringing) and end it from the row.
async function apiToken(request: APIRequestContext, email: string): Promise<string> {
  const res = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email, password: FIXTURE_PASSWORD } });
  return ((await res.json()) as { access: { token: string } }).access.token;
}

test("A5: place a call from the console, see it ringing, and cancel it", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = await apiToken(request, user.email);
  const create = async (number: string, name: string) => {
    const res = await request.post("/atsapbx.v1.PbxService/CreateExtension", {
      data: { tenantId, number, displayName: name, deviceType: "DEVICE_TYPE_SIP" },
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res.status()).toBe(200);
    return ((await res.json()) as { extension: { extensionId: string } }).extension.extensionId;
  };
  const callerNumber = `78${String(Date.now()).slice(-4)}`;
  const callerId = await create(callerNumber, "Console caller");
  const calleeNumber = `79${String(Date.now()).slice(-4)}`;
  const calleeId = await create(calleeNumber, "Console callee");
  seedContact(callerId);
  teardown = async () => {
    forgetContact(callerId);
    for (const extensionId of [callerId, calleeId]) {
      await request.post("/atsapbx.v1.PbxService/DeleteExtension", { data: { tenantId, extensionId }, headers: { Authorization: `Bearer ${token}` } });
    }
  };

  await signIn(page, user.email);
  await page.goto("/calls");
  await page.getByRole("button", { name: "Place a call" }).click();
  const dialog = page.getByRole("dialog", { name: "New call" });
  await dialog.getByLabel("From extension").selectOption(callerId);
  await dialog.getByLabel("Number to call").fill(calleeNumber);
  await dialog.getByRole("button", { name: "Call" }).click();

  const bar = page.getByRole("region", { name: "Current call" });
  await expect(bar.getByText(`Ringing your extension ${callerNumber}`)).toBeVisible();
  await expect(bar.getByText(`Answer your phone to continue. ${calleeNumber} is not dialled yet.`)).toBeVisible();

  const table = page.getByRole("table", { name: "Calls of this tenant" });
  const row = table.getByRole("row").nth(1);
  await expect(row).toContainText(calleeNumber);
  await expect(row).toContainText("Presenting");

  // Cancel before answering: the destination is never dialled.
  await bar.getByRole("button", { name: `Cancel call to ${calleeNumber}` }).click();
  await expect(bar.getByText("Call ended — ended from the console")).toBeVisible();
  await expect(row).toContainText("Terminated", { timeout: 5000 });
});
