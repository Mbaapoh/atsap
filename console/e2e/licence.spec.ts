import { expect, test } from "@playwright/test";
import { mockLicence, mockSession } from "./support/session";

// console-foundation task 2.3 in a real browser, API mocked: the Setup
// gate, the licence page at phone width, and applying a key by keyboard
// alone with the key masked throughout.

const KEY = "eyJhbGciOiJFZERTQSJ9.ZTJlLWxpY2VuY2Uta2V5.c2ln";
const setup = { state: "LICENSE_STATE_NOT_ACTIVATED", condition: "LICENSE_CONDITION_SETUP", tier: "LICENSE_TIER_UNSPECIFIED", capacity: "CAPACITY_LEVEL_UNSPECIFIED", channels: 0, edition: "" };

test("in Setup, telephony pages lead to the licence page", async ({ page }) => {
  await mockSession(page);
  await mockLicence(page, setup);
  await page.goto("/extensions");
  await expect(page).toHaveURL(/\/licence$/);
  await expect(page.getByText("Extensions open once a licence is in force")).toBeVisible();
  await expect(page.getByRole("link", { name: /Licence: Setup/ })).toBeVisible();
});

test("a platform administrator applies a key by keyboard, and it is never shown unasked", async ({ page }) => {
  let applied: unknown;
  await mockSession(page, ["PLATFORM_ADMIN"]);
  await mockLicence(page, setup);
  await page.route("**/atsapbx.v1.LicensingService/ApplyLicenseKey", async (route) => {
    applied = route.request().postDataJSON();
    await mockLicence(page);
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ status: { condition: "LICENSE_CONDITION_VALID", state: "LICENSE_STATE_VALID", channels: 16 } }) });
  });

  await page.goto("/licence");
  const input = page.getByLabel("Licence key");
  await input.focus();
  await page.keyboard.insertText(KEY);
  await expect(input).toHaveAttribute("type", "password");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Show" })).toBeFocused();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");

  const dialog = page.getByRole("dialog", { name: "Apply this licence?" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Apply licence" })).toBeFocused();
  await page.keyboard.press("Enter");

  await expect(page.getByText("Licence applied.")).toBeVisible();
  await expect(page.locator(".licence-condition")).toHaveText("Valid");
  expect(applied).toEqual({ token: KEY });
  expect(await page.content()).not.toContain(KEY);
  expect(page.url()).not.toContain(KEY);
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("the licence page fits without sideways scrolling", async ({ page }) => {
    await mockSession(page, ["PLATFORM_ADMIN"]);
    await page.goto("/licence");
    await expect(page.locator(".licence-condition")).toHaveText("Valid");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBe(0);
  });
});
