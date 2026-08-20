import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { axe } from "vitest-axe";
import { OfflineScreen } from "./offline-screen";

describe("OfflineScreen", () => {
  it("renders non-technical reconnecting message and passes axe", async () => {
    const { container } = render(
      <OfflineScreen
        kioskName="ICU Main Kiosk"
        supportCode="KIOSK-ICU-1"
      />
    );

    expect(screen.getByTestId("offline-title")).toHaveTextContent("Reconnecting to hospital network");
    expect(screen.getByTestId("offline-fallback-instruction")).toHaveTextContent(
      /paper register/i
    );
    expect(screen.getByTestId("kiosk-support-code")).toHaveTextContent("KIOSK-ICU-1");

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("offlineScreenHasNoTechnicalStrings", () => {
    const { container } = render(
      <OfflineScreen
        kioskName="Emergency Kiosk"
        supportCode="KIOSK-EMG-1"
      />
    );

    const renderedText = container.textContent ?? "";

    // Strictly assert NO technical keywords, status codes, URLs, or exception fragments
    expect(renderedText).not.toMatch(/https?:\/\//i);
    expect(renderedText).not.toMatch(/500|502|503|504|404|400|401|403/);
    expect(renderedText).not.toMatch(/stack|trace|exception|undefined|null|\[object Object\]/i);
    expect(renderedText).not.toMatch(/fetch|axios|network error|cors|socket|econnrefused/i);
  });
});
