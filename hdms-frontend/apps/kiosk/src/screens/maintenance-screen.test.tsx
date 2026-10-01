import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { MaintenanceScreen } from "./maintenance-screen";

describe("MaintenanceScreen", () => {
  it("tells staff to use the paper register and that it comes back by itself", async () => {
    const { container } = render(
      <LocaleProvider locale="en">
        <MaintenanceScreen kioskName="Ward 3" supportCode="KIOSK-W3" />
      </LocaleProvider>,
    );

    expect(screen.getByTestId("maintenance-title")).toHaveTextContent("Under maintenance");
    expect(screen.getByTestId("maintenance-subtitle")).toHaveTextContent(/resume by itself/i);
    expect(screen.getByTestId("maintenance-paper-instruction")).toHaveTextContent(/paper register/i);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("shows no technical strings", () => {
    const { container } = render(<MaintenanceScreen />);
    const text = container.textContent ?? "";
    expect(text).not.toMatch(/503|maintenance_|restore_|https?:\/\//i);
  });

  it("speaks Japanese", () => {
    render(
      <LocaleProvider locale="ja">
        <MaintenanceScreen />
      </LocaleProvider>,
    );
    expect(screen.getByTestId("maintenance-title")).toHaveTextContent("ただいまメンテナンス中です");
  });
});
