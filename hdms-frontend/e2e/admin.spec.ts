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

test("E14_ReissueLostCard_KillsOldToken_NewWorksAtKiosk", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();

  // 1. Seed user, device, and kiosk
  const user = await api.seedUser({ fullName: "Dr. Evelyn Reed", employeeNo: `E14-${Date.now().toString().slice(-6)}` });
  const device = await api.seedDevice();
  const kiosk = await api.registerKiosk("E14 Kiosk", "Radiology");

  // 2. Perform admin login
  await page.goto("/login");
  await page.getByLabel("Email").fill("admin@example.org");
  await page.getByLabel("Password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.getByLabel("Authenticator code").fill(totp);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

  // 3. Navigate to user detail page
  await page.goto(`/users/${user.id}`);
  await expect(page.getByRole("heading", { name: "Dr. Evelyn Reed" })).toBeVisible({ timeout: 10_000 });

  // 4. Click Report Lost & Reissue / Reissue & Print
  const reissueBtn = page.getByRole("button", { name: /reissue & print/i });
  await expect(reissueBtn).toBeVisible({ timeout: 5_000 });
  await reissueBtn.click();

  // 5. Fill reason and submit
  await expect(page.getByText(/the current card in the borrower's pocket stops working immediately/i)).toBeVisible();
  await page.getByPlaceholder(/lost in cafeteria/i).fill("Card lost in operating theater");
  await page.getByRole("button", { name: /reissue & print card/i }).click();

  // 6. Token reveal dialog appears with the new token
  await expect(page.getByRole("heading", { name: /credential minted/i })).toBeVisible({ timeout: 10_000 });
  const newTokenElement = page.getByTestId("revealed-token");
  await expect(newTokenElement).toBeVisible();
  const newTokenText = (await newTokenElement.innerText()).trim();

  // Close reveal dialog
  await page.getByRole("button", { name: /done/i }).click();

  // 7. Verify old token is dead at kiosk
  const oldAttempt = await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);
  expect(oldAttempt.event).toBe("card_revoked");

  // 8. Verify new token works at kiosk
  const newAttempt = await api.seedLoanViaKiosk(kiosk.token, device.token, newTokenText);
  expect(newAttempt.event).toBe("loan_opened");
  expect(newAttempt.loan).toBeDefined();
});

test("E15_ForceReturn_AuditsOverrideAndReason", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();

  // 1. Seed user, device, kiosk and create an active loan
  const user = await api.seedUser({ fullName: "Nurse Kenji Sato", employeeNo: `E15-${Date.now().toString().slice(-6)}` });
  const device = await api.seedDevice({ assetTag: `DEV-E15-${Date.now().toString().slice(-4)}` });
  const kiosk = await api.registerKiosk("E15 Kiosk", "ICU");

  const openRes = await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);
  expect(openRes.event).toBe("loan_opened");
  const loanId = openRes.loan.id;

  // 2. Admin login
  await page.goto("/login");
  await page.getByLabel("Email").fill("admin@example.org");
  await page.getByLabel("Password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.getByLabel("Authenticator code").fill(totp);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

  // 3. Navigate to Loan detail page
  await page.goto(`/loans/${loanId}`);
  await expect(page.getByTestId("force-return-button")).toBeVisible({ timeout: 10_000 });

  // 4. Force return with reason
  await page.getByTestId("force-return-button").click();
  await expect(page.getByText("Force Return Loan")).toBeVisible();

  await page.getByLabel(/reason for administrative return/i).fill("Device returned to charge bay by orderly without scanning");
  await page.getByRole("button", { name: /confirm return/i }).click();

  // 5. Verify loan status is now returned
  await expect(page.getByText(/returned/i).first()).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(/loan\.force_returned/i)).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(/Device returned to charge bay by orderly without scanning/i)).toBeVisible();
});

