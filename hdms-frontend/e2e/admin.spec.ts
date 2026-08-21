import { expect, test } from "@playwright/test";
import { generateTotp, TestApiClient } from "./helpers/test-api";

test("admin loads over HTTPS and redirects an unauthenticated visitor to login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/, { timeout: 15_000 });
  await expect(page.getByRole("heading", { name: "Device management" })).toBeVisible();
  await expect(page.getByLabel("Email")).toBeVisible();
});

test("E8b_RegisterBorrowerWithBlankCard_ThenBorrowAtKiosk", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();

  // 1. Seed blank card, device, and kiosk
  const blankCard = await api.seedUnboundCard();
  const device = await api.seedDevice();
  const kiosk = await api.registerKiosk("E8b Kiosk", "Surgery Floor");

  // 2. Perform admin login
  await page.goto("/login");
  await page.getByLabel("Email").fill("admin@example.org");
  await page.getByLabel("Password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.getByLabel("Authenticator code").fill(totp);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

  // 3. Navigate to Register borrower screen
  await page.goto("/register");
  await expect(page.getByRole("heading", { name: "Register borrower" })).toBeVisible({ timeout: 10_000 });

  // 4. Fill in borrower details
  const empNo = `E8B-${Date.now().toString().slice(-6)}`;
  const fullName = "Dr. Alice Morgan";
  await page.getByLabel("Full name").fill(fullName);
  await page.getByLabel("Employee no.").fill(empNo);

  // 5. Select Option A (Scan a blank card) and enter the blank card token
  await page.getByLabel(/scan a blank card/i).check();
  const tokenInput = page.getByPlaceholder("Scan or enter blank card token...");
  await tokenInput.fill(blankCard.token);

  // 6. Submit registration
  const submitBtn = page.getByRole("button", { name: /register & issue/i });
  await expect(submitBtn).toBeEnabled({ timeout: 5_000 });
  await submitBtn.click();

  // 7. Verify success confirmation screen
  await expect(page.getByRole("heading", { name: `${fullName} is registered` })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(empNo)).toBeVisible();

  // 8. Verify the registered borrower can now borrow a device at the kiosk using this bound card
  const loanRes = await api.seedLoanViaKiosk(kiosk.token, device.token, blankCard.token);
  expect(loanRes.event).toBe("loan_opened");
  expect(loanRes.loan).toBeDefined();

  // Verify active loan for device
  const loans = await api.getDeviceLoans(device.id);
  const activeLoan = loans.find((l: any) => l.returnedAt === null || l.status === "active" || l.status === "open");
  expect(activeLoan).toBeDefined();
});

