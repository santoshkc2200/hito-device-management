import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { credentialsRoute } from "../routes/credentials";

const CredentialsPage = credentialsRoute.options.component!;

vi.mock("@hdms/api-client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@hdms/api-client")>();
  return {
    ...actual,
    getUnboundCredentialCount: vi.fn(),
    issueBlankBatch: vi.fn(),
    listUsers: vi.fn(),
    resolveCredential: vi.fn(),
    bindCredential: vi.fn(),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Link: ({ to, children, className }: any) => (
      <a href={to} className={className}>
        {children}
      </a>
    ),
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

describe("Blank Card Stock & Quick Bind (4.5c)", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = createTestQueryClient();
  });

  it("displays unbound drawer count and healthy status when stock >= 10", async () => {
    vi.mocked(apiClient.getUnboundCredentialCount).mockResolvedValue({
      data: { count: 25 },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPage />
      </QueryClientProvider>
    );

    expect(await screen.findByText("25")).toBeInTheDocument();
    expect(screen.getByText(ja.credentials.cardsReady)).toBeInTheDocument();
    expect(screen.queryByText(ja.credentials.lowStockWarning)).not.toBeInTheDocument();
  });

  it("displays running low warning when unbound stock < 10", async () => {
    vi.mocked(apiClient.getUnboundCredentialCount).mockResolvedValue({
      data: { count: 4 },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPage />
      </QueryClientProvider>
    );

    expect(await screen.findByText("4")).toBeInTheDocument();
    expect(screen.getByText(ja.credentials.lowStockWarning)).toBeInTheDocument();
  });

  it("mints batch and transitions to sheet printing view", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.getUnboundCredentialCount).mockResolvedValue({
      data: { count: 12 },
      error: undefined,
    } as any);
    vi.mocked(apiClient.issueBlankBatch).mockResolvedValue({
      data: {
        items: [
          { id: "c-1", token: "HD-U-1111111111-1" },
          { id: "c-2", token: "HD-U-2222222222-2" },
        ],
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPage />
      </QueryClientProvider>
    );

    await screen.findByText(ja.credentials.title);
    const mintBtn = screen.getByRole("button", {
      name: translate(catalogues, "ja", "credentials.mintAndPrint", { count: 10 }),
    });
    await user.click(mintBtn);

    expect(await screen.findByText(ja.credentials.batchTitle)).toBeInTheDocument();
    expect(screen.getByText("HD-U-1111111111-1")).toBeInTheDocument();
    expect(screen.getByText("HD-U-2222222222-2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: ja.credentials.printSheet })).toBeInTheDocument();

    // Click Done to return
    await user.click(screen.getByRole("button", { name: ja.credentials.done }));
    expect(screen.getByText(ja.credentials.title)).toBeInTheDocument();
  });

  it("binds an unbound blank card to an existing borrower", async () => {
    const user = userEvent.setup();
    vi.mocked(apiClient.getUnboundCredentialCount).mockResolvedValue({
      data: { count: 15 },
      error: undefined,
    } as any);
    vi.mocked(apiClient.listUsers).mockResolvedValue({
      data: {
        items: [
          {
            id: "user-42",
            employeeNo: "EMP-4200",
            fullName: "Nurse Clara Barton",
            hasCredential: false,
          },
        ],
      },
      error: undefined,
    } as any);
    vi.mocked(apiClient.resolveCredential).mockResolvedValue({
      data: {
        type: "unbound",
        credentialId: "cred-unbound-99",
        credentialStatus: "active",
      },
      error: undefined,
    } as any);
    vi.mocked(apiClient.bindCredential).mockResolvedValue({
      data: {
        id: "cred-unbound-99",
        subjectId: "user-42",
        status: "active",
      },
      error: undefined,
    } as any);

    render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPage />
      </QueryClientProvider>
    );

    await screen.findByText(ja.credentials.quickBindTitle);

    // Search user
    const searchInput = screen.getByPlaceholderText(ja.credentials.userSearchPlaceholder);
    await user.type(searchInput, "Clara");

    const userOption = await screen.findByText("Nurse Clara Barton");
    await user.click(userOption);

    expect(
      screen.getByText(
        new RegExp(`${escapeRegExp(ja.credentials.selectedPrefix)} Nurse Clara Barton`)
      )
    ).toBeInTheDocument();

    // Enter token
    const tokenInput = screen.getByPlaceholderText(ja.credentials.tokenPlaceholder);
    await user.type(tokenInput, "HD-U-B3G6822S6K-H");

    const bindBtn = screen.getByRole("button", { name: ja.credentials.bindAndActivate });
    expect(bindBtn).toBeEnabled();
    await user.click(bindBtn);

    await waitFor(() => {
      expect(apiClient.resolveCredential).toHaveBeenCalledWith({
        query: { token: "HD-U-B3G6822S6K-H" },
      });
      expect(apiClient.bindCredential).toHaveBeenCalledWith({
        path: { id: "cred-unbound-99" },
        body: { subjectId: "user-42" },
      });
    });

    expect(
      await screen.findByText(
        translate(catalogues, "ja", "credentials.boundSuccess", {
          user: "Nurse Clara Barton (EMP-4200)",
        })
      )
    ).toBeInTheDocument();
  });

  it("passes axe accessibility audit", async () => {
    vi.mocked(apiClient.getUnboundCredentialCount).mockResolvedValue({
      data: { count: 18 },
      error: undefined,
    } as any);

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <CredentialsPage />
      </QueryClientProvider>
    );

    await screen.findByText(ja.credentials.title);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
