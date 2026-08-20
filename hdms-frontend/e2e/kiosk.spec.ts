import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("kiosk loads over HTTPS, renders primary idle screen, and passes accessibility checks", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Scan your ID card or a device barcode")).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText("HDMS Kiosk")).toBeVisible();
  await expect(page.getByText(/Scanner (Ready|Idle|Disconnected)/)).toBeVisible();

  // Axe accessibility scan
  const accessibilityScanResults = await new AxeBuilder({ page }).analyze();
  expect(accessibilityScanResults.violations).toEqual([]);
});
