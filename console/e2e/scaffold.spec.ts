import { expect, test } from "@playwright/test";
import { mockSession } from "./support/session";

// Scaffold smoke test: the built shell loads in a real browser, a direct
// visit to a deep route works (history fallback), and keyboard focus is
// visible. The A1–A5 journeys arrive with their screens.
test("a direct visit to a route renders it", async ({ page }) => {
  await mockSession(page);
  await page.goto("/calls");
  // The session restores before the shell renders.
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Call history" })).toBeVisible();
  await expect(page).toHaveTitle("AtsaPBX");
});

test("sign-in renders outside the shell", async ({ page }) => {
  // No session to restore: the refresh cookie is absent or refused.
  await page.route("**/atsapbx.v1.AuthService/RefreshSession", (route) =>
    route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ code: "unauthenticated", message: "x" }) }),
  );
  await page.goto("/sign-in");
  await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
});

test("keyboard users reach the navigation and land on the heading", async ({ page }) => {
  await mockSession(page);
  await page.goto("/");
  // The session restores before the shell renders.
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Skip to content" })).toBeFocused();
  await page.getByRole("link", { name: "Users" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: "Users" })).toBeFocused();
});

test("the dev server forwards the public API to the backend", async ({ request }) => {
  const res = await request.post("/atsapbx.v1.AuthService/DiscoverSignIn", {
    data: { email: "nobody@example.com" },
    headers: { "Content-Type": "application/json" },
  });
  test.skip(res.status() === 502 || res.status() === 504, "no local API running on :8080");
  expect(res.status()).toBe(200);
  expect(await res.json()).toEqual({ method: "SIGN_IN_METHOD_PASSWORD" });
});
