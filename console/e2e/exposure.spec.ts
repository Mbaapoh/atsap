import { expect, test } from "@playwright/test";
import { engineFindings, renderFindings, renderedSurface, secretFindings, type Finding } from "./support/exposure";
import { mockSession } from "./support/session";

// console-foundation task 3.3, hermetic half: the API is mocked to be as
// unhelpful as possible — engine vocabulary in error messages, in a
// call's termination reason, in fields the console has no business
// showing — and every page is rendered and scanned. The console's own
// guards (fixed error copy, view models that drop raw fields) must keep
// all of it off the screen. The real-API half is exposure.rig.spec.ts.

const ENGINE = "e_0123456789abcdef0123456789abcdef";
const PAGES = ["/", "/licence", "/users", "/extensions", "/trunks", "/calls", "/account"];

test("engine detail in API responses and errors never reaches the screen", async ({ page }) => {
  await mockSession(page, ["PLATFORM_ADMIN"]);
  const hostileError = (status: number, code: string) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify({ code, message: `PJSIP/${ENGINE}-0000002a: ps_endpoints row missing (pjsip.conf), channel 1791844407.123 in bridge-4f9c2a1e-77aa` }),
  });

  // Calls whose termination reason and participants carry engine detail.
  await page.route("**/atsapbx.v1.TelephonyService/ListCalls", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        calls: [{
          callId: "c1", direction: "INBOUND", state: "Terminated", sourceNumber: "+237600000001", destNumber: "1001",
          startedAt: "2026-10-12T09:30:00Z", answeredAt: "2026-10-12T09:30:05Z", durationSeconds: 61,
          terminationReason: `hangup by PJSIP/${ENGINE} cause 16 (stasis-in)`,
        }],
      }),
    }),
  );
  // An extension whose SIP username is its engine id (allowed on the wire,
  // never rendered by these pages).
  await page.route("**/atsapbx.v1.PbxService/ListExtensions", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ extensions: [{ extensionId: "x", number: "1001", authUsername: ENGINE }] }) }),
  );
  // Failures whose server messages are full of engine detail.
  await page.route("**/atsapbx.v1.PbxService/ListRoutes", (r) => r.fulfill(hostileError(500, "internal")));
  await page.route("**/atsapbx.v1.IdentityService/ListPrincipals", (r) => r.fulfill(hostileError(403, "permission_denied")));
  await page.route("**/atsapbx.v1.PbxService/ListInboundNumbers", (r) => r.fulfill(hostileError(500, "internal")));
  await page.route("**/atsapbx.v1.PbxService/GetEmergencyProfile", (r) => r.fulfill(hostileError(503, "unavailable")));
  await page.route("**/atsapbx.v1.LicensingService/ApplyLicenseKey", (r) => r.fulfill(hostileError(400, "invalid_argument")));

  const findings: Finding[] = [];
  for (const path of PAGES) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.waitForLoadState("networkidle");
    findings.push(...renderFindings(path, await renderedSurface(page), [], {}));
  }

  // The overview did show the failures — as console copy.
  await page.goto("/");
  await expect(page.locator("li.checklist-item", { hasText: "Connect a trunk and a route" })).toContainText("Could not check");
  await expect(page.locator("li.checklist-item", { hasText: "Add a user" })).toContainText("Your role cannot see this");
  await expect(page.locator("li.checklist-item", { hasText: "Complete a call" })).toContainText("Done");

  // A refused licence key, answered with an engine-laden message.
  await page.goto("/licence");
  await page.getByLabel("Licence key").fill("eyJub3QiOiJyZWFsIn0.a2V5.c2ln");
  await page.getByRole("button", { name: "Apply licence" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply licence" }).click();
  await expect(page.getByLabel("Licence key")).toHaveAccessibleDescription(/did not verify/);
  findings.push(...renderFindings("/licence after a refusal", await renderedSurface(page), [], {}));

  expect(findings).toEqual([]);
});

