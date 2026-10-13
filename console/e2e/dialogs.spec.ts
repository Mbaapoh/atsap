import { expect, test } from "@playwright/test";
import { mockSession } from "./support/session";

// A long form in a dialog must stay usable on a small screen: the dialog
// scrolls within the viewport so its actions are always reachable.

test.use({ viewport: { width: 360, height: 640 } });

test("the trunk form's actions are reachable on a small phone", async ({ page }) => {
  await mockSession(page, ["TENANT_ADMIN"]);
  await page.goto("/trunks");
  await page.getByRole("button", { name: "Add trunk" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a trunk" });
  const box = await dialog.boundingBox();
  expect(box!.height).toBeLessThanOrEqual(640);
  const submit = dialog.getByRole("button", { name: "Add trunk" });
  await submit.scrollIntoViewIfNeeded();
  await expect(submit).toBeInViewport();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
});
