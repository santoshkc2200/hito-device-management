import { expect, test } from "@playwright/test";

test("admin loads over HTTPS and redirects an unauthenticated visitor to login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/, { timeout: 15_000 });
  await expect(page.getByRole("heading", { name: "Device management" })).toBeVisible();
  await expect(page.getByLabel("Email")).toBeVisible();
});
