import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { currentAdminQueryKey } from "@/lib/auth";
import { UserDetailPage, userDetailRoute } from "../routes/users.$userId";

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockParams = { userId: "user-1" };

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ to, params, children, className }: any) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
  };
});

vi.spyOn(userDetailRoute, "useParams").mockImplementation(() => mockParams as any);

const mockDepartments: apiClient.Department[] = [
  { id: "dept-1", name: "Emergency Department" },
  { id: "dept-2", name: "Cardiology" },
];

const mockAdminUser: apiClient.User = {
  id: "user-1",
  employeeNo: "HH-1001",
  fullName: "Dr. Taro Yamada",
  departmentId: "dept-1",
  email: "yamada@hito.local",
  phone: "090-1234-5678",
  notes: "Senior consultant physician",
  status: "active",
  registeredAt: "2026-08-01T09:00:00Z",
  registeredBy: "admin:admin-99",
  updatedAt: "2026-08-01T09:00:00Z",
};

const mockImportUser: apiClient.User = {
  id: "user-2",
  employeeNo: "HH-1002",
  fullName: "Nurse Hanako Sato",
  departmentId: "dept-2",
  email: "sato@hito.local",
  phone: "090-9876-5432",
  notes: "ICU staff",
  status: "active",
  registeredAt: "2026-08-02T10:00:00Z",
  registeredBy: "import:batch-42",
  updatedAt: "2026-08-02T10:00:00Z",
};

const mockImportNoBatchUser: apiClient.User = {
  id: "user-3",
  employeeNo: "HH-1003",
  fullName: "Staff Kenji Tanaka",
  departmentId: "dept-1",
  status: "active",
  registeredAt: "2026-08-03T11:00:00Z",
  registeredBy: "import",
  updatedAt: "2026-08-03T11:00:00Z",
};

const mockActiveCredential: apiClient.Credential = {
  id: "cred-1",
  subjectType: "user",
  subjectId: "user-1",
  kind: "qr",
  tokenPreview: "7X9AB",
  status: "active",
  issueSeq: 1,
  issuedAt: "2026-08-01T09:05:00Z",
  issuedBy: "admin:admin-99",
  printedCount: 1,
};

const mockRevokedCredential: apiClient.Credential = {
  id: "cred-0",
  subjectType: "user",
  subjectId: "user-1",
  kind: "qr",
  tokenPreview: "1A2BC",
  status: "revoked",
  issueSeq: 1,
  issuedAt: "2026-07-01T09:05:00Z",
  issuedBy: "admin:admin-99",
  revokedAt: "2026-08-01T09:00:00Z",
  revokedBy: "admin:admin-99",
  revokedReason: "Card lost in transit",
  printedCount: 1,
};

const mockLoans: apiClient.Loan[] = [
  {
    id: "loan-101",
    deviceId: "LAPTOP-07",
    userId: "user-1",
    status: "open",
    origin: "kiosk",
    borrowedAt: "2026-08-15T09:15:00Z",
    dueAt: "2026-08-18T18:00:00Z",
    borrowActor: "user-1",
    borrowSource: "hid_wedge",
    recordedAt: "2026-08-15T09:15:00Z",
    disputed: false,
  },
  {
    id: "loan-100",
    deviceId: "PROJECTOR-02",
    userId: "user-1",
    status: "returned",
    origin: "paper",
    borrowedAt: "2026-08-10T08:30:00Z",
    dueAt: "2026-08-10T17:00:00Z",
    returnedAt: "2026-08-10T16:45:00Z",
    borrowActor: "admin:admin-99",
    returnActor: "admin:admin-99",
    borrowSource: "manual_entry",
    returnSource: "manual_entry",
    paperRef: "2026-08-10 p.1",
    recordedAt: "2026-08-10T08:30:00Z",
    disputed: false,
  },
];

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

