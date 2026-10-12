import { expect, test } from "@playwright/test";
import { mockSession } from "./support/session";

test.beforeEach(async ({ page }) => {
  await mockSession(page);
});

// console-foundation task 1.2: keyboard-only use of the shell in a real
// browser, at desktop and phone widths.

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Tab walks skip link, then the navigation in order, with a visible focus ring", async ({ page }) => {
    await page.goto("/");
    // The session restores before the shell renders.
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const expected = ["Skip to content", "Overview", "Users", "Extensions", "Trunks & routes", "Call history", "Account"];
    for (const name of expected) {
      await page.keyboard.press("Tab");
      const focused = page.locator(":focus");
      await expect(focused).toHaveText(name);
      const outline = await focused.evaluate((el) => getComputedStyle(el).outlineStyle);
      expect(outline, `${name} shows a focus ring`).not.toBe("none");
    }
    await expect(page.getByRole("button", { name: "Open navigation" })).toBeHidden();
  });

  test("Skip to content moves past the navigation", async ({ page }) => {
    await page.goto("/users");
    // The session restores before the shell renders.
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Enter");
    await page.keyboard.press("Tab");
    await expect(page.locator(":focus")).not.toHaveText("Overview");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("the drawer opens from the keyboard, traps focus, makes the page inert, and closes on Escape", async ({ page }) => {
    await page.goto("/");
    // The session restores before the shell renders.
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Main" })).toBeHidden();
    const menu = page.getByRole("button", { name: "Open navigation" });
    await menu.focus();
    await page.keyboard.press("Enter");

    const drawer = page.getByRole("dialog", { name: "AtsaPBX" });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole("button", { name: "Close navigation" })).toBeFocused();
    expect(await page.evaluate(() => document.getElementById("root")?.inert)).toBe(true);

    for (let i = 0; i < 10; i++) {
      await page.keyboard.press("Tab");
      const inside = await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'));
      expect(inside, "focus stays in the drawer").toBe(true);
    }

    await page.keyboard.press("Escape");
    await expect(drawer).toBeHidden();
    await expect(menu).toBeFocused();
    expect(await page.evaluate(() => document.getElementById("root")?.inert)).toBe(false);
  });

  test("choosing a page in the drawer lands on its heading", async ({ page }) => {
    await page.goto("/");
    // The session restores before the shell renders.
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "Open navigation" }).click();
    await page.getByRole("dialog").getByRole("link", { name: "Extensions" }).click();
    await expect(page.getByRole("dialog")).toBeHidden();
    await expect(page.getByRole("heading", { level: 1, name: "Extensions" })).toBeFocused();
  });

  test("nothing scrolls the page sideways", async ({ page }) => {
    await page.goto("/calls");
    // The session restores before the shell renders.
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
});
