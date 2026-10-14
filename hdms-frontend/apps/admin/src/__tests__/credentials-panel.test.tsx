import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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


  it("shows a permanent employee code without issuing, revoking or regenerating credentials", async () => {
    renderPanel({ subjectType: "user", credentials: [] });
    expect(await screen.findByText("HH-1001")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /reissue|revoke|report lost|add employee/i })).not.toBeInTheDocument();
    expect(apiClient.listCredentialsBySubject).not.toHaveBeenCalled();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /view qr/i }));
    expect(await screen.findByTestId("revealed-token")).toHaveTextContent("HH-1001");
    expect(apiClient.revealCredential).not.toHaveBeenCalled();
    expect(apiClient.issueCredential).not.toHaveBeenCalled();
    expect(apiClient.reissueCredential).not.toHaveBeenCalled();
    expect(apiClient.revokeCredential).not.toHaveBeenCalled();
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
