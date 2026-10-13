import { expect, test, type Page } from "@playwright/test";
import { FIXTURE_PASSWORD, forgetUser, rigAvailable, seedUser, type RigUser } from "./support/rig";

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
const created: string[] = [];
test.afterEach(() => {
  for (const email of created.splice(0)) forgetUser(email);
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

test("A2: an administrator adds a person, who can then sign in", async ({ page, browser }) => {
  user = seedUser(["Acme"]);
  const email = `console-e2e-added-${Date.now()}@example.test`;
  const password = "added person passphrase";
  created.push(email);

  await signIn(page, user.email);
  await page.goto("/users");
  await page.getByRole("button", { name: "Add user" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a user" });
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Name").fill("Added Person");
  await dialog.getByLabel("Role").selectOption("SUPERVISOR");
  await dialog.getByLabel("Initial password (optional)").fill(password);
  await dialog.getByRole("button", { name: "Add user" }).click();
  await expect(page.getByText("Added Person added to this tenant.")).toBeVisible();
  const row = page.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: new RegExp(email) });
  await expect(row).toContainText("Supervisor");
  expect(await page.content()).not.toContain(password);

  // The new person signs in with the initial password.
  const theirs = await browser.newContext();
  const them = await theirs.newPage();
  await signIn(them, email, password);
  await expect(them.locator(".tenant-indicator")).toContainText(user.tenants[0]!.name);
  await theirs.close();
});

test("A2: grant and revoke a role; the last administrator cannot be removed", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email);
  await page.goto("/users");
  const actions = page.getByRole("button", { name: /^Actions for/ });

  // Grant Agent to the administrator, then revoke it again.
  await actions.click();
  await page.getByRole("menuitem", { name: "Grant a role…" }).click();
  await page.getByRole("dialog").getByLabel("Role").selectOption("AGENT");
  await page.getByRole("dialog").getByRole("button", { name: "Grant role" }).click();
  const row = page.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: new RegExp(user.email) });
  await expect(row).toContainText("Agent");
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke Agent" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke role" }).click();
  await expect(page.getByText("Agent revoked from")).toBeVisible();
  await expect(row).not.toContainText("Agent");

  // The server refuses to remove the tenant's last administrator.
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke Tenant administrator" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke role" }).click();
  await expect(page.getByRole("dialog")).toContainText("is this tenant's last administrator. Make someone else a tenant administrator first.");
  await page.getByRole("dialog").getByRole("button", { name: "Cancel" }).click();
  await expect(row).toContainText("Tenant administrator");
});
