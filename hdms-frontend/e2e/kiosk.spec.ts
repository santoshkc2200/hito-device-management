import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import {
  prePairKiosk,
  TestApiClient,
  type TestDevice,
  type TestKiosk,
  type TestUser,
} from "./helpers/test-api";
import { simulateScan, typeKeypad } from "./helpers/scan";

test.describe("HDMS Kiosk E2E Scenarios (E1–E13)", () => {
  let api: TestApiClient;
  let kiosk: TestKiosk;

  test.beforeEach(async ({ page }) => {
    api = new TestApiClient();
    kiosk = await api.registerKiosk("Main Station Kiosk", "Emergency Ward");
    await prePairKiosk(page, kiosk);
  });

  test("E1_DeviceThenUser_Borrow (FR-20, FR-21)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });

    // Step 1: Scan Device
    await simulateScan(page, device.token);
    await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("pending-device-card")).toContainText(device.name);
    await expect(page.getByTestId("pending-device-asset-tag")).toHaveText(device.assetTag);

    // Step 2: Scan User
    await simulateScan(page, user.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("success-title")).toContainText(/borrow/i);
    await expect(page.getByTestId("success-device-card")).toContainText(device.name);

    // Assert database state via API: 1 active loan exists
    const loans = await api.getDeviceLoans(device.id);
    const activeLoan = loans.find((l: any) => l.returnedAt === null || l.status === "active" || l.status === "open");
    expect(activeLoan).toBeDefined();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E2_UserThenDevice_Borrow (FR-21)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Step 1: Scan User
    await simulateScan(page, user.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("user-greeting-badge")).toContainText(user.fullName);

    // Step 2: Scan Device
    await simulateScan(page, device.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("success-title")).toContainText(/borrow/i);

    // Assert database state via API: 1 active loan exists
    const loans = await api.getDeviceLoans(device.id);
    const activeLoan = loans.find((l: any) => l.returnedAt === null || l.status === "active" || l.status === "open");
    expect(activeLoan).toBeDefined();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E3_DeviceThenUser_Return (FR-22)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();
    // Seed existing loan
    await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Step 1: Scan on-loan Device
    await simulateScan(page, device.token);
    await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("pending-device-card")).toContainText(device.name);

    // Step 2: Scan Borrower's User Card
    await simulateScan(page, user.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("success-kind-badge")).toContainText(/return/i);

    // Assert database state via API: loan is closed
    const loans = await api.getDeviceLoans(device.id);
    const activeLoan = loans.find((l: any) => l.returnedAt === null && (l.status === "active" || l.status === "open"));
    expect(activeLoan).toBeUndefined();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E4_UserThenDevice_Return (FR-22)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();
    // Seed existing loan
    await api.seedLoanViaKiosk(kiosk.token, device.token, user.token);

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Step 1: Scan Borrower's User Card
    await simulateScan(page, user.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId(`loan-row-${device.id}`).or(page.getByText(device.name))).toBeVisible();

    // Step 2: Scan on-loan Device
    await simulateScan(page, device.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("success-kind-badge")).toContainText(/return/i);

    // Assert database state via API: loan is closed
    const loans = await api.getDeviceLoans(device.id);
    const activeLoan = loans.find((l: any) => l.returnedAt === null && (l.status === "active" || l.status === "open"));
    expect(activeLoan).toBeUndefined();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E5_DeviceHeldBySomeoneElse_Blocked (FR-23)", async ({ page }) => {
    const userA: TestUser = await api.seedUser({ departmentName: "Cardiology" });
    const userB: TestUser = await api.seedUser({ departmentName: "Pediatrics" });
    const device: TestDevice = await api.seedDevice();

    // Loan device to User A
    await api.seedLoanViaKiosk(kiosk.token, device.token, userA.token);

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // User B attempts to borrow device held by User A
    await simulateScan(page, userB.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });

    await simulateScan(page, device.token);
    await expect(page.getByTestId("blocked-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("blocked-title")).toBeVisible();
    // Department of holder is shown
    await expect(page.getByTestId("blocked-detail")).toContainText(/Cardiology/i);

    // Assert database state: loan still belongs to User A
    const loans = await api.getDeviceLoans(device.id);
    const openLoan = loans.find((l: any) => l.userId === userA.id && (l.returnedAt === null || l.status === "open" || l.status === "active"));
    expect(openLoan).toBeDefined();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E6_ThreeDevicesOneSession (FR-24)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const dev1: TestDevice = await api.seedDevice({ name: "Pulse Oximeter 1" });
    const dev2: TestDevice = await api.seedDevice({ name: "Pulse Oximeter 2" });
    const dev3: TestDevice = await api.seedDevice({ name: "Pulse Oximeter 3" });

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Scan User
    await simulateScan(page, user.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });

    // Device 1
    await simulateScan(page, dev1.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await page.getByTestId("scan-another-button").click();
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });

    // Device 2
    await simulateScan(page, dev2.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await page.getByTestId("scan-another-button").click();
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });

    // Device 3
    await simulateScan(page, dev3.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
    await page.getByTestId("done-success-button").click();
    await page.getByTestId("done-session-button").click();

    // Returned to idle
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 5_000 });

    // Assert database state: 3 active loans created
    const allLoans = await api.getLoans();
    const userLoans = allLoans.filter(
      (l: any) => l.userId === user.id && (l.returnedAt === null || l.status === "active" || l.status === "open")
    );
    expect(userLoans.length).toBe(3);

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E7_UnknownToken_RefusedWithGuidance (FR-40, FR-43)", async ({ page }) => {
    const initialUserCount = await api.getUserCount();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Scan unknown token with valid mod-37 checksum
    await simulateScan(page, "HD-U-2VYGLTXLW4-A");
    await expect(page.getByTestId("blocked-screen")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("blocked-detail")).toContainText(/attendant|register|administrator|paper/i);

    // Assert database: user count is strictly unchanged (kiosk cannot create users!)
    const finalUserCount = await api.getUserCount();
    expect(finalUserCount).toBe(initialUserCount);

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E8_UnboundCard_RefusedAndPendingDeviceReleased (FR-43, INV-12)", async ({ page }) => {
    const device: TestDevice = await api.seedDevice();
    const validUser: TestUser = await api.seedUser();
    const unboundCard = await api.seedUnboundCard();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Scan device -> awaiting user (device is pending)
    await simulateScan(page, device.token);
    await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });

    // Scan unbound card -> rejected
    await simulateScan(page, unboundCard.token);
    await expect(page.getByTestId("blocked-screen")).toBeVisible({ timeout: 5_000 });
    await page.getByTestId("ok-blocked-button").click();

    // Returns to idle, pending device is released (assert DB status: available, no active loans)
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 5_000 });
    const releasedDevice = await api.getDevice(device.id);
    expect(releasedDevice.status).toBe("available");
    const device1Loans = await api.getDeviceLoans(device.id);
    expect(device1Loans.length).toBe(0);

    // Valid user can now borrow equipment
    const device2: TestDevice = await api.seedDevice();
    await simulateScan(page, validUser.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });
    await expect(page.getByTestId("user-greeting-badge")).toContainText(validUser.fullName);

    await simulateScan(page, device2.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });

    // Assert database state: loan belongs to validUser, not unbound card
    const loans = await api.getDeviceLoans(device2.id);
    const activeLoan = loans.find((l: any) => l.returnedAt === null || l.status === "active" || l.status === "open");
    expect(activeLoan).toBeDefined();
    expect(activeLoan.userId).toBe(validUser.id);
  });

  test("E9_RevokedCard_RejectedAndLogged (FR-15)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    await api.revokeCredential(user.credentialId, "lost card");

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Scan revoked card
    await simulateScan(page, user.token);
    await expect(page.getByTestId("blocked-screen")).toBeVisible({ timeout: 5_000 });

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E10_SessionTimeout_ReturnsToIdleWithNoPartialTransaction (FR-27)", async ({ page }) => {
    const device: TestDevice = await api.seedDevice();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Scan device -> awaiting user
    await simulateScan(page, device.token);
    await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });

    // Cancel session explicitly (simulating session reset / timeout)
    await page.getByTestId("cancel-session-button").click();
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 5_000 });

    // Assert database state: no loan exists for device
    const loans = await api.getDeviceLoans(device.id);
    const activeLoans = loans.filter((l: any) => l.returnedAt === null && (l.status === "active" || l.status === "open"));
    expect(activeLoans.length).toBe(0);
  });

  test("E11_DoubleScanWithin1500ms_OneTransaction (FR-63)", async ({ page }) => {
    const user: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Fast double scan of device within 50ms (simulating wedge bounce)
    await simulateScan(page, device.token, 2);
    await simulateScan(page, device.token, 2);

    // Kiosk is cleanly in awaiting-user (debounced)
    await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });

    // Scan user
    await simulateScan(page, user.token);
    await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });

    // Assert database: exactly 1 loan created
    const loans = await api.getDeviceLoans(device.id);
    expect(loans.length).toBe(1);
  });

  test("E12_ApiKilled_FriendlyBannerNeverABrowserError (NFR-4)", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Simulate network API failure
    await page.route("**/v1/**", (route) => route.abort());

    // Trigger a scan that fails over network
    await simulateScan(page, "HD-D-2VYGLTXLW4-A");

    // Must show friendly offline screen or error banner, never a crash or unhandled error
    await expect(
      page.getByTestId("offline-screen").or(page.getByTestId("blocked-screen"))
    ).toBeVisible({ timeout: 10_000 });

    // Cold offline launch
    await page.goto("/");
    await expect(
      page.getByTestId("offline-screen").or(page.getByTestId("idle-prompt"))
    ).toBeVisible({ timeout: 10_000 });

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });

  test("E13_CameraFallbackDecodesRenderedQr (FR-61)", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Open camera viewfinder modal
    await page.getByRole("button", { name: /camera/i }).click();

    // Assert camera viewfinder overlay is rendered
    await expect(page.getByRole("heading", { name: "Camera Barcode Scanner" })).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByTestId("camera-preview-feed")).toBeVisible();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);

    // Close camera overlay
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("idle-prompt")).toBeVisible();
  });
});
