import { expect, test } from "@playwright/test";
import { FIXTURE_PASSWORD, rigAvailable, rigLicenceKey, seedUser, type RigUser } from "../support/rig";

// UAT — HLD-18 Flow A1, "First run: install to first login", from the
// console onward, on the dev rig as a person does it: open the console,
// sign in as the platform administrator, read the licence, apply the
// installation's key, and see the console confirm it.
//
// Bootstrap itself is the CLI (`atsap-api bootstrap`), proven by the
// identity e2e suite; the rig already has tenants, so the administrator is
// seeded with exactly the membership bootstrap creates (PLATFORM_ADMIN,
// system scope). The key is the rig's own (make dev-licence), so applying
// it leaves the rig as it was.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let admin: RigUser | undefined;
test.afterEach(() => {
  admin?.cleanup();
  admin = undefined;
});

test("A1: the platform administrator signs in, applies the licence key, and the console confirms it", async ({ page }) => {
  admin = seedUser(["First tenant"], { platformAdmin: true });
  const key = rigLicenceKey();

  // Open the console: not signed in, so the sign-in page.
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();

  // Email, then the method the server says (a password), then sign in.
  await page.getByLabel("Email").fill(admin.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();

  // One tenant: straight into the console, which names it and shows the
  // licence in the top bar.
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.locator(".tenant-indicator")).toContainText(admin.tenants[0]!.name);
  const pill = page.getByRole("link", { name: /^Licence:/ });
  await expect(pill).toBeVisible();

  // Read the licence and apply the installation's key.
  await pill.click();
  await expect(page.getByRole("heading", { level: 1, name: "Licence" })).toBeVisible();
  await page.getByLabel("Licence key").fill(key);
  await expect(page.getByLabel("Licence key")).toHaveAttribute("type", "password");
  await page.getByRole("button", { name: "Apply licence" }).click();
  const dialog = page.getByRole("dialog", { name: "Apply this licence?" });
  await expect(dialog).toContainText("Calls in progress are not interrupted.");
  await dialog.getByRole("button", { name: "Apply licence" }).click();

  // The console confirms, shows the licence now in force, and keeps no
  // trace of the key.
  await expect(page.getByText("Licence applied.")).toBeVisible();
  await expect(page.locator(".licence-condition")).toHaveText("Valid");
  await expect(page.locator("dl.facts")).toContainText("of 16");
  await expect(pill).toHaveAccessibleName(/Valid · 16 channels/);
  await expect(page.getByLabel("Licence key")).toHaveValue("");
  expect(await page.content()).not.toContain(key);
  const stored = await page.evaluate(() => localStorage.length + sessionStorage.length);
  expect(stored).toBe(0);

  // The console is unlocked: telephony configuration is reachable.
  await page.getByRole("link", { name: "Extensions" }).first().click();
  await expect(page.getByRole("heading", { level: 1, name: "Extensions" })).toBeVisible();
});
