# HDMS End-to-End Testing Suite (Playwright)

This directory contains automated end-to-end tests exercising critical user journeys across the HDMS Kiosk and Admin Console against a real backend API and PostgreSQL database.

## Architecture

- **Simulated Hardware Input**: Barcode and QR scans are dispatched via `simulateScan(page, token)` at ~8ms keyboard event intervals, faithfully reproducing real Bluetooth HID wedge scanners.
- **Full Invariant & State Verification**: Tests assert both user-visible UI outcomes and ground-truth database/API state (`GET /v1/loans`, `GET /v1/users`, etc.).
- **Accessibility Budget**: `axe-core` accessibility audits run on every screen transition in the test suite to ensure strict zero-violation compliance (WCAG 2.2 AA).
- **Zero Raw Errors**: All failure modes assert that no stack traces or raw errors are presented to users.

## Scenarios

### Kiosk Journeys (`e2e/kiosk.spec.ts`)
- `E1_DeviceThenUser_Borrow` (FR-20, FR-21)
- `E2_UserThenDevice_Borrow` (FR-21)
- `E3_DeviceThenUser_Return` (FR-22)
- `E4_UserThenDevice_Return` (FR-22)
- `E5_DeviceHeldBySomeoneElse_Blocked` (FR-23)
- `E6_ThreeDevicesOneSession` (FR-24)
- `E7_UnknownToken_RefusedWithGuidance` (FR-40, FR-43)
- `E8_UnboundCard_RefusedAndPendingDeviceReleased` (FR-43, INV-12)
- `E9_RevokedCard_RejectedAndLogged` (FR-15)
- `E10_SessionTimeout_ReturnsToIdleWithNoPartialTransaction` (FR-27)
- `E11_DoubleScanWithin1500ms_OneTransaction` (FR-63)
- `E12_ApiKilled_FriendlyBannerNeverABrowserError` (NFR-4)
- `E13_CameraFallbackDecodesRenderedQr` (FR-61)

### Admin Console Journeys (`e2e/admin.spec.ts`)
- Reserved for Phase 4 (E8b, E14–E19).

## Running Tests Locally

The suite signs in as a dedicated test administrator and seeds some fixtures over
`psql`, so a fresh machine needs both before `task e2e` will pass:

```bash
# 1. Start database and backend stack
task dev

# 2. Once per database — create the administrator the suite signs in as
cd hdms-backend && go run ./cmd/hdms-cli admin bootstrap \
  --email admin@example.org --name "Admin" \
  --password "correct horse battery staple" \
  --totp-secret "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7"

# 3. `psql` must be on PATH (macOS: brew install libpq, then add its bin)
psql --version

# 4. Run Playwright E2E suite
task e2e
```

The credentials above are the ones in `.env` (`HDMS_TEST_ADMIN_*`) and in CI; they
are development-only.

Or run via pnpm directly in `hdms-frontend`:
```bash
pnpm exec playwright test
```

## Flake Policy

1. **Zero Tolerance for Unaddressed Flakes**: A flaky test in CI or local runs must be either fixed immediately or quarantined into an explicit tracked issue within 24 hours. Tests must never be left silently retrying.
2. **Deterministic Synchronization**: No `waitForTimeout` arbitrary sleeps are permitted in the suite. All assertions must be synchronized against visible UI states, element state locators, or API completion events.
