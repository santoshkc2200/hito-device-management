import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
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

    expect(await screen.findByText("Issue #2")).toBeInTheDocument();
    expect(screen.getByText("Borrower Cards")).toBeInTheDocument();
    expect(screen.getByText("Issue #1")).toBeInTheDocument();
    expect(screen.getByText("…7K3M")).toBeInTheDocument();
    expect(screen.getByText("…4A2F")).toBeInTheDocument();

    // Revocation details
    expect(screen.getByText(/Badge lost during commute/)).toBeInTheDocument();
    expect(screen.getByText("Replaced by issue #2")).toBeInTheDocument();
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

    expect(await screen.findByText("No card issued")).toBeInTheDocument();
    expect(
      screen.getByText("This borrower cannot borrow devices until a credential is assigned.")
    ).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Bind blank card/i }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: /Issue (new )?card/i }).length).toBeGreaterThan(0);
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

    expect(await screen.findByText("All credentials have been revoked")).toBeInTheDocument();
    expect(
      screen.getByText(/This borrower has no working credentials. Issue a replacement card/)
    ).toBeInTheDocument();
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

    expect(await screen.findByText("Issue #1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Print$/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Reissue & print/i })).not.toBeInTheDocument();
  });

  it("renders 'Reissue & print' on user panel and never plain 'Print'", async () => {
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

    expect(await screen.findByText("Issue #2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Reissue & print/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Print$/i })).not.toBeInTheDocument();
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

    expect(await screen.findByText("Issue #2")).toBeInTheDocument();
    const reissueBtn = screen.getByRole("button", { name: /Reissue & print/i });
    await user.click(reissueBtn);

    // Dialog opens with consequences description
    expect(
      screen.getByText(/The current card in the borrower's pocket stops working immediately/)
    ).toBeInTheDocument();

    const submitBtn = screen.getByRole("button", { name: /Reissue & Print Card/i });
    expect(submitBtn).toBeDisabled();

    // Cancel branch
    const cancelBtn = screen.getByRole("button", { name: /Cancel/i });
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

    expect(await screen.findByText("Issue #2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Reissue & print/i }));

    const textarea = screen.getByPlaceholderText(/Lost in cafeteria/i);
    await user.type(textarea, "Card broken into two pieces");

    const submitBtn = screen.getByRole("button", { name: /Reissue & Print Card/i });
    expect(submitBtn).toBeEnabled();
    await user.click(submitBtn);

    await waitFor(() => {
      expect(apiClient.reissueCredential).toHaveBeenCalledWith({
        path: { id: "cred-2" },
        body: { reason: "Card broken into two pieces" },
      });
    });
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

    await screen.findByText("Borrower Cards");
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
