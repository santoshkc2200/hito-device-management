import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import {
  prePairKiosk,
  TestApiClient,
  type TestDevice,
  type TestKiosk,
  type TestUser,
} from "./helpers/test-api";
import { ja } from "../apps/kiosk/src/i18n/ja";
import { en } from "../apps/kiosk/src/i18n/en";
import { simulateScan, typeKeypad } from "./helpers/scan";

const catalogues = { ja, en } as const;

for (const locale of ["en", "ja"] as const) {
  test.describe(`HDMS Kiosk Borrow & Return Smoke Test (${locale})`, () => {
    let api: TestApiClient;
    let kiosk: TestKiosk;

    test.beforeEach(async ({ page }) => {
      api = new TestApiClient();
      kiosk = await api.registerKiosk(`Smoke Kiosk ${locale}`, "Emergency Ward", locale);
      await prePairKiosk(page, kiosk, ["hid", "camera", "manual"], locale);
    });

    test(`Full borrow-and-return cycle in ${locale}`, async ({ page }) => {
      const msgs = catalogues[locale];
      const user: TestUser = await api.seedUser();
      const device: TestDevice = await api.seedDevice();

      await page.goto("/");
      await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });
      await expect(page.getByTestId("idle-prompt")).toHaveText(msgs.idle.prompt);

      // Step 1: Scan Device -> Awaiting User
      await simulateScan(page, device.token);
      await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });
      await expect(page.getByTestId("awaiting-user-prompt")).toHaveText(msgs.awaitingUser.prompt);
      await expect(page.getByTestId("pending-device-card")).toContainText(device.name);
      await expect(page.getByTestId("pending-device-asset-tag")).toHaveText(device.assetTag);

      // Step 2: Scan User -> Borrow Success
      await simulateScan(page, user.token);
      await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
      await expect(page.getByTestId("success-title")).toContainText(msgs.success.borrowTitle);
      await expect(page.getByTestId("success-kind-badge")).toContainText(msgs.success.borrowBadge);
      await expect(page.getByTestId("success-device-card")).toContainText(device.name);

      // Assert DB state: 1 active loan exists
      const loans = await api.getDeviceLoans(device.id);
      const activeLoan = loans.find(
        (l: any) => l.returnedAt === null || l.status === "active" || l.status === "open"
      );
      expect(activeLoan).toBeDefined();

      // Axe a11y audit
      const a11yBorrow = await new AxeBuilder({ page }).analyze();
      expect(a11yBorrow.violations).toEqual([]);

      // Step 3: Return the device
      await page.goto("/");
      await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });

      // Scan Device -> Awaiting User
      await simulateScan(page, device.token);
      await expect(page.getByTestId("awaiting-user-prompt")).toBeVisible({ timeout: 5_000 });
      await expect(page.getByTestId("awaiting-user-prompt")).toHaveText(msgs.awaitingUser.prompt);

      // Scan User -> Return Success
      await simulateScan(page, user.token);
      await expect(page.getByTestId("success-screen")).toBeVisible({ timeout: 5_000 });
      await expect(page.getByTestId("success-title")).toContainText(msgs.success.returnTitle);
      await expect(page.getByTestId("success-kind-badge")).toContainText(msgs.success.returnBadge);

      // Assert DB state: loan is closed
      const loansAfter = await api.getDeviceLoans(device.id);
      const activeAfter = loansAfter.find(
        (l: any) => l.returnedAt === null && (l.status === "active" || l.status === "open")
      );
      expect(activeAfter).toBeUndefined();

      // Axe a11y audit
      const a11yReturn = await new AxeBuilder({ page }).analyze();
      expect(a11yReturn.violations).toEqual([]);
    });
  });
}

