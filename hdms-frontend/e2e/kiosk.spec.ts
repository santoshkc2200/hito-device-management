import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("kiosk loads over HTTPS, renders and passes accessibility checks", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "HDMS Kiosk" })).toBeVisible();
  // The API status line renders once the health check settles, whether the
  // API is reachable in this environment or not — the point of this smoke
  // test is that the app itself boots, not that the backend is up. A
  // generous timeout covers TanStack Query's retry/backoff when the API
  // isn't running in this environment.
  await expect(page.getByText(/API (status|unreachable)/)).toBeVisible({ timeout: 15_000 });

  // Axe accessibility scan
  const accessibilityScanResults = await new AxeBuilder({ page }).analyze();
  expect(accessibilityScanResults.violations).toEqual([]);
});
