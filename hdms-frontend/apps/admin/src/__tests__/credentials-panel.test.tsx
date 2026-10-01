import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { LocaleProvider, translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { CredentialsPanel } from "../components/credentials-panel";

vi.mock("@hdms/api-client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@hdms/api-client")>();
  return {
    ...actual,
    listCredentialsBySubject: vi.fn(),
    getCredentialHistory: vi.fn(),
    issueCredential: vi.fn(),
    reprintCredential: vi.fn(),
    revokeCredential: vi.fn(),
    reissueCredential: vi.fn(),
    bindCredential: vi.fn(),
    resolveCredential: vi.fn(),
    revealCredential: vi.fn(),
  };
});

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

describe("CredentialsPanel (4.5a, 4.5b)", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = createTestQueryClient();
  });

  const mockUserCredentials: apiClient.Credential[] = [
    {
      id: "cred-2",
      subjectType: "user",
      subjectId: "user-1",
      kind: "qr",
      tokenPreview: "7K3M",
      status: "active",
      issueSeq: 2,
      replacesId: "cred-1",
      issuedAt: "2026-08-10T10:00:00Z",
      issuedBy: "admin:admin-1",
      printedCount: 1,
      lastPrintedAt: "2026-08-10T10:00:00Z",
    },
    {
      id: "cred-1",
      subjectType: "user",
      subjectId: "user-1",
      kind: "qr",
      tokenPreview: "4A2F",
      status: "revoked",
      issueSeq: 1,
      issuedAt: "2026-08-01T09:00:00Z",
      issuedBy: "admin:admin-1",
      revokedAt: "2026-08-10T09:55:00Z",
      revokedBy: "admin:admin-1",
      revokedReason: "Badge lost during commute",
      printedCount: 1,
      lastPrintedAt: "2026-08-01T09:00:00Z",
    },
  ];

  const mockDeviceCredentials: apiClient.Credential[] = [
    {
      id: "cred-dev-1",
      subjectType: "device",
      subjectId: "dev-1",
      kind: "qr",
      tokenPreview: "8N4P",
      status: "active",
      issueSeq: 1,
      issuedAt: "2026-08-01T09:00:00Z",
      issuedBy: "admin:admin-1",
      printedCount: 3,
      lastPrintedAt: "2026-08-05T14:00:00Z",
    },
  ];

  it("renders active and historical credentials with issue sequence, tokens, and reasons", async () => {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 2 }))
    ).toBeInTheDocument();
    expect(screen.getByText(ja.credentialsPanel.borrowerCardsHeading)).toBeInTheDocument();
    expect(
      screen.getByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 1 }))
    ).toBeInTheDocument();
    expect(screen.getByText("…7K3M")).toBeInTheDocument();
    expect(screen.getByText("…4A2F")).toBeInTheDocument();

    // Revocation details
    expect(screen.getByText(/Badge lost during commute/)).toBeInTheDocument();
    expect(
      screen.getByText(translate(catalogues, "ja", "credentialsPanel.replacedBySeq", { seq: 2 }))
    ).toBeInTheDocument();
  });

  it("distinguishes 'No card issued' empty state for user with bind and issue buttons", async () => {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: [] },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(await screen.findByText(ja.credentialsPanel.noCardIssued)).toBeInTheDocument();
    expect(screen.getByText(ja.credentialsPanel.noCardIssuedHint)).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: ja.credentialsPanel.bindBlankCard }).length
    ).toBeGreaterThan(0);
    expect(
      screen.getAllByRole("button", { name: new RegExp(escapeRegExp(ja.credentialsPanel.issueCard)) })
        .length
    ).toBeGreaterThan(0);
  });

  it("distinguishes 'All credentials have been revoked' warning state", async () => {
    const allRevokedUserCreds = [mockUserCredentials[1]]; // Only revoked cred-1

    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: allRevokedUserCreds },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(await screen.findByText(ja.credentialsPanel.allRevoked)).toBeInTheDocument();
    expect(screen.getByText(ja.credentialsPanel.allRevokedHintUser)).toBeInTheDocument();
  });

  it("renders 'Print' button on device panel and never 'Reissue & print'", async () => {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockDeviceCredentials },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="device"
          subjectId="dev-1"
          subject={{
            type: "device",
            assetTag: "LAPTOP-07",
            name: "Dell Latitude 5420",
            model: "Dell Latitude",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 1 }))
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: ja.credentialsPanel.reprint })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: ja.credentialsPanel.reportLost })
    ).not.toBeInTheDocument();
  });

  it("renders 'View QR' and 'Report lost' on user panel and never plain 'Print'", async () => {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 2 }))
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: ja.credentialsPanel.viewQr })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: ja.credentialsPanel.reportLost })
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: ja.credentialsPanel.reprint })
    ).not.toBeInTheDocument();
  });

  it("destructive flow requires mandatory reason and cancel leaves state untouched", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 2 }))
    ).toBeInTheDocument();
    const reissueBtn = screen.getByRole("button", { name: ja.credentialsPanel.reportLost });
    await user.click(reissueBtn);

    // Dialog opens with consequences description
    expect(
      screen.getByText(ja.credentialsPanel.reissueConfirmDescriptionUser)
    ).toBeInTheDocument();

    const submitBtn = screen.getByRole("button", {
      name: ja.credentialsPanel.reissueConfirmButtonUser,
    });
    expect(submitBtn).toBeDisabled();

    // Cancel branch
    const cancelBtn = screen.getByRole("button", { name: ja.common.cancel });
    await user.click(cancelBtn);

    expect(apiClient.reissueCredential).not.toHaveBeenCalled();
  });

  it("submits reissue when reason is provided", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);
    vi.mocked(apiClient.reissueCredential).mockResolvedValue({
      data: {
        id: "cred-3",
        token: "HD-U-9999999999-K",
        issueSeq: 3,
        status: "active",
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 2 }))
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: ja.credentialsPanel.reportLost }));

    const textarea = screen.getByPlaceholderText(ja.credentialsPanel.reasonPlaceholder);
    await user.type(textarea, "Card broken into two pieces");

    const submitBtn = screen.getByRole("button", {
      name: ja.credentialsPanel.reissueConfirmButtonUser,
    });
    expect(submitBtn).toBeEnabled();
    await user.click(submitBtn);

    await waitFor(() => {
      expect(apiClient.reissueCredential).toHaveBeenCalledWith({
        path: { id: "cred-2" },
        body: { reason: "Card broken into two pieces" },
      });
    });
  });

  it("destructive revoke flow requires mandatory reason and supports cancel", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    expect(
      await screen.findByText(translate(catalogues, "ja", "credentialsPanel.issueSeq", { seq: 2 }))
    ).toBeInTheDocument();
    const revokeBtn = screen.getByRole("button", { name: ja.credentialsPanel.revokeAction });
    await user.click(revokeBtn);

    // Revoke dialog opens
    expect(screen.getByText(ja.credentialsPanel.revokeConfirmTitle)).toBeInTheDocument();
    const submitBtn = screen.getByRole("button", {
      name: ja.credentialsPanel.revokeConfirmButton,
    });
    expect(submitBtn).toBeDisabled();

    // Cancel branch
    const cancelBtn = screen.getByRole("button", { name: ja.common.cancel });
    await user.click(cancelBtn);
    expect(apiClient.revokeCredential).not.toHaveBeenCalled();
  });

  function renderPanel({
    subjectType,
    credentials,
  }: {
    subjectType: "user" | "device";
    credentials: apiClient.Credential[];
  }) {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: credentials },
      error: undefined,
    } as any);

    const subject =
      subjectType === "user"
        ? {
            type: "user" as const,
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }
        : {
            type: "device" as const,
            assetTag: "TAG-001",
            name: "Infusion Pump",
            model: "IP-1000",
          };

    return render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <CredentialsPanel
            subjectType={subjectType}
            subjectId={subjectType === "user" ? "user-1" : "dev-1"}
            subject={subject}
          />
        </QueryClientProvider>
      </LocaleProvider>
    );
  }

  it("offers view and print on a user card and no reissue-to-reprint", async () => {
    renderPanel({ subjectType: "user", credentials: [mockUserCredentials[0]] });

    expect(await screen.findByRole("button", { name: /view qr/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^reissue$/i })).not.toBeInTheDocument();
    // The lost-card path survives: it is the only way to invalidate a card
    // someone can no longer find (ADR-0016).
    expect(screen.getByRole("button", { name: /report lost/i })).toBeInTheDocument();
  });

  it("keeps reissue and reprint on a device credential", async () => {
    renderPanel({ subjectType: "device", credentials: [mockDeviceCredentials[0]] });

    expect(await screen.findByRole("button", { name: /reissue/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /reprint/i })).toBeInTheDocument();
  });

  it("offers no issue button on a device panel, even when it has no credentials", async () => {
    renderPanel({ subjectType: "device", credentials: [] });

    expect(await screen.findByText(/no credential issued/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /issue/i })).not.toBeInTheDocument();
  });

  it("passes axe accessibility checks", async () => {
    vi.mocked(apiClient.listCredentialsBySubject).mockResolvedValue({
      data: { items: mockUserCredentials },
      error: undefined,
    } as any);

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPanel
          subjectType="user"
          subjectId="user-1"
          subject={{
            type: "user",
            fullName: "Dr. Taro Yamada",
            employeeNo: "HH-1001",
            department: "Emergency Department",
          }}
        />
      </QueryClientProvider>
    );

    await screen.findByText(ja.credentialsPanel.borrowerCardsHeading);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
