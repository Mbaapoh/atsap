import { expect, test, type APIRequestContext } from "@playwright/test";
import { engineFindings, recordTraffic, renderFindings, renderedSurface, trafficFindings, type Finding } from "./support/exposure";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-foundation task 3.3 against the real API on the dev rig: with
// engine-backed configuration in place (an extension, a trunk and a route,
// all projected into Asterisk), a platform administrator walks every page
// of the console. Every API exchange and every rendered page is scanned:
// no credential travels or appears out of place, and no Asterisk
// identifier or vocabulary reaches the wire or the screen — except an
// extension's SIP username, in the one API field that carries it.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
// teardown removes, in order, what the test created through the API (a
// route before its trunk), then the seeded fixture.
let teardown: (() => Promise<void>) | undefined;
test.afterEach(async () => {
  await teardown?.();
  teardown = undefined;
  user?.cleanup();
  user = undefined;
});

const PAGES = ["/", "/licence", "/users", "/extensions", "/trunks", "/calls", "/account"];

async function call(request: APIRequestContext, token: string, procedure: string, data: unknown) {
  const res = await request.post(`/atsapbx.v1.${procedure}`, { data, headers: { Authorization: `Bearer ${token}` } });
  expect(res.status(), `${procedure}: ${await res.text()}`).toBe(200);
  return res.json() as Promise<Record<string, Record<string, string>>>;
}

test("the scanner recognises what it must reject", () => {
  // A scanner that matches nothing would pass every page; prove each
  // pattern fires on a realistic sample.
  for (const sample of [
    "e_0123456789abcdef0123456789abcdef",
    "PJSIP/e_0123456789abcdef0123456789abcdef-00000001",
    "1791844407.123",
    "bridge-4f9c2a1e-77aa",
    "ps_endpoints",
    "pjsip.conf",
    "stasis-in",
  ]) {
    expect(engineFindings("sample", sample), sample).not.toEqual([]);
  }
  expect(engineFindings("sample", '{"authUsername":"e_0123456789abcdef0123456789abcdef"}')).toEqual([]);
});

test("no credential or engine identifier crosses the wire or reaches the screen", async ({ page, request, context }) => {
  user = seedUser(["Acme"], { platformAdmin: true });
  const tenantId = user.tenants[0]!.id;

  // Engine-backed configuration, through the public API.
  const signIn = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } });
  const token = ((await signIn.json()) as { access: { token: string } }).access.token;
  const created: { extensionId?: string; trunkId?: string; routeId?: string } = {};
  teardown = async () => {
    if (created.routeId) await call(request, token, "PbxService/DeleteRoute", { tenantId, routeId: created.routeId });
    if (created.trunkId) await call(request, token, "PbxService/DeleteTrunk", { tenantId, trunkId: created.trunkId });
    if (created.extensionId) await call(request, token, "PbxService/DeleteExtension", { tenantId, extensionId: created.extensionId });
  };
  const ext = (await call(request, token, "PbxService/CreateExtension", {
    tenantId, number: `78${String(Date.now()).slice(-4)}`, displayName: "Exposure e2e", deviceType: "DEVICE_TYPE_SIP",
  })).extension!;
  created.extensionId = ext.extensionId;
  const trunk = (await call(request, token, "PbxService/CreateTrunk", {
    tenantId, name: "Exposure carrier", host: "198.51.100.7", channelLimit: 4, priority: 1,
  })).trunk!;
  created.trunkId = trunk.trunkId;
  const route = (await call(request, token, "PbxService/CreateRoute", {
    tenantId, prefixPattern: "+237", trunkId: trunk.trunkId, costPerMinuteMicros: 1000, priority: 1,
  })).route!;
  created.routeId = route.routeId;

  // Now the person, in a real browser, with every exchange recorded.
  const exchanges = recordTraffic(page);
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.locator("li.checklist-item", { hasText: "Add an extension" })).toContainText("Done");
  const refreshSecret = (await context.cookies()).find((c) => c.name === "atsa_refresh")?.value;
  expect(refreshSecret, "the session cookie exists").toBeTruthy();

  const bogusKey = "eyJleHBvc3VyZSI6InRlc3QifQ.ZXhwb3N1cmUta2V5.c2ln";
  const creds = { password: FIXTURE_PASSWORD, licenceKey: bogusKey, refreshSecret: refreshSecret! };
  const findings: Finding[] = [];

  for (const path of PAGES) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.waitForLoadState("networkidle");
    findings.push(...renderFindings(path, await renderedSurface(page), exchanges, creds));
  }

  // A licence key that does not verify: refused, and the key is neither
  // echoed by the server nor left on the page.
  await page.goto("/licence");
  await page.getByLabel("Licence key").fill(bogusKey);
  await page.getByRole("button", { name: "Apply licence" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply licence" }).click();
  await expect(page.getByLabel("Licence key")).toHaveAccessibleDescription(/did not verify/);
  findings.push(...renderFindings("/licence after a refused key", await renderedSurface(page), exchanges, creds));

  findings.push(...trafficFindings(exchanges, creds));
  expect(findings).toEqual([]);

  // The scan was not vacuous: the engine-backed data did cross the wire.
  const procedures = new Set(exchanges.map((e) => e.procedure));
  for (const p of ["PbxService/ListExtensions", "PbxService/ListTrunks", "PbxService/ListRoutes", "LicensingService/ApplyLicenseKey"]) {
    expect(procedures, p).toContain(`atsapbx.v1.${p}`);
  }
  expect(exchanges.some((e) => e.responseBody.includes(ext.authUsername!)), "the SIP username was on the wire, in its field").toBe(true);
  expect(exchanges.find((e) => e.procedure.endsWith("ApplyLicenseKey"))?.requestBody).toContain(bogusKey);
});