function renderUserDetailPage(role: "admin" | "technician" | "viewer" = "admin") {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(currentAdminQueryKey, {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role,
    status: "active",
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <UserDetailPage />
    </QueryClientProvider>
  );
}

describe("4.4c User Detail Page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockParams = { userId: "user-1" };

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "listDepartments").mockResolvedValue({
      data: { items: mockDepartments },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "getUser").mockResolvedValue({
      data: mockAdminUser,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listCredentialsBySubject").mockResolvedValue({
      data: { items: [mockActiveCredential] },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listUserLoans").mockResolvedValue({
      data: { items: mockLoans, nextCursor: undefined },
      error: undefined,
    } as any);
  });

  it("renders profile, resolved department, and details accurately", async () => {
    renderUserDetailPage("admin");

    expect(await screen.findByRole("heading", { name: "Dr. Taro Yamada" })).toBeInTheDocument();
    expect(screen.getAllByText("HH-1001").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("Emergency Department").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("yamada@hito.local")).toBeInTheDocument();
    expect(screen.getByText("090-1234-5678")).toBeInTheDocument();
    expect(screen.getByText("Senior consultant physician")).toBeInTheDocument();
  });

  it("renders provenance for an admin-registered user", async () => {
    renderUserDetailPage("admin");

    expect(await screen.findByText(ja.userDetail.provenanceHeading)).toBeInTheDocument();
    expect(
      screen.getByText(
        translate(catalogues, "ja", "userDetail.provenanceAdmin", { adminId: "admin-99" })
      )
    ).toBeInTheDocument();
  });

  it("renders provenance for an import-created user with batch ID", async () => {
    mockParams = { userId: "user-2" };
    vi.spyOn(apiClient, "getUser").mockResolvedValueOnce({
      data: mockImportUser,
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    expect(await screen.findByRole("heading", { name: "Nurse Hanako Sato" })).toBeInTheDocument();
    expect(
      screen.getByText(
        translate(catalogues, "ja", "userDetail.provenanceImportBatch", { batchId: "batch-42" })
      )
    ).toBeInTheDocument();
  });

  it("renders provenance for a legacy/plain import user", async () => {
    mockParams = { userId: "user-3" };
    vi.spyOn(apiClient, "getUser").mockResolvedValueOnce({
      data: mockImportNoBatchUser,
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    expect(await screen.findByRole("heading", { name: "Staff Kenji Tanaka" })).toBeInTheDocument();
    expect(screen.getByText(ja.userDetail.provenanceCsvImport)).toBeInTheDocument();
  });

  it("shows 'no card issued' badge and no issue or bind action when user has no credentials", async () => {
    vi.spyOn(apiClient, "listCredentialsBySubject").mockResolvedValue({
      data: { items: [] },
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    // The header badge and the CredentialsPanel empty-state heading both read
    // "カード未発行" in Japanese (distinct wording in English, identical here).
    expect((await screen.findAllByText(ja.userDetail.noCardIssued)).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText(ja.credentialsPanel.noCardIssuedHint)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /発行|紐付/ })).not.toBeInTheDocument();
  });

  it("shows revoked state warning when all credentials are revoked", async () => {
    vi.spyOn(apiClient, "listCredentialsBySubject").mockResolvedValue({
      data: { items: [mockRevokedCredential] },
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    expect(await screen.findByText(ja.credentialsPanel.allRevoked)).toBeInTheDocument();
  });

  it("renders currently held devices with links and overdue badge if overdue", async () => {
    renderUserDetailPage("admin");

    expect(await screen.findByText(ja.userDetail.heldDevicesHeading)).toBeInTheDocument();
    expect(
      screen.getByText(
        translate(catalogues, "ja", "userDetail.devicePrefix", { deviceId: "LAPTOP-07" })
      )
    ).toBeInTheDocument();
    expect(screen.getByText(ja.userDetail.viewLoan)).toBeInTheDocument();
    expect(screen.getByText(ja.userDetail.overdue)).toBeInTheDocument();
  });

  it("renders loan history with status, origin badges, and links", async () => {
    renderUserDetailPage("admin");

    expect(await screen.findByText(ja.userDetail.historyHeading)).toBeInTheDocument();
    expect(screen.getByText("LAPTOP-07")).toBeInTheDocument();
    expect(screen.getByText("PROJECTOR-02")).toBeInTheDocument();
    // loan.origin is rendered raw (kiosk/paper), not translated through the catalogue.
    expect(screen.getByText("kiosk")).toBeInTheDocument();
    expect(screen.getByText("paper")).toBeInTheDocument();
    expect(screen.getAllByText(ja.userDetail.view).length).toBeGreaterThanOrEqual(1);
  });

  it("performs borrower suspend with mandatory reason", async () => {
    const suspendSpy = vi.spyOn(apiClient, "suspendUser").mockResolvedValueOnce({
      data: { ...mockAdminUser, status: "suspended" },
      error: undefined,
    } as any);

    renderUserDetailPage("technician");

    expect(await screen.findByRole("heading", { name: "Dr. Taro Yamada" })).toBeInTheDocument();

    const user = userEvent.setup();
    const suspendButton = screen.getByRole("button", { name: ja.userDetail.suspend });
    await user.click(suspendButton);

    expect(screen.getByRole("heading", { name: ja.userDetail.suspendTitle })).toBeInTheDocument();
    await user.type(screen.getByPlaceholderText(ja.userDetail.reasonPlaceholder), "Misplaced hospital ID");
    await user.click(screen.getByRole("button", { name: ja.userDetail.suspend }));

    await waitFor(() => {
      expect(suspendSpy).toHaveBeenCalledWith({
        path: { id: "user-1" },
        body: { reason: "Misplaced hospital ID" },
      });
    });
  });

  it("performs borrower archive with mandatory reason and redirects to /users", async () => {
    const archiveSpy = vi.spyOn(apiClient, "archiveUser").mockResolvedValueOnce({
      data: { ...mockAdminUser, status: "archived" },
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    expect(await screen.findByRole("heading", { name: "Dr. Taro Yamada" })).toBeInTheDocument();

    const user = userEvent.setup();
    const archiveButton = screen.getByRole("button", { name: ja.userDetail.archive });
    await user.click(archiveButton);

    expect(screen.getByRole("heading", { name: ja.userDetail.archiveTitle })).toBeInTheDocument();
    await user.type(
      screen.getByPlaceholderText(ja.userDetail.reasonPlaceholder),
      "Doctor relocated to new hospital"
    );
    await user.click(screen.getByRole("button", { name: ja.userDetail.archive }));

    await waitFor(() => {
      expect(archiveSpy).toHaveBeenCalledWith({
        path: { id: "user-1" },
        body: { reason: "Doctor relocated to new hospital" },
      });
      expect(mockNavigate).toHaveBeenCalledWith({ to: "/users" });
    });
  });

  it("enforces role gating: viewer cannot edit, suspend, or archive", async () => {
    renderUserDetailPage("viewer");

    expect(await screen.findByRole("heading", { name: "Dr. Taro Yamada" })).toBeInTheDocument();

    expect(screen.queryByRole("button", { name: /^edit$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^suspend$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^archive$/i })).not.toBeInTheDocument();
  });

  it("passes accessibility check with axe", async () => {
    const { container } = renderUserDetailPage("admin");
    expect(await screen.findByRole("heading", { name: "Dr. Taro Yamada" })).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("renders reservation history sub-list", async () => {
    vi.spyOn(apiClient, "listUserReservations").mockResolvedValueOnce({
      data: {
        items: [
          {
            id: "res-user-1",
            deviceId: "dev-ultra",
            userId: "user-1",
            deviceName: "Ultrasound Probe",
            deviceAssetTag: "US-01",
            userName: "Dr. Taro Yamada",
            userEmployeeNo: "HH-1001",
            status: "active",
            startAt: "2026-09-25T09:00:00Z",
            endAt: "2026-09-25T17:00:00Z",
            createdBy: "admin:1",
            createdSource: "admin",
            createdAt: "2026-09-18T10:00:00Z",
            updatedAt: "2026-09-18T10:00:00Z",
          },
        ],
        nextCursor: undefined,
      },
      error: undefined,
    } as any);

    renderUserDetailPage("admin");

    await waitFor(() => {
      expect(screen.getByTestId("user-reservations-card")).toBeInTheDocument();
    });

    expect(screen.getByText("Ultrasound Probe")).toBeInTheDocument();
  });

  it("lets an admin issue a staff login password and shows it once", async () => {
    const reset = vi.spyOn(apiClient, "resetStaffPassword").mockResolvedValue({
      data: { temporaryPassword: "Tmp9Pass8Word7Xy" },
      error: undefined,
    } as any);
    const user = userEvent.setup();

    renderUserDetailPage("admin");

    await user.click(await screen.findByRole("button", { name: ja.userDetail.staffPassword }));
    expect(reset).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: ja.userDetail.staffPasswordConfirm }));

    expect(await screen.findByText("Tmp9Pass8Word7Xy")).toBeInTheDocument();
    expect(reset).toHaveBeenCalledWith({ path: { id: "user-1" } });

    await user.click(screen.getByRole("button", { name: ja.userDetail.staffPasswordDone }));
    await waitFor(() => {
      expect(screen.queryByText("Tmp9Pass8Word7Xy")).not.toBeInTheDocument();
    });
  });

  it("hides the staff login action from technicians", async () => {
    renderUserDetailPage("technician");

    expect(await screen.findByRole("button", { name: ja.userDetail.edit })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: ja.userDetail.staffPassword })).not.toBeInTheDocument();
  });
});