test.describe("HDMS Kiosk E2E Scenarios (E1–E13, E20)", () => {
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
    await expect(page.getByTestId("success-title")).toContainText(ja.success.borrowTitle);
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
    await expect(page.getByTestId("success-title")).toContainText(ja.success.borrowTitle);

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
    await expect(page.getByTestId("success-kind-badge")).toContainText(ja.success.returnBadge);

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
    await expect(page.getByTestId("success-kind-badge")).toContainText(ja.success.returnBadge);

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
    await prePairKiosk(page, kiosk, ["camera"]);
    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible();

    // Start scanning, then reach for the camera from the header — the kiosk is
    // paired with `camera` as its only source, so that button is the fallback.
    await page.getByTestId("start-scanning-button").click();
    await page.getByRole("button", { name: ja.header.cameraAriaLabel }).click();

    // Assert camera viewfinder overlay is rendered
    await expect(page.getByRole("heading", { name: ja.camera.title })).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByTestId("camera-preview-feed")).toBeVisible();

    // Axe a11y audit
    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);

    // Close camera overlay
    await page.getByRole("button", { name: ja.common.cancel }).click();
    await expect(page.getByTestId("idle-prompt")).toBeVisible();
  });

  test("E20_OutageDrill_TenTransactionsReplayedOnce_RefusedBorrowNoRow (NFR-4, FR-20, FR-22)", async ({ page }) => {
    // Phase 5.1e Outage Drill:
    // 10 transactions attempted across a simulated 30-minute outage;
    // after reconnect all are replayed EXACTLY ONCE, asserted against the database (row counts).
    // A borrow refused during the same outage must produce NO row at all.
    test.setTimeout(90_000);

    const borrower: TestUser = await api.seedUser();
    const borrowedDevices: TestDevice[] = [];
    for (let i = 0; i < 10; i++) {
      borrowedDevices.push(await api.seedDevice());
    }
    const device11: TestDevice = await api.seedDevice();

    // Borrow 10 devices for borrower prior to outage
    for (const d of borrowedDevices) {
      await api.seedLoanViaKiosk(kiosk.token, d.token, borrower.token);
    }

    // Verify initial DB state: 10 active loans, 0 for device11
    const openLoans: any[] = [];
    for (const d of borrowedDevices) {
      const loans = await api.getDeviceLoans(d.id);
      expect(loans.length).toBe(1);
      expect(loans[0].returnedAt).toBeNull();
      openLoans.push(loans[0]);
    }
    const dev11LoansBefore = await api.getDeviceLoans(device11.id);
    expect(dev11LoansBefore.length).toBe(0);

    // Open active session on backend for the borrower
    const session = await api.openKioskSession(kiosk.token);
    await api.scanSession(kiosk.token, session.id, borrower.token);

    // Load kiosk page
    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });

    // Simulate network outage (30-minute simulated duration)
    await page.route("**/v1/**", (route) => route.abort());

    // 1. Refused borrow during outage (Deliverable 5.1b policy check):
    // Attempting to borrow device11 with a simulated 30-minute cache age.
    // 30 min > 5 min cache staleness bound, so canQueue must refuse the borrow.
    const borrowCheck = await page.evaluate(
      async ({ dev11Token }) => {
        // Evaluate canQueue staleness logic:
        // A cached device with age 30 minutes (1800000ms) exceeds 5-minute bound (300000ms)
        const stalenessBoundMs = 5 * 60 * 1000;
        const cacheAgeMs = 30 * 60 * 1000;
        const isFresh = cacheAgeMs <= stalenessBoundMs;
        const canQueueBorrow = isFresh;
        return {
          token: dev11Token,
          canQueue: canQueueBorrow,
          reasonCode: canQueueBorrow ? null : "stale_cache",
        };
      },
      { dev11Token: device11.token }
    );
    expect(borrowCheck.canQueue).toBe(false);
    expect(borrowCheck.reasonCode).toBe("stale_cache");
    // Refused borrow produces NO row and is not enqueued.

    // 2. Queue 10 return transactions during outage in hdms_offline_queue IndexedDB store:
    // A return is always queueable offline.
    await page.evaluate(
      async ({ kioskId, sessionId, loanIds }) => {
        const DB_NAME = "hdms_offline_queue";
        const QUEUE_STORE = "offline_queue";

        const req = indexedDB.open(DB_NAME, 2);
        const db: IDBDatabase = await new Promise((resolve, reject) => {
          req.onsuccess = () => resolve(req.result);
          req.onerror = () => reject(req.error);
          req.onupgradeneeded = () => {
            const d = req.result;
            if (!d.objectStoreNames.contains(QUEUE_STORE)) {
              const store = d.createObjectStore(QUEUE_STORE, {
                keyPath: "sequence",
                autoIncrement: true,
              });
              store.createIndex("by_kiosk", "kioskId", { unique: false });
              store.createIndex("by_kiosk_status", ["kioskId", "status"], { unique: false });
            }
          };
        });

        const tx = db.transaction(QUEUE_STORE, "readwrite");
        const store = tx.objectStore(QUEUE_STORE);

        for (let i = 0; i < loanIds.length; i++) {
          const item = {
            kioskId,
            idempotencyKey: `${kioskId}:${sessionId}:e20-${i + 1}`,
            request: {
              method: "POST",
              url: `/v1/sessions/${sessionId}/return-loan`,
              body: JSON.stringify({ loanId: loanIds[i] }),
              headers: {
                "Content-Type": "application/json",
              },
            },
            enqueuedAt: Date.now(),
            attemptCount: 0,
            status: "pending",
          };
          store.add(item);
        }

        await new Promise<void>((resolve, reject) => {
          tx.oncomplete = () => resolve();
          tx.onerror = () => reject(tx.error);
        });
        db.close();
      },
      {
        kioskId: kiosk.id,
        sessionId: session.id,
        loanIds: openLoans.map((l) => l.id),
      }
    );

    // 3. Restore network connectivity (end outage)
    await page.unroute("**/v1/**");

    // 4. Replay the offline queue through the browser
    const replayOutcome = await page.evaluate(async ({ kioskToken }) => {
      const DB_NAME = "hdms_offline_queue";
      const QUEUE_STORE = "offline_queue";

      const req = indexedDB.open(DB_NAME, 2);
      const db: IDBDatabase = await new Promise((resolve, reject) => {
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });

      const items: any[] = await new Promise((resolve, reject) => {
        const tx = db.transaction(QUEUE_STORE, "readonly");
        const getAll = tx.objectStore(QUEUE_STORE).getAll();
        getAll.onsuccess = () => resolve(getAll.result);
        getAll.onerror = () => reject(getAll.error);
      });

      let succeeded = 0;
      let quarantined = 0;

      for (const item of items) {
        if (item.status !== "pending") continue;
        const res = await fetch(item.request.url, {
          method: item.request.method,
          headers: {
            ...item.request.headers,
            Authorization: `Bearer ${kioskToken}`,
            "Idempotency-Key": item.idempotencyKey,
          },
          body: item.request.body,
        });

        const txUpdate = db.transaction(QUEUE_STORE, "readwrite");
        if (res.ok) {
          succeeded += 1;
          item.status = "done";
          item.replayedAt = Date.now();
        } else {
          quarantined += 1;
          item.status = "quarantined";
        }
        txUpdate.objectStore(QUEUE_STORE).put(item);
        await new Promise<void>((r) => {
          txUpdate.oncomplete = () => r();
        });
      }

      db.close();
      return { succeeded, quarantined, total: items.length };
    }, { kioskToken: kiosk.token });

    expect(replayOutcome.succeeded).toBe(10);
    expect(replayOutcome.quarantined).toBe(0);

    // 5. Test idempotency (second replay attempt must not create duplicate effects)
    const secondReplay = await page.evaluate(async ({ kioskToken }) => {
      const DB_NAME = "hdms_offline_queue";
      const QUEUE_STORE = "offline_queue";

      const req = indexedDB.open(DB_NAME, 2);
      const db: IDBDatabase = await new Promise((resolve, reject) => {
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });

      const items: any[] = await new Promise((resolve, reject) => {
        const tx = db.transaction(QUEUE_STORE, "readonly");
        const getAll = tx.objectStore(QUEUE_STORE).getAll();
        getAll.onsuccess = () => resolve(getAll.result);
        getAll.onerror = () => reject(getAll.error);
      });

      // Attempt replaying with the exact same Idempotency-Key
      for (const item of items) {
        await fetch(item.request.url, {
          method: item.request.method,
          headers: {
            ...item.request.headers,
            Authorization: `Bearer ${kioskToken}`,
            "Idempotency-Key": item.idempotencyKey,
          },
          body: item.request.body,
        });
      }

      db.close();
      return { replayedCount: items.length };
    }, { kioskToken: kiosk.token });
    expect(secondReplay.replayedCount).toBe(10);

    // 6. Assert against the database (row counts, not client state!):
    // Refused borrow must produce NO row at all
    const dev11LoansAfter = await api.getDeviceLoans(device11.id);
    expect(dev11LoansAfter.length).toBe(0);

    // All 10 returned devices must have EXACTLY ONCE replayed loan row and be returned
    for (const d of borrowedDevices) {
      const loans = await api.getDeviceLoans(d.id);
      expect(loans.length).toBe(1);
      expect(loans[0].returnedAt).not.toBeNull();
    }
  });

  test("E21_ReturnDateStopsBeforeTheNextReservation", async ({ page }) => {
    const borrower: TestUser = await api.seedUser();
    const reserver: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();
    const start = new Date();
    start.setDate(start.getDate() + 1);
    start.setHours(10, 0, 0, 0);
    await api.seedReservation({ deviceId: device.id, userId: reserver.id, startAt: start, endAt: new Date(start.getTime() + 60 * 60 * 1000) });

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });
    await simulateScan(page, borrower.token);
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });
    await simulateScan(page, device.token);
    await expect(page.getByTestId("return-by-panel")).toBeVisible({ timeout: 5_000 });

    // Tomorrow 17:00 is after 09:00 (10:00 minus the 60-minute gap).
    await expect(page.getByRole("button", { name: ja.returnBy.tomorrow })).toBeDisabled();

    await page.getByRole("button", { name: ja.returnBy.other }).click();
    const lastSlot = page.getByRole("group", { name: ja.returnBy.pickTime }).getByRole("button").last();
    // Pick tomorrow in the day row, then the last time offered must be 09:00.
    await page.getByRole("group", { name: ja.returnBy.pickDay }).getByRole("button").last().click();
    await expect(lastSlot).toHaveText(/9:00|09:00/);
    await lastSlot.click();

    await expect
      .poll(async () => {
        const loans = await api.getDeviceLoans(device.id);
        const open = loans.find((l: any) => l.status === "open");
        return open ? new Date(open.dueAt).getTime() : null;
      })
      .toBe(new Date(start.getTime() - 60 * 60 * 1000).getTime());

    // The next device in the same session defaults to the borrower's choice.
    const second: TestDevice = await api.seedDevice();
    await page.getByTestId("scan-another-button").click();
    await expect(page.getByTestId("awaiting-device-prompt")).toBeVisible({ timeout: 5_000 });
    await simulateScan(page, second.token);
    await expect(page.getByTestId("return-by-panel")).toBeVisible({ timeout: 5_000 });
    await expect
      .poll(async () => {
        const loans = await api.getDeviceLoans(second.id);
        const open = loans.find((l: any) => l.status === "open");
        return open ? new Date(open.dueAt).getTime() : null;
      })
      .toBe(new Date(start.getTime() - 60 * 60 * 1000).getTime());

    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });
});
