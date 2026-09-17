import { expect, test } from "@playwright/test";
import { generateTotp, TestApiClient } from "./helpers/test-api";
import { ja } from "../apps/admin/src/i18n/ja";
import { en } from "../apps/admin/src/i18n/en";

const catalogues = { ja, en } as const;

test("admin loads over HTTPS and redirects an unauthenticated visitor to login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/, { timeout: 15_000 });
  // Login page before authentication renders in the default locale (ja), but
  // the test should pass regardless of which catalogue value is active. Match
  // either language and use a stable id selector for the input.
  await expect(page.getByRole("heading", { name: /Device management|機器管理/ })).toBeVisible();
  await expect(page.locator("#email")).toBeVisible();
});

test("AdminRegistersAndPairsKioskWithoutCLI", async ({ page, browser }) => {
  const kioskContext = await browser.newContext({ ignoreHTTPSErrors: true });
  const kioskPage = await kioskContext.newPage();
  const kioskName = `E20 Kiosk ${Date.now().toString().slice(-6)}`;

  try {
    // Keep legacy English assertions stable by ensuring admin is in en before UI login.
    // Login form itself is in the default locale (ja) before auth, so use stable id selectors.
    const apiForE20 = new TestApiClient();
    await apiForE20.login();
    await apiForE20.updateMyLocale("en");
    await page.goto("/login");
    await page.locator("#email").fill("admin@example.org");
    await page.locator("#password").fill("correct horse battery staple");
    await page.locator("#totpCode").fill(generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7"));
    await page.locator('button[type="submit"]').click();
    await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

    await page.goto("/settings?tab=kiosks");
    await page.getByRole("button", { name: "Register Kiosk" }).click();
    const registration = page.getByRole("dialog");
    await registration.getByLabel("Kiosk Name").fill(kioskName);
    await registration.getByRole("button", { name: "Register Kiosk" }).click();

    const pairingDialog = page.getByRole("dialog");
    const code = (await pairingDialog.getByTestId("pairing-code").innerText()).replace(/\D/g, "");
    expect(code).toMatch(/^\d{6}$/);

    await kioskPage.goto("https://localhost:5173/");
    await expect(kioskPage.getByTestId("pairing-screen")).toBeVisible();
    for (const digit of code) await kioskPage.getByTestId(`pairing-key-${digit}`).click();
    await kioskPage.getByTestId("pairing-submit-button").click();
    await expect(kioskPage.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });
    await expect(kioskPage.getByText(kioskName)).toBeVisible();

    // A second tablet, not a second tab: the first kiosk's pairing lives in this
    // origin's localStorage, so a page in the same context would already be paired.
    const secondContext = await browser.newContext({ ignoreHTTPSErrors: true });
    const secondKiosk = await secondContext.newPage();
    await secondKiosk.goto("https://localhost:5173/");
    for (const digit of code) await secondKiosk.getByTestId(`pairing-key-${digit}`).click();
    await secondKiosk.getByTestId("pairing-submit-button").click();
    await expect(secondKiosk.getByTestId("pairing-error-message")).toBeVisible({ timeout: 10_000 });
    await secondContext.close();
  } finally {
    await kioskContext.close();
  }
});

