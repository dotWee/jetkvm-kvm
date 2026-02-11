import { test, expect, type Page } from "@playwright/test";

import { openAccessSettings } from "./helpers";

async function getRdpToggle(page: Page) {
  const item = page.locator("label").filter({ hasText: /RDP Server/i }).first();
  await expect(item).toBeVisible({ timeout: 15000 });
  return item.locator('input[type="checkbox"]').first();
}

async function expectRdpStatus(page: Page, expected: "Running" | "Stopped") {
  const statusItem = page.locator("label").filter({ hasText: /RDP Status/i }).first();
  await expect(statusItem).toBeVisible({ timeout: 10000 });
  await expect(statusItem).toContainText(expected);
}

test.describe("RDP Settings", () => {
  test("access page shows RDP experimental section and persists toggle", async ({ page }) => {
    await openAccessSettings(page);

    await expect(page.getByText("RDP Server", { exact: true })).toBeVisible();
    await expect(page.getByText("Experimental", { exact: true }).first()).toBeVisible();

    const toggle = await getRdpToggle(page);
    const initialEnabled = await toggle.isChecked();

    if (initialEnabled) {
      await toggle.uncheck();
      await expect(page.getByText("RDP settings updated successfully")).toBeVisible({ timeout: 10000 });
      await expectRdpStatus(page, "Stopped");
    } else {
      await toggle.check();
      await expect(page.getByText("RDP settings updated successfully")).toBeVisible({ timeout: 10000 });
      await expectRdpStatus(page, "Running");
    }

    const expectedAfterToggle = !initialEnabled;
    await page.reload();
    await openAccessSettings(page);

    const toggleAfterReload = await getRdpToggle(page);
    await expect(toggleAfterReload).toHaveJSProperty("checked", expectedAfterToggle);

    if (expectedAfterToggle) {
      await expectRdpStatus(page, "Running");
    } else {
      await expectRdpStatus(page, "Stopped");
    }

    // Restore original state.
    if (expectedAfterToggle !== initialEnabled) {
      if (initialEnabled) {
        await toggleAfterReload.check();
      } else {
        await toggleAfterReload.uncheck();
      }
      await expect(page.getByText("RDP settings updated successfully")).toBeVisible({ timeout: 10000 });
    }
  });
});
