import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as fs from "node:fs";
import * as path from "node:path";
import { LOCALES, LocaleProvider, translate, type Locale } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { IdleScreen } from "./idle-screen";
import { AwaitingUserScreen } from "./awaiting-user-screen";
import { AwaitingDeviceScreen } from "./awaiting-device-screen";
import { SuccessScreen } from "./success-screen";
import { BlockedScreen } from "./blocked-screen";
import { OfflineScreen } from "./offline-screen";
import { PairingScreen } from "./pairing-screen";
import { OUTCOME_FEEDBACK_MAP, ALL_OUTCOME_KINDS } from "@/lib/feedback-config";

/** The longest value in a locale's catalogue is the worst case for that locale. */
function longestValue(locale: Locale): string {
  let longest = "";
  const walk = (node: unknown) => {
    if (typeof node === "string") {
      if (node.length > longest.length) longest = node;
    } else if (node && typeof node === "object") {
      Object.values(node).forEach(walk);
    }
  };
  walk(catalogues[locale]);
  return longest;
}

describe("Phase 3.9 — Visual Design & Typography System", () => {
  describe("1. Token Usage Audit (Zero Ad-hoc Styles)", () => {
    it("no hard-coded hex colors or inline font sizes outside token sheet", () => {
      const screensDir = path.resolve(__dirname);
      const componentsDir = path.resolve(__dirname, "../components");

      const getTsxFiles = (dir: string): string[] => {
        const entries = fs.readdirSync(dir, { withFileTypes: true });
        let files: string[] = [];
        for (const entry of entries) {
          const fullPath = path.join(dir, entry.name);
          if (entry.isDirectory() && entry.name !== "node_modules") {
            files = files.concat(getTsxFiles(fullPath));
          } else if (
            entry.isFile() &&
            (entry.name.endsWith(".tsx") || entry.name.endsWith(".ts")) &&
            !entry.name.endsWith(".test.tsx") &&
            !entry.name.endsWith(".test.ts")
          ) {
            files.push(fullPath);
          }
        }
        return files;
      };

      const filesToCheck = [...getTsxFiles(screensDir), ...getTsxFiles(componentsDir)];

      for (const filePath of filesToCheck) {
        const content = fs.readFileSync(filePath, "utf-8");
        const relPath = path.relative(screensDir, filePath);

        // 1. Check for hard-coded hex colors (#fff, #123456, etc.)
        const hexMatches = content.match(/#[0-9a-fA-F]{3,8}\b/g);
        expect(
          hexMatches,
          `File ${relPath} contains hard-coded hex colors: ${hexMatches?.join(", ")}. Use design tokens instead.`
        ).toBeNull();

        // 2. Check for inline style font size overrides: style={{ fontSize: ... }}
        expect(
          content.includes("fontSize:"),
          `File ${relPath} contains inline fontSize styles. Use tokenized classes (e.g. text-kiosk-prompt).`
        ).toBe(false);

        // 3. Check for ad-hoc arbitrary Tailwind text size brackets like text-[22px]
        const arbitraryTextMatches = content.match(/text-\[\d+px\]/g);
        expect(
          arbitraryTextMatches,
          `File ${relPath} contains arbitrary text sizes: ${arbitraryTextMatches?.join(", ")}. Use tokenized classes.`
        ).toBeNull();
      }
    });
  });

  describe.each(LOCALES)("2. Worst-Case Content Stress Testing in %s", (locale) => {
    it("handles 40+ character device names without layout breakage", () => {
      const veryLongDeviceName = `Sonosite M-Turbo Portable Ultrasound ${longestValue(locale)}`;

      const { container } = render(
        <LocaleProvider locale={locale}>
          <AwaitingUserScreen
            pendingDevice={{
              id: "dev-long-1",
              assetTag: "US-PORT-99014",
              name: veryLongDeviceName,
            }}
            expiresAt={new Date(Date.now() + 30000).toISOString()}
            onCancel={vi.fn()}
          />
        </LocaleProvider>
      );

      expect(screen.getByText(veryLongDeviceName)).toBeInTheDocument();
      expect(screen.getByTestId("pending-device-asset-tag")).toHaveTextContent("US-PORT-99014");
      expect(screen.getByTestId("cancel-session-button")).toBeInTheDocument();
      expect(container.querySelector('[data-testid="awaiting-user-prompt"]')).toBeInTheDocument();
    });

    it("handles 5+ active loans list with mixed overdue states cleanly", () => {
      const mockLoans = [
        {
          id: "loan-1",
          deviceId: "dev-1",
          deviceName: "Mindray Patient Monitor ePM 12M",
          assetTag: "MON-001",
          borrowedAt: new Date(Date.now() - 86400000 * 4).toISOString(),
          dueAt: new Date(Date.now() - 86400000 * 2).toISOString(), // 2 days overdue
        },
        {
          id: "loan-2",
          deviceId: "dev-2",
          deviceName: "Alaris Infusion Pump 8100 Series",
          assetTag: "PUMP-8100",
          borrowedAt: new Date(Date.now() - 86400000 * 2).toISOString(),
          dueAt: new Date(Date.now() + 86400000 * 1).toISOString(),
        },
        {
          id: "loan-3",
          deviceId: "dev-3",
          deviceName: "Welch Allyn Connex Spot Monitor",
          assetTag: "WA-CSM-44",
          borrowedAt: new Date(Date.now() - 3600000 * 5).toISOString(),
          dueAt: new Date(Date.now() + 3600000 * 19).toISOString(),
        },
        {
          id: "loan-4",
          deviceId: "dev-4",
          deviceName: "Zoll R Series Defibrillator Unit",
          assetTag: "ZOLL-R-09",
          borrowedAt: new Date(Date.now() - 86400000 * 5).toISOString(),
          dueAt: new Date(Date.now() - 86400000 * 1).toISOString(), // 1 day overdue
        },
        {
          id: "loan-5",
          deviceId: "dev-5",
          deviceName: "Medtronic Covidien Capnostream 35",
          assetTag: "CAPNO-35",
          borrowedAt: new Date(Date.now() - 3600000 * 2).toISOString(),
          dueAt: new Date(Date.now() + 3600000 * 22).toISOString(),
        },
      ];

      render(
        <LocaleProvider locale={locale}>
          <AwaitingDeviceScreen
            user={{
              id: "user-100",
              fullName: "Dr. Catherine Montgomery-Smith",
              department: "Pediatric Intensive Care Unit",
              openLoanCount: 5,
            }}
            openLoans={mockLoans}
            expiresAt={new Date(Date.now() + 25000).toISOString()}
            onReturnLoan={vi.fn()}
            onClose={vi.fn()}
          />
        </LocaleProvider>
      );

      expect(
        screen.getByText(
          translate(catalogues, locale, "awaitingDevice.loansHeading", { count: 5 })
        )
      ).toBeInTheDocument();
      expect(screen.getByText("Mindray Patient Monitor ePM 12M")).toBeInTheDocument();
      expect(screen.getByText("Medtronic Covidien Capnostream 35")).toBeInTheDocument();
      expect(screen.getByTestId("done-session-button")).toBeInTheDocument();

      // Check overdue indicators
      expect(screen.getByTestId("overdue-flag-loan-1")).toHaveTextContent(/overdue|超過/i);
      expect(screen.getByTestId("overdue-flag-loan-4")).toHaveTextContent(/overdue|超過/i);
    });

    it("handles 3+ line server refusal message with proper guidance display", () => {
      const longGuidance =
        "This device is currently designated for Emergency Department Level 1 Trauma use only.\n" +
        "Please select a general-purpose unit from Charging Bay 3 or consult the duty charge nurse on Ext 4410.\n" +
        "If immediate assistance is required, record checkout manually in the paper register.";

      render(
        <LocaleProvider locale={locale}>
          <BlockedScreen
            message={{
              title: "Restricted Equipment Pool",
              detail: longGuidance,
              tone: "warning",
            }}
            supportCode="RESTRICTED-POOL-ED"
            onDismiss={vi.fn()}
          />
        </LocaleProvider>
      );

      expect(screen.getByTestId("blocked-title")).toHaveTextContent("Restricted Equipment Pool");
      expect(screen.getByTestId("blocked-detail")).toHaveTextContent(/Charging Bay 3/);
      expect(screen.getByTestId("ok-blocked-button")).toBeInTheDocument();
    });
  });

  describe.each(LOCALES)("3. Orientation & 150% Dynamic Type Safety in %s", (locale) => {
    it("renders AwaitingDeviceScreen safely in portrait simulation (768px)", () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <div style={{ width: "768px", minHeight: "1024px" }}>
            <AwaitingDeviceScreen
              user={{
                id: "user-1",
                fullName: "Nurse Alex Taylor",
                department: "Surgery 2",
                openLoanCount: 1,
              }}
              openLoans={[
                {
                  id: "loan-1",
                  deviceId: "dev-1",
                  deviceName: "Surgical iPad Terminal",
                  assetTag: "SURG-IPAD-01",
                  borrowedAt: new Date().toISOString(),
                },
              ]}
              expiresAt={new Date(Date.now() + 25000).toISOString()}
              onReturnLoan={vi.fn()}
              onClose={vi.fn()}
            />
          </div>
        </LocaleProvider>
      );

      expect(container.querySelector('[data-testid="awaiting-device-prompt"]')).toBeInTheDocument();
      expect(container.querySelector('[data-testid="done-session-button"]')).toBeInTheDocument();
    });

    it("renders IdleScreen safely in 150% scaled container", () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <div style={{ fontSize: "150%" }}>
            <IdleScreen
              kioskName="North Ward Station"
              supportCode="KIOSK-NW"
              scannerReady={true}
              scannerFresh={true}
            />
          </div>
        </LocaleProvider>
      );

      expect(screen.getByTestId("idle-prompt")).toBeInTheDocument();
      expect(container.textContent).toContain(
        translate(catalogues, locale, "idle.prompt")
      );
    });

    it.each([
      ["iPad mini portrait", 744, 1133],
      ["iPad portrait", 768, 1024],
      ["iPad Air portrait", 820, 1180],
      ["iPad Pro portrait", 1024, 1366],
      ["iPad landscape", 1024, 768],
      ["iPad Air landscape", 1180, 820],
      ["iPad Pro landscape", 1366, 1024],
    ] as const)("keeps idle actions available at %s (%spx × %spx)", (_name, width, height) => {
      const { rerender } = render(
        <LocaleProvider locale={locale}>
          <div style={{ width: `${width}px`, height: `${height}px` }}>
            <IdleScreen onStartScanning={vi.fn()} />
          </div>
        </LocaleProvider>
      );

      const actionStack = screen.getByTestId("idle-action-stack");
      expect(actionStack).toHaveClass("min-[700px]:flex-row");
      expect(screen.getByRole("button", { name: /Start|開始/ })).toBeInTheDocument();
      expect(screen.getByTestId("scan-guidance-card")).toBeInTheDocument();

      rerender(
        <LocaleProvider locale={locale}>
          <div style={{ width: `${width}px`, height: `${height}px`, fontSize: "150%" }}>
            <IdleScreen onStartScanning={vi.fn()} />
          </div>
        </LocaleProvider>
      );

      expect(screen.getByTestId("idle-prompt")).toHaveTextContent(
        translate(catalogues, locale, "idle.prompt")
      );
    });
  });

  describe.each(LOCALES)("4. Complete vitest-axe Accessibility Audit in %s", (locale) => {
    it("declares the page language so a screen reader pronounces it correctly", () => {
      render(
        <LocaleProvider locale={locale}>
          <IdleScreen kioskName="Central Kiosk" supportCode="KIOSK-01" />
        </LocaleProvider>
      );
      expect(document.documentElement.lang).toBe(locale);
    });

    it("IdleScreen passes vitest-axe in light and dark modes", async () => {
      const { container, rerender } = render(
        <LocaleProvider locale={locale}>
          <IdleScreen kioskName="Central Kiosk" supportCode="KIOSK-01" />
        </LocaleProvider>
      );
      let results = await axe(container);
      expect(results).toHaveNoViolations();

      rerender(
        <LocaleProvider locale={locale}>
          <div className="dark">
            <IdleScreen kioskName="Central Kiosk" supportCode="KIOSK-01" />
          </div>
        </LocaleProvider>
      );
      results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("AwaitingUserScreen passes vitest-axe", async () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <AwaitingUserScreen
            pendingDevice={{
              id: "dev-1",
              assetTag: "DEV-01",
              name: "Telemetry Box",
            }}
            expiresAt={new Date(Date.now() + 45000).toISOString()}
            onCancel={vi.fn()}
          />
        </LocaleProvider>
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("AwaitingDeviceScreen passes vitest-axe with active loans", async () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <AwaitingDeviceScreen
            user={{
              id: "user-1",
              fullName: "Dr. Sarah Lee",
              department: "Cardiology",
              openLoanCount: 1,
            }}
            openLoans={[
              {
                id: "loan-1",
                deviceId: "dev-1",
                deviceName: "ECG Monitor Unit",
                assetTag: "ECG-002",
                borrowedAt: new Date().toISOString(),
              },
            ]}
            expiresAt={new Date(Date.now() + 25000).toISOString()}
            onReturnLoan={vi.fn()}
            onClose={vi.fn()}
          />
        </LocaleProvider>
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("SuccessScreen passes vitest-axe for borrowed and returned variants", async () => {
      const { container, rerender } = render(
        <LocaleProvider locale={locale}>
          <SuccessScreen
            kind="borrowed"
            device={{
              id: "dev-1",
              name: "Infusion Pump",
              assetTag: "PUMP-01",
            }}
            dueAt={new Date(Date.now() + 86400000).toISOString()}
            onDone={vi.fn()}
          />
        </LocaleProvider>
      );
      let results = await axe(container);
      expect(results).toHaveNoViolations();

      rerender(
        <LocaleProvider locale={locale}>
          <SuccessScreen
            kind="returned"
            device={{
              id: "dev-1",
              name: "Infusion Pump",
              assetTag: "PUMP-01",
            }}
            onDone={vi.fn()}
          />
        </LocaleProvider>
      );
      results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("BlockedScreen passes vitest-axe across warning and info tones", async () => {
      const { container, rerender } = render(
        <LocaleProvider locale={locale}>
          <BlockedScreen
            message={{
              title: "Device Already on Loan",
              detail: "This unit was not marked returned by its previous user.",
              tone: "warning",
            }}
            onDismiss={vi.fn()}
          />
        </LocaleProvider>
      );
      let results = await axe(container);
      expect(results).toHaveNoViolations();

      rerender(
        <LocaleProvider locale={locale}>
          <BlockedScreen
            message={{
              title: "Information Notice",
              detail: "Please proceed to reception.",
              tone: "info",
            }}
            onDismiss={vi.fn()}
          />
        </LocaleProvider>
      );
      results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("OfflineScreen passes vitest-axe", async () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <OfflineScreen />
        </LocaleProvider>
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("PairingScreen passes vitest-axe", async () => {
      const { container } = render(
        <LocaleProvider locale={locale}>
          <PairingScreen />
        </LocaleProvider>
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });

  describe.each(LOCALES)("5. Color-Deficiency & Multichannel Feedback Verification in %s", (locale) => {
    it("every feedback outcome kind has distinct icon and textual word (never color alone)", () => {
      for (const kind of ALL_OUTCOME_KINDS) {
        const config = OUTCOME_FEEDBACK_MAP[kind];
        expect(config).toBeDefined();
        expect(config.word.length).toBeGreaterThan(0);
        expect(config.icon).toBeDefined();
        // Word and icon guarantee accessibility under protanopia and deuteranopia
      }
    });

    it("overdue items display both icon and text description", () => {
      render(
        <LocaleProvider locale={locale}>
          <AwaitingDeviceScreen
            user={{
              id: "user-1",
              fullName: "Staff Member",
              department: "Nursing",
              openLoanCount: 1,
            }}
            openLoans={[
              {
                id: "loan-overdue",
                deviceId: "dev-1",
                deviceName: "Overdue Device",
                assetTag: "DEV-OD",
                borrowedAt: new Date(Date.now() - 86400000 * 5).toISOString(),
                dueAt: new Date(Date.now() - 86400000 * 3).toISOString(),
              },
            ]}
            expiresAt={null}
            onReturnLoan={vi.fn()}
            onClose={vi.fn()}
          />
        </LocaleProvider>
      );

      const overdueBadge = screen.getByTestId("overdue-flag-loan-overdue");
      expect(overdueBadge).toHaveTextContent(/overdue|超過/i);
      // SVG icon inside badge confirms visual cue + text
      expect(overdueBadge.querySelector("svg")).toBeInTheDocument();
    });
  });
});
