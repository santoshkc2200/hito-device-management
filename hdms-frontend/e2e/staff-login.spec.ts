import { expect, test } from "@playwright/test";

test.use({ baseURL: "https://localhost:5175" });

test("a staff member signs in with an employee number, changes a temporary password, and sees their QR", async ({
  page,
}) => {
  const tempPassword = process.env.E2E_TEMP_PASSWORD ?? "temporary-password-123";

  await page.goto("/");
  await page.getByLabel(/employee number/i).fill("E-E2E-1");
  await page.getByLabel(/password/i).fill(tempPassword);
  await page.getByRole("button", { name: /sign in/i }).click();

  // The temporary password forces a change before anything else is reachable.
  await expect(page.getByRole("heading", { name: /choose your own/i })).toBeVisible();
  await page.getByLabel(/current password/i).fill(tempPassword);
  await page.getByLabel(/new password/i).fill("a-much-longer-password");
  await page.getByRole("button", { name: /save/i }).click();

  await expect(page.getByRole("img", { name: /your qr code/i })).toBeVisible();
});

test("the same journey reads correctly in Japanese", async ({ page }) => {
  await page.goto("/?lang=ja");
  await expect(page.getByRole("button", { name: "サインイン" })).toBeVisible();
});
