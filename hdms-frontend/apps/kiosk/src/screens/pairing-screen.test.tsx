import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { axe } from "vitest-axe";
import { PairingScreen } from "./pairing-screen";
import * as apiClient from "@hdms/api-client";
import { getKioskConfig, clearKioskConfig } from "@/lib/kiosk-config";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof apiClient>("@hdms/api-client");
  return {
    ...actual,
    pairKiosk: vi.fn(),
  };
});

describe("PairingScreen", () => {
  beforeEach(() => {
    clearKioskConfig();
    vi.clearAllMocks();
  });

  it("renders keypad and passes axe accessibility audit", async () => {
    const { container } = render(<PairingScreen />);

    expect(screen.getByTestId("pairing-title")).toHaveTextContent(
      "This iPad is not yet paired"
    );
    expect(screen.getByTestId("pairing-keypad")).toBeInTheDocument();
    expect(screen.getByTestId("pairing-submit-button")).toBeDisabled();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("enters 6-digit code via keypad and enables submit button", async () => {
    const user = userEvent.setup();
    render(<PairingScreen />);

    const submitBtn = screen.getByTestId("pairing-submit-button");
    expect(submitBtn).toBeDisabled();

    // Click digits 1 2 3 4 5 6
    await user.click(screen.getByTestId("pairing-key-1"));
    await user.click(screen.getByTestId("pairing-key-2"));
    await user.click(screen.getByTestId("pairing-key-3"));
    await user.click(screen.getByTestId("pairing-key-4"));
    await user.click(screen.getByTestId("pairing-key-5"));
    await user.click(screen.getByTestId("pairing-key-6"));

    expect(screen.getByTestId("pairing-digit-0")).toHaveTextContent("1");
    expect(screen.getByTestId("pairing-digit-5")).toHaveTextContent("6");
    expect(submitBtn).toBeEnabled();

    // Test backspace
    await user.click(screen.getByTestId("pairing-key-backspace"));
    expect(screen.getByTestId("pairing-digit-5")).toHaveTextContent("");
    expect(submitBtn).toBeDisabled();

    // Re-add digit 6
    await user.click(screen.getByTestId("pairing-key-6"));
    expect(submitBtn).toBeEnabled();

    // Test clear
    await user.click(screen.getByTestId("pairing-key-clear"));
    expect(screen.getByTestId("pairing-digit-0")).toHaveTextContent("");
    expect(submitBtn).toBeDisabled();
  });

  it("pairingExchangeStoresTokenAndEntersIdle on successful code submission", async () => {
    const user = userEvent.setup();
    const onPaired = vi.fn();

    vi.mocked(apiClient.pairKiosk).mockResolvedValueOnce({
      data: {
        kioskId: "kiosk-north-01",
        name: "North Wing Kiosk",
        token: "secret-bearer-token-123",
      },
      error: undefined,
      request: new Request("http://localhost/v1/kiosks/pair"),
      response: new Response(JSON.stringify({}), { status: 200 }),
    });

    render(<PairingScreen onPaired={onPaired} />);

    // Type 6 digits
    for (let i = 1; i <= 6; i++) {
      await user.click(screen.getByTestId(`pairing-key-${i}`));
    }

    const submitBtn = screen.getByTestId("pairing-submit-button");
    await user.click(submitBtn);

    await waitFor(() => {
      expect(apiClient.pairKiosk).toHaveBeenCalledWith({
        body: { code: "123456" },
      });
    });

    // Token and kiosk info must be stored in kiosk config
    const storedConfig = getKioskConfig();
    expect(storedConfig?.kioskId).toBe("kiosk-north-01");
    expect(storedConfig?.kioskName).toBe("North Wing Kiosk");
    expect(storedConfig?.token).toBe("secret-bearer-token-123");

    expect(onPaired).toHaveBeenCalledWith({
      kioskId: "kiosk-north-01",
      kioskName: "North Wing Kiosk",
    });
  });

  it("displays error message and support code on invalid pairing code", async () => {
    const user = userEvent.setup();

    vi.mocked(apiClient.pairKiosk).mockResolvedValueOnce({
      data: undefined,
      error: {
        type: "https://api.hdms.hito.internal/problems/pairing-code-invalid",
        title: "Invalid Pairing Code",
        status: 400,
        detail: "The pairing code is invalid or has expired.",
        requestId: "REQ-PAIR-ERR-99",
      } as any,
      request: new Request("http://localhost/v1/kiosks/pair"),
      response: new Response(JSON.stringify({}), { status: 400 }),
    });

    render(<PairingScreen />);

    // Type 6 digits
    for (let i = 0; i < 6; i++) {
      await user.click(screen.getByTestId("pairing-key-9"));
    }

    await user.click(screen.getByTestId("pairing-submit-button"));

    await waitFor(() => {
      expect(screen.getByTestId("pairing-error-message")).toBeInTheDocument();
    });

    expect(screen.getByTestId("pairing-error-message")).toHaveTextContent(
      "The pairing code is invalid or has expired."
    );
    expect(screen.getByTestId("pairing-error-message")).toHaveTextContent(
      "REQ-PAIR-ERR-99"
    );

    // Kiosk config must remain null
    expect(getKioskConfig()).toBeNull();
  });
});
