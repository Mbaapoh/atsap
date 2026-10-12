import { expect, test } from "@playwright/test";
import { PUBLIC_PROCEDURES } from "../src/api/publicApi";

// console-foundation task 1.3: in a real browser, every request the
// console makes on every page stays on its own origin, and every API call
// is a published public procedure — no probe, no private route, no third
// party.
const pages = ["/sign-in", "/", "/licence", "/users", "/extensions", "/trunks", "/calls", "/account", "/no-such-page"];

test("every request stays on the console's origin and inside the public API", async ({ page, baseURL }) => {
  const origin = new URL(baseURL!).origin;
  const seen: string[] = [];
  page.on("request", (req) => seen.push(req.url()));

  for (const path of pages) {
    await page.goto(path);
    await expect(page.locator("h1")).toBeVisible();
  }

  expect(seen.length).toBeGreaterThan(0);
  for (const url of seen) {
    const u = new URL(url);
    expect(u.origin, url).toBe(origin);
    expect(u.pathname, url).not.toMatch(/^\/(healthz|readyz|metrics)\b/);
    if (u.pathname.includes("atsapbx.")) {
      expect(PUBLIC_PROCEDURES.has(u.pathname), url).toBe(true);
    }
  }
});
