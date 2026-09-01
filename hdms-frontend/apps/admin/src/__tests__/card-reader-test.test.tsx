import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { cardReaderTestRoute } from "../routes/card-reader-test";

const CardReaderTestPage = cardReaderTestRoute.options.component!;

vi.mock("@hdms/api-client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@hdms/api-client")>();
  return {
    ...actual,
    resolveCredential: vi.fn(),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Link: ({ to, params, children, className }: any) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
  };
});

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

describe("Card Reader Diagnostic Tool (4.5d)", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = createTestQueryClient();
  });

  it("renders empty diagnostic state waiting for input", async () => {
    render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    expect(await screen.findByText(ja.cardReaderTest.title)).toBeInTheDocument();
    expect(screen.getByText(ja.cardReaderTest.waitingForScan)).toBeInTheDocument();
    expect(screen.getByText(ja.cardReaderTest.listenerActive)).toBeInTheDocument();
  });

  it("analyzes valid user token with Crockford Base32 breakdown and DB resolution", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.resolveCredential).mockResolvedValue({
      data: {
        type: "user",
        subjectId: "user-101",
        credentialId: "cred-user-101",
        credentialStatus: "active",
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    const input = screen.getByPlaceholderText(ja.cardReaderTest.inputPlaceholder);
    await user.type(input, "HD-U-B3G6822S6K-H");

    const analyzeBtn = screen.getByRole("button", { name: ja.cardReaderTest.analyzeString });
    await user.click(analyzeBtn);

    // Grammar & Checksum Analysis
    expect(await screen.findByText(ja.cardReaderTest.validToken)).toBeInTheDocument();
    expect(screen.getByText(ja.cardReaderTest.userStaffBadge)).toBeInTheDocument();
    expect(screen.getByText("B3G6822S6K")).toBeInTheDocument();
    expect(screen.getByText("H")).toBeInTheDocument();
    expect(screen.getAllByText("HD-U-B3G6822S6K-H").length).toBeGreaterThan(0);

    // DB Resolution
    expect(await screen.findByText(ja.cardReaderTest.viewBorrowerProfile)).toBeInTheDocument();
    expect(screen.getByText("cred-user-101")).toBeInTheDocument();
  });

  it("analyzes valid device token and shows Device Record link", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.resolveCredential).mockResolvedValue({
      data: {
        type: "device",
        subjectId: "dev-202",
        credentialId: "cred-dev-202",
        credentialStatus: "active",
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    const input = screen.getByPlaceholderText(ja.cardReaderTest.inputPlaceholder);
    await user.type(input, "HD-D-BRH8VFTBAA-7");
    await user.click(screen.getByRole("button", { name: ja.cardReaderTest.analyzeString }));

    expect(await screen.findByText(ja.cardReaderTest.deviceEquipmentLabel)).toBeInTheDocument();
    expect(await screen.findByText(ja.cardReaderTest.viewDeviceRecord)).toBeInTheDocument();
  });

  it("handles invalid checksum with specific diagnostic guidance", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    const input = screen.getByPlaceholderText(ja.cardReaderTest.inputPlaceholder);
    await user.type(input, "hd-u-b3g6822s6k-9");
    await user.click(screen.getByRole("button", { name: ja.cardReaderTest.analyzeString }));

    expect(await screen.findByText(ja.cardReaderTest.invalidToken)).toBeInTheDocument();
    expect(
      screen.getByText(
        translate(catalogues, "ja", "cardReaderTest.failureReason", { reason: "invalid-checksum" })
      )
    ).toBeInTheDocument();
    // The token-parsing guidance is produced by the shared domain package and is
    // not part of the admin i18n catalogue — it stays in English regardless of locale.
    expect(
      screen.getByText(/That code doesn't look right — check the last character/i)
    ).toBeInTheDocument();
  });

  it("captures keyboard HID wedge scanner keystrokes via global window listener", async () => {
    vi.mocked(apiClient.resolveCredential).mockResolvedValue({
      data: {
        type: "unbound",
        credentialId: "cred-unbound-1",
        credentialStatus: "active",
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    await screen.findByText(ja.cardReaderTest.waitingForScan);

    // Simulate rapid USB wedge keystrokes outside input focus
    const scanString = "HD-U-B3G6822S6K-H";
    for (const char of scanString) {
      fireEvent.keyDown(window, { key: char });
    }
    fireEvent.keyDown(window, { key: "Enter" });

    expect(await screen.findByText(ja.cardReaderTest.usbWedgeScanner)).toBeInTheDocument();
    expect(screen.getByText(ja.cardReaderTest.validToken)).toBeInTheDocument();
    expect(await screen.findByText(ja.cardReaderTest.readyToAssign)).toBeInTheDocument();
  });

  it("passes axe accessibility audit", async () => {
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <CardReaderTestPage />
      </QueryClientProvider>
    );

    await screen.findByText(ja.cardReaderTest.title);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
