import { expect, test, type Page } from "@playwright/test";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-admin-screens: HLD-18 A2 (users, roles, permissions) against the
// real API on the dev rig.

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

export async function signIn(page: Page, email: string, password = FIXTURE_PASSWORD) {
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
}

test("A2: the users list shows this tenant's real members and roles", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email);
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Users", exact: true }).click();
  const table = page.getByRole("table", { name: "Users of this tenant" });
  const row = table.getByRole("row", { name: new RegExp(user.email) });
  await expect(row).toContainText("Tenant administrator");
  await expect(row).toContainText("Active");
  await expect(table.getByRole("row")).toHaveCount(2); // header + the one member
});