test("E8b_RegisterBorrowerWithBlankCard_ThenBorrowAtKiosk", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();
  // Existing English-only assertions expect en; keep admin in en for this legacy journey.
  await api.updateMyLocale("en");

  // 1. Seed blank card, device, and kiosk
  const blankCard = await api.seedUnboundCard();
  const device = await api.seedDevice();
  const kiosk = await api.registerKiosk("E8b Kiosk", "Surgery Floor");

  // 2. Perform admin login (form before auth is in default ja, so use id selectors)
  await page.goto("/login");
  await page.locator("#email").fill("admin@example.org");
  await page.locator("#password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.locator("#totpCode").fill(totp);
  await page.locator('button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

  // 3. Navigate to Register borrower screen
  await page.goto("/register");
  await expect(page.getByRole("heading", { name: "Register borrower" })).toBeVisible({ timeout: 10_000 });

  // 4. Fill in borrower details
  const empNo = `E8B-${Date.now().toString().slice(-6)}`;
  const fullName = "Dr. Alice Morgan";
  await page.getByLabel("Full name").fill(fullName);
  await page.getByLabel("Employee no.").fill(empNo);

  // 5. Select Option A (Scan a blank card) and enter the blank card token.
  // The option's label text is also the label of the token field it reveals, so
  // address the radio by role rather than by label text.
  await page.getByRole("radio", { name: new RegExp(en.register.scanBlankCard, "i") }).check();
  // A real scanner ends its wedge input with Enter, which is what triggers the
  // credential lookup that enables the submit button.
  await page.locator("#scan-token").fill(blankCard.token);
  await page.locator("#scan-token").press("Enter");
  await expect(page.getByText(new RegExp(en.register.readyToBind.split("{")[0], "i"))).toBeVisible({ timeout: 10_000 });

  // 6. Submit registration
  const submitBtn = page.getByRole("button", { name: /register & issue/i });
  await expect(submitBtn).toBeEnabled({ timeout: 5_000 });
  await submitBtn.click();

  // 7. Verify success confirmation screen
  await expect(page.getByRole("heading", { name: `${fullName} is registered` })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(empNo)).toBeVisible();

  // 8. Verify the registered borrower can now borrow a device at the kiosk using this bound card
  const loanRes = await api.seedLoanViaKiosk(kiosk.token, device.token, blankCard.token);
  expect(loanRes.outcome.kind).toBe("borrowed");
  expect(loanRes.outcome.loanId).toBeDefined();

  // Verify active loan for device
  const loans = await api.getDeviceLoans(device.id);
  const activeLoan = loans.find((l: any) => l.returnedAt === null || l.status === "active" || l.status === "open");
  expect(activeLoan).toBeDefined();
});

test("E14_ReissueLostCard_KillsOldToken_NewWorksAtKiosk", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();
  await api.updateMyLocale("en");

  // 1. Seed user, device, and kiosk
  const user = await api.seedUser({ fullName: "Dr. Evelyn Reed", employeeNo: `E14-${Date.now().toString().slice(-6)}` });
  const device = await api.seedDevice();
  const kiosk = await api.registerKiosk("E14 Kiosk", "Radiology");

  // 2. Perform admin login (form before auth is in ja, use id selectors)
  await page.goto("/login");
  await page.locator("#email").fill("admin@example.org");
  await page.locator("#password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.locator("#totpCode").fill(totp);
  await page.locator('button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login$/, { timeout: 10_000 });

  // 3. Navigate to user detail page
  await page.goto(`/users/${user.id}`);
  await expect(page.getByRole("heading", { name: "Dr. Evelyn Reed" })).toBeVisible({ timeout: 10_000 });

  // 4. Report the card lost, which reissues it
  const reissueBtn = page.getByRole("button", { name: en.credentialsPanel.reportLost, exact: true });
  await expect(reissueBtn).toBeVisible({ timeout: 5_000 });
  await reissueBtn.click();

  // 5. Fill reason and submit
  await expect(page.getByText(en.credentialsPanel.reissueConfirmDescriptionUser)).toBeVisible();
  await page.getByPlaceholder(en.credentialsPanel.reasonPlaceholder).fill("Card lost in operating theater");
  await page.getByRole("button", { name: en.credentialsPanel.reissueConfirmButtonUser }).click();

  // 6. Token reveal dialog appears with the new token
  await expect(page.getByRole("heading", { name: en.tokenRevealDialog.title })).toBeVisible({ timeout: 10_000 });
  const newTokenElement = page.getByTestId("revealed-token");
  await expect(newTokenElement).toBeVisible();
  const newTokenText = (await newTokenElement.innerText()).trim();

  // Close reveal dialog
  await page.keyboard.press("Escape");

  // 7. Verify old token is dead at kiosk
  const oldAttempt = await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);
  expect(oldAttempt.outcome.kind).toBe("rejected");

  // 8. Verify new token works at kiosk
  const newAttempt = await api.seedLoanViaKiosk(kiosk.token, device.token, newTokenText);
  expect(newAttempt.outcome.kind).toBe("borrowed");
  expect(newAttempt.outcome.loanId).toBeDefined();
});

test("E15_ForceReturn_AuditsOverrideAndReason", async ({ page }) => {
  const api = new TestApiClient();
  await api.login();
  await api.updateMyLocale("en");

  // 1. Seed user, device, kiosk and create an active loan
  const user = await api.seedUser({ fullName: "Nurse Kenji Sato", employeeNo: `E15-${Date.now().toString().slice(-6)}` });
  const device = await api.seedDevice({ assetTag: `DEV-E15-${Date.now().toString().slice(-4)}` });
  const kiosk = await api.registerKiosk("E15 Kiosk", "ICU");

  const openRes = await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);
  expect(openRes.outcome.kind).toBe("borrowed");
  const loanId = openRes.outcome.loanId;

  // 2. Admin login (form before auth is in ja, use id selectors)
  await page.goto("/login");
  await page.locator("#email").fill("admin@example.org");
  await page.locator("#password").fill("correct horse battery staple");
  const totp = generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7");
  await page.locator("#totpCode").fill(totp);
  await page.locator('button[type="submit"]').click();
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
  // The reason shows twice on the page — as the audit note and inside the raw
  // event payload — so assert on the first.
  await expect(page.getByText(/Device returned to charge bay by orderly without scanning/i).first()).toBeVisible();
});

