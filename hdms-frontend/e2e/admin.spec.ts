import { expect, test } from "@playwright/test";

test("admin loads over HTTPS and renders", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "HDMS Admin" })).toBeVisible();
  await expect(page.getByText(/API (status|unreachable)/)).toBeVisible({ timeout: 15_000 });
});
