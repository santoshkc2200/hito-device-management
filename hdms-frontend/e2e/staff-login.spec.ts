import { expect, test } from "@playwright/test";
import { TestApiClient } from "./helpers/test-api";
import { ja } from "../apps/staff/src/i18n/ja";
import { en } from "../apps/staff/src/i18n/en";

// The staff app renders in the default locale (ja) until someone switches it,
// so every visible string is matched against both catalogues.
const either = (pick: (c: typeof ja) => string) =>
  new RegExp(`${escapeRegExp(pick(ja))}|${escapeRegExp(pick(en))}`);

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

test("a staff member signs in with an employee number, changes a temporary password, and sees their QR", async ({
  page,
}) => {
  const api = new TestApiClient();
  await api.login();
  const user = await api.seedUser();
  const temporaryPassword = await api.resetStaffPassword(user.id);

  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/, { timeout: 15_000 });
  await page.locator("#employeeNo").fill(user.employeeNo);
  await page.locator("#password").fill(temporaryPassword);
  await page.locator('button[type="submit"]').click();

  // The temporary password forces a change before anything else is reachable.
  await expect(
    page.getByRole("heading", { name: either((c) => c.changePassword.forcedTitle) }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(
    page.getByText(either((c) => c.changePassword.forcedExplanation)),
  ).toBeVisible();

  await page.locator("#currentPassword").fill(temporaryPassword);
  await page.locator("#newPassword").fill("a-much-longer-password");
  await page.locator('button[type="submit"]').click();

  await expect(page.getByRole("img", { name: either((c) => c.myQr.alt) })).toBeVisible({
    timeout: 15_000,
  });
});

test("the login screen reads correctly in Japanese", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/, { timeout: 15_000 });
  await expect(page.getByRole("button", { name: ja.login.signIn })).toBeVisible();
});