// One signed-in admin journey per locale — sign in, list devices, open one,
// edit it, export CSV — asserting through catalogue. Not a screen-by-screen sweep.
for (const locale of ["ja", "en"] as const) {
  test.describe(`Admin console i18n journey — ${locale}`, () => {
    let api: TestApiClient;
    let device: { id: string; assetTag: string; name: string };

    test.beforeEach(async () => {
      api = new TestApiClient();
      await api.login();
      await api.updateMyLocale(locale);
      device = await api.seedDevice({
        name: `E2E Device ${locale}-${Date.now().toString().slice(-6)}`,
      });
    });

    test(`sign in → list → open → edit → export CSV in ${locale}`, async ({ page }) => {
      const msgs = catalogues[locale];

      // 1. Sign in — login form before auth is in the default locale (ja),
      // so use stable id selectors to avoid pre-auth language mismatch.
      await page.goto("/login");
      await page.locator("#email").fill("admin@example.org");
      await page.locator("#password").fill("correct horse battery staple");
      await page.locator("#totpCode").fill(generateTotp(process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7"));
      await page.locator('button[type="submit"]').click();
      await expect(page).not.toHaveURL(/\/login$/, { timeout: 15_000 });

      // 2. Language is sticky per account
      await expect.poll(async () => page.evaluate(() => document.documentElement.lang), { timeout: 5_000 }).toBe(locale);

      // 3. List devices — heading through catalogue
      await page.goto("/devices");
      await expect(page.getByRole("heading", { name: msgs.devices.title })).toBeVisible({ timeout: 10_000 });
      await expect(page.getByText(device.assetTag).first()).toBeVisible({ timeout: 10_000 });

      // 4. Open device detail
      await page.goto(`/devices/${device.id}`);
      await expect(page.getByRole("heading", { name: msgs.deviceDetail.attributesHeading })).toBeVisible({ timeout: 10_000 });
      await expect(page.getByRole("heading", { name: device.name })).toBeVisible({ timeout: 10_000 });
      await expect(page.getByText(device.assetTag).first()).toBeVisible();

      // 5. Edit device
      await page.getByRole("button", { name: msgs.deviceDetail.edit }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expect(page.getByRole("heading", { name: msgs.deviceDetail.editTitle })).toBeVisible();
      const newName = `Edited ${locale} ${Date.now().toString().slice(-5)}`;
      await page.locator("#name").fill(newName);
      await page.getByRole("button", { name: msgs.common.save }).click();
      await expect(page.getByText(msgs.deviceForm.deviceUpdated)).toBeVisible({ timeout: 10_000 });
      await expect(page.getByRole("dialog")).not.toBeVisible({ timeout: 5_000 });
      await expect(page.getByRole("heading", { name: newName })).toBeVisible({ timeout: 10_000 });
      const updated = await api.getDevice(device.id);
      expect(updated.name).toBe(newName);

      // 6. Export CSV — select device and export. The CSV Blob carries a BOM
      // (verified in unit test hdms-frontend/apps/admin/src/lib/csv.test.ts);
      // here we verify the file is downloadable and the UI reports success through catalogue.
      await page.goto("/devices");
      await expect(page.getByText(device.assetTag).first()).toBeVisible({ timeout: 10_000 });
      const selectLabel = msgs.devices.selectDeviceAria.replace("{assetTag}", device.assetTag);
      await page.getByLabel(selectLabel).check();
      const exportBtn = page.getByRole("button", { name: msgs.devices.exportCsv });
      await expect(exportBtn).toBeVisible();
      const downloadPromise = page.waitForEvent("download");
      await exportBtn.click();
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toMatch(/devices-export.*\.csv/);
      const expectedExport = msgs.devices.exported.replace("{count}", "1");
      await expect(page.getByText(expectedExport)).toBeVisible({ timeout: 5_000 });
    });
  });
}