test("the SIP username is allowed in its marked element, and caught anywhere else", async ({ page }) => {
  await page.setContent(`<main><dl><dt>SIP username</dt><dd><code data-sip-username>${ENGINE}</code></dd></dl></main>`);
  expect(engineFindings("allowed", await renderedSurface(page))).toEqual([]);

  await page.setContent(`<main><p>Endpoint ${ENGINE}</p><code data-sip-username>${ENGINE}</code></main>`);
  expect(engineFindings("outside", await renderedSurface(page)).map((f) => f.what)).toEqual([`engine endpoint id: ${ENGINE}`]);

  // Text nodes are scanned one by one: an id between other words, with no
  // space around it, is still an id.
  await page.setContent(`<main><dt>SIP username</dt><dd><code>${ENGINE}</code><button>Copy</button></dd></main>`);
  expect(engineFindings("adjacent", await renderedSurface(page))).not.toEqual([]);

  await page.setContent(`<main><a title="${ENGINE}" href="#">x</a></main>`);
  expect(engineFindings("attribute", await renderedSurface(page))).not.toEqual([]);
});

// console-admin-screens task 6.3: the credential panel, and fault
// injection proving the scans catch the two leaks they exist for.

const SIP_PASSWORD = "Exposure-sip-password-9";

async function openCredentialPanel(page: import("@playwright/test").Page) {
  await mockSession(page, ["TENANT_ADMIN"]);
  await page.route("**/atsapbx.v1.PbxService/ListExtensions", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ extensions: [{ extensionId: "x1", number: "1001", displayName: "Reception", authUsername: ENGINE }] }) }),
  );
  await page.route("**/atsapbx.v1.PbxService/CreateExtension", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ extension: { extensionId: "x2", number: "1002", displayName: "Desk", authUsername: ENGINE, deviceType: "DEVICE_TYPE_SIP" }, secret: SIP_PASSWORD }) }),
  );
  await page.route("**/atsapbx.v1.PbxService/GetSipRegistrar", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ configured: true, host: "sip.example.test", port: 5060, transport: "udp" }) }),
  );
  await page.goto("/extensions");
  await page.getByRole("button", { name: "Add extension" }).click();
  const dialog = page.getByRole("dialog", { name: "Add an extension" });
  await dialog.getByLabel("Number").fill("1002");
  await dialog.getByLabel("Name").fill("Desk");
  await dialog.getByRole("button", { name: "Add extension" }).click();
  const panel = page.getByRole("dialog", { name: "Extension 1002 created" });
  await expect(panel).toContainText(SIP_PASSWORD);
  return panel;
}

test("the credential panel shows the SIP username only in its marked element; once dismissed, nothing remains", async ({ page }) => {
  const panel = await openCredentialPanel(page);
  await expect(panel.locator("[data-sip-username]")).toHaveText(ENGINE);
  expect(engineFindings("credential panel", await renderedSurface(page))).toEqual([]);
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  const surface = await renderedSurface(page);
  expect([...engineFindings("/extensions after", surface), ...secretFindings("/extensions after", surface, { "SIP password": SIP_PASSWORD })]).toEqual([]);
});

test("fault injection: a SIP username outside its marked element is caught", async ({ page }) => {
  await openCredentialPanel(page);
  // The fault: the username rendered without its marker, as a regression would.
  await page.locator("[data-sip-username]").evaluate((el) => el.removeAttribute("data-sip-username"));
  expect(engineFindings("credential panel", await renderedSurface(page)).map((f) => f.what)).toEqual([`engine endpoint id: ${ENGINE}`]);
});

test("fault injection: a secret left in a list is caught", async ({ page }) => {
  const panel = await openCredentialPanel(page);
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  const list = page.getByRole("table", { name: "Extensions of this tenant" });
  await expect(list).toBeVisible();
  expect(secretFindings("/extensions", await renderedSurface(page), { "SIP password": SIP_PASSWORD })).toEqual([]);
  // The fault: the one-time password left behind in a list cell.
  await list.locator("tbody td").first().evaluate((td, secret) => td.append(` ${secret}`), SIP_PASSWORD);
  expect(secretFindings("/extensions", await renderedSurface(page), { "SIP password": SIP_PASSWORD })).toEqual([{ where: "/extensions", what: "SIP password present" }]);
  // Hidden is not safe either: the scan reads hidden text and attributes.
  await list.locator("tbody td").first().evaluate((td) => td.replaceChildren());
  await list.evaluate((t, secret) => t.setAttribute("data-debug", secret), SIP_PASSWORD);
  expect(secretFindings("/extensions", await renderedSurface(page), { "SIP password": SIP_PASSWORD })).toHaveLength(1);
});
