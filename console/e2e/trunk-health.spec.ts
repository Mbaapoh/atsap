import { expect, test } from "@playwright/test";
import { mockSession } from "./support/session";

// pbx-trunk-health-probing task 3.1, in a real browser with the API
// mocked: the health column says what was observed, why and when, and
// fits a phone.
const observed = new Date();
const trunks = [
  { trunkId: "u", name: "Brand new", host: "sip.new.example", channelLimit: 2, health: "TRUNK_HEALTH_UNKNOWN" },
  { trunkId: "t", name: "Typo", host: "locslhodt", channelLimit: 2, health: "TRUNK_HEALTH_UNHEALTHY", healthReason: "TRUNK_HEALTH_REASON_HOST_NOT_FOUND", healthObservedAt: observed.toISOString() },
];

for (const width of [390, 1280]) {
  test(`trunk health reads as observed, with why and when, at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockSession(page, ["TENANT_ADMIN"]);
    await page.route("**/atsapbx.v1.PbxService/ListTrunks", (r) => r.fulfill({ contentType: "application/json", body: JSON.stringify({ trunks }) }));
    await page.goto("/trunks");

    const table = page.getByRole("table", { name: "Trunks of this tenant" });
    await expect(table.getByRole("row", { name: /Brand new/ })).toContainText("Not checked yet");
    await expect(table.getByRole("row", { name: /Brand new/ })).not.toContainText(/checked \d/);
    await expect(table.getByRole("row", { name: /Typo/ })).toContainText(/Unhealthy\s*Host not found · checked \d/);

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
  });
}

// Every admin table with row actions fits a phone: the page never scrolls
// sideways, only the table inside its own box. (Found with the health
// column: the visually-hidden "Actions" header escaped the scroll box.)
const tables: { path: string; procedure: string; body: object; name: string }[] = [
  { path: "/trunks", procedure: "PbxService/ListTrunks", body: { trunks }, name: "Trunks of this tenant" },
  { path: "/extensions", procedure: "PbxService/ListExtensions", body: { extensions: [{ extensionId: "x", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP" }] }, name: "Extensions of this tenant" },
  {
    path: "/users",
    procedure: "IdentityService/ListPrincipals",
    body: { principals: [{ principal: { principalId: "p", email: "ana@example.com", displayName: "Ana Mbe", status: "PRINCIPAL_STATUS_ACTIVE" }, roleBindings: [{ role: "SUPERVISOR", scope: "tenant" }] }] },
    name: "Users of this tenant",
  },
];
for (const t of tables) {
  test(`${t.path} does not scroll sideways on a phone`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await mockSession(page, ["TENANT_ADMIN"]);
    await page.route(`**/atsapbx.v1.${t.procedure}`, (r) => r.fulfill({ contentType: "application/json", body: JSON.stringify(t.body) }));
    await page.goto(t.path);
    await expect(page.getByRole("table", { name: t.name })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
  });
}
