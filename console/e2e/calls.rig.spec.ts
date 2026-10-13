import { expect, test } from "@playwright/test";
import { rigAvailable, seedCalls, seedUser, signIn, type RigUser } from "./support/rig";

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
test.afterEach(() => {
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
