import { expect, test } from "@playwright/test";
import { renderFindings, renderedSurface, type Finding } from "./support/exposure";
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
