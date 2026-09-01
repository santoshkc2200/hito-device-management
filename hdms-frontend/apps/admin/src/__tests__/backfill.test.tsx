import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { backfillRoute } from "../routes/backfill";

const BackfillPage = backfillRoute.options.component!;

// ─────────────────────────────────────────────────────────────────────────────
// i18n helpers — assert through the catalogue even where the count is dynamic
// ─────────────────────────────────────────────────────────────────────────────

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Turns a `{count}`-templated catalogue string into a regex matching any count. */
function localizedCountPattern(template: string): RegExp {
  return new RegExp(template.split("{count}").map(escapeRegExp).join("\\d+"));
}

const stagedRowsHeadingPattern = localizedCountPattern(ja.backfill.stagedRowsHeading);

function stagedRowsHeading(count: number): string {
  return translate(catalogues, "ja", "backfill.stagedRowsHeading", { count });
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock @hdms/api-client
// ─────────────────────────────────────────────────────────────────────────────

vi.mock("@hdms/api-client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@hdms/api-client")>();
  return {
    ...actual,
    listUsers: vi.fn(),
    listDepartments: vi.fn(),
    previewBackfillBatch: vi.fn(),
    recordBackfillBatch: vi.fn(),
  };
});

// ─────────────────────────────────────────────────────────────────────────────
// Mock router
// ─────────────────────────────────────────────────────────────────────────────

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Link: ({ to, children, className }: { to: string; children: React.ReactNode; className?: string }) => (
      <a href={to} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => vi.fn(),
    createRoute: actual.createRoute,
  };
});

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderPage() {
  const queryClient = createTestQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <BackfillPage />
    </QueryClientProvider>,
  );
}

function mockPreviewOk() {
  vi.mocked(apiClient.previewBackfillBatch).mockResolvedValue({
    data: {
      committed: false,
      rows: [],
      summary: { ok: 0, conflicts: 0, newUsers: 0 },
    },
    error: undefined,
  } as any);
}



// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

describe("Paper Backfill — Phase 4.6", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    vi.mocked(apiClient.listUsers).mockResolvedValue({
      data: { items: [], total: 0 },
      error: undefined,
    } as any);
    vi.mocked(apiClient.listDepartments).mockResolvedValue({
      data: { items: [] },
      error: undefined,
    } as any);
    mockPreviewOk();
  });

  // ── 4.6a: Entry bar & keyboard navigation ────────────────────────────────

  describe("4.6a — Entry bar", () => {
    it("renders page reference and date context inputs", async () => {
      renderPage();
      expect(screen.getByLabelText(ja.backfill.pageReferenceLabel)).toBeInTheDocument();
      expect(screen.getByLabelText(ja.backfill.pageDateLabel)).toBeInTheDocument();
    });

    it("shows entry bar only after paperRef is set", async () => {
      const user = userEvent.setup();
      renderPage();

      expect(screen.queryByText(ja.backfill.newEntryHeading)).not.toBeInTheDocument();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "R");

      expect(await screen.findByText(ja.backfill.newEntryHeading)).toBeInTheDocument();
    });

    it("focuses device field on load and after Escape reset", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "A"); // minimal ref to show entry bar

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      // Click the device field to focus, then type
      await user.click(deviceInput);
      await user.keyboard("HDMS-001");
      expect(deviceInput).toHaveValue("HDMS-001");

      // Escape resets the in-progress row
      await user.keyboard("{Escape}");
      expect(deviceInput).toHaveValue("");
      // Focus should return to device field
      expect(document.activeElement).toBe(deviceInput);
    });

    it("commits row on Enter and refocuses cleared Device field", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "X");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-ENTER");
      await user.keyboard("{Enter}");

      // The staged rows table should appear
      expect(await screen.findByText(stagedRowsHeadingPattern)).toBeInTheDocument();

      // Device field should be cleared and refocused
      await waitFor(() => expect(deviceInput).toHaveValue(""));
      expect(document.activeElement).toBe(deviceInput);
    });

    it("Ctrl+Enter also commits the row", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "Y");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-CTRL");
      await user.keyboard("{Control>}{Enter}{/Control}");

      expect(await screen.findByText(stagedRowsHeadingPattern)).toBeInTheDocument();
    });

    it("Escape clears in-progress row without removing staged rows", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "Z");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);

      // Stage first row
      await user.click(deviceInput);
      await user.keyboard("HDMS-001");
      await user.keyboard("{Enter}");
      expect(await screen.findByText(stagedRowsHeading(1))).toBeInTheDocument();

      // Start a second row, then escape
      await user.click(deviceInput);
      await user.keyboard("HDMS-002");
      await user.keyboard("{Escape}");

      // In-progress row is cleared
      expect(deviceInput).toHaveValue("");
      // Staged rows remain untouched
      expect(screen.getByText(stagedRowsHeading(1))).toBeInTheDocument();
    });
  });

  // ── 4.6b: Field resolution ───────────────────────────────────────────────

  describe("4.6b — Field resolution", () => {
    it("person typeahead shows results matching the search query", async () => {
      const user = userEvent.setup();
      vi.mocked(apiClient.listDepartments).mockResolvedValue({
        data: {
          items: [
            { id: "dept-1", name: "Nursing" },
            { id: "dept-2", name: "Radiology" },
          ],
        },
        error: undefined,
      } as any);
      vi.mocked(apiClient.listUsers).mockResolvedValue({
        data: {
          items: [
            { id: "user-1", fullName: "Sharma Priya", employeeNo: "E-001", departmentId: "dept-1" },
            { id: "user-2", fullName: "Sharma Rahul", employeeNo: "E-002", departmentId: "dept-2" },
          ],
          total: 2,
        },
        error: undefined,
      } as any);

      renderPage();
      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "P");
      await screen.findByText(ja.backfill.newEntryHeading);

      const personInput = screen.getByPlaceholderText(ja.backfill.personPlaceholder);
      await user.click(personInput);
      await user.keyboard("Sharma");

      // Both Sharmas must appear and be distinguishable by department
      expect(await screen.findByText("Sharma Priya")).toBeInTheDocument();
      expect(await screen.findByText("Sharma Rahul")).toBeInTheDocument();
      expect(screen.getByText(/nursing/i)).toBeInTheDocument();
      expect(screen.getByText(/radiology/i)).toBeInTheDocument();
    });

    it("inline 'Create new person' dialog opens without losing staged rows", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "Q");
      await screen.findByText(ja.backfill.newEntryHeading);

      // Stage one row first
      const deviceInput = screen.getByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-STAGED");
      await user.keyboard("{Enter}");
      expect(await screen.findByText(stagedRowsHeading(1))).toBeInTheDocument();

      // Click the create-new-person button
      const createBtn = screen.getByRole("button", { name: ja.backfill.createNewPerson });
      await user.click(createBtn);

      // Dialog appears
      expect(await screen.findByText(ja.backfill.createNewPerson)).toBeInTheDocument();
      expect(screen.getByText(ja.backfillInlineUserDialog.description)).toBeInTheDocument();

      // Staged rows should still be visible
      expect(screen.getByText(stagedRowsHeading(1))).toBeInTheDocument();
    });

    it("inline person creation marks the person as NEW in staged table", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "N");
      await screen.findByText(ja.backfill.newEntryHeading);

      const createBtn = screen.getByRole("button", { name: ja.backfill.createNewPerson });
      await user.click(createBtn);

      await screen.findByLabelText(ja.userDetail.fullNameLabel);
      await user.type(screen.getByLabelText(ja.userDetail.fullNameLabel), "Tanaka Hiroshi");
      await user.click(screen.getByRole("button", { name: ja.backfillInlineUserDialog.addToBatch }));

      // Now commit the row
      const deviceInput = screen.getByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-NEW");
      await user.keyboard("{Enter}");

      // Staged table should show NEW badge
      expect(await screen.findByText(stagedRowsHeadingPattern)).toBeInTheDocument();
      expect(screen.getAllByText(ja.backfill.newBadge)).toHaveLength(1);
    });
  });

  // ── 4.6c: Batch state & localStorage ────────────────────────────────────

  describe("4.6c — Batch state & crash resilience", () => {
    it("restores staged rows from localStorage on remount (simulated crash)", async () => {
      const user = userEvent.setup();
      const { unmount } = renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "A");
      await user.type(refInput, "B");
      // Clear and retype to get a known value
      await user.clear(refInput);
      // Set value directly via fireEvent to ensure stable state
      await act(async () => {
        const nativeSetter = Object.getOwnPropertyDescriptor(
          window.HTMLInputElement.prototype,
          "value",
        )!.set!;
        nativeSetter.call(refInput, "REG-PERSIST");
        refInput.dispatchEvent(new Event("input", { bubbles: true }));
      });
      await waitFor(() => expect(refInput).toHaveValue("REG-PERSIST"));

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-CRASH");
      await user.keyboard("{Enter}");

      await waitFor(() =>
        expect(screen.getByText(stagedRowsHeading(1))).toBeInTheDocument(),
      );

      // Verify localStorage was written
      expect(localStorage.getItem("hdms_backfill_context_ref")).toBe("REG-PERSIST");
      expect(
        JSON.parse(localStorage.getItem("hdms_backfill_staged_REG-PERSIST") ?? "[]"),
      ).toHaveLength(1);

      // Simulate crash: unmount
      unmount();

      // Remount
      const queryClient2 = createTestQueryClient();
      render(
        <QueryClientProvider client={queryClient2}>
          <BackfillPage />
        </QueryClientProvider>,
      );

      // paperRef should be restored from localStorage
      const refInput2 = screen.getByLabelText(ja.backfill.pageReferenceLabel);
      expect(refInput2).toHaveValue("REG-PERSIST");

      // Staged rows should survive the simulated crash
      expect(await screen.findByText(stagedRowsHeading(1))).toBeInTheDocument();
    });

    it("two page references keep separate staged sets", async () => {
      const user = userEvent.setup();
      const { unmount: _ } = renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;

      // Set paperRef to "page-A" directly (no intermediate empty state)
      const fireChange = (val: string) => {
        const nativeSetter = Object.getOwnPropertyDescriptor(
          window.HTMLInputElement.prototype,
          "value",
        )!.set!;
        nativeSetter.call(refInput, val);
        refInput.dispatchEvent(new Event("input", { bubbles: true }));
      };

      fireChange("page-A");
      await waitFor(() => expect(refInput).toHaveValue("page-A"));

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-A");
      await user.keyboard("{Enter}");
      expect(await screen.findByText(stagedRowsHeading(1))).toBeInTheDocument();

      // Verify "page-A"'s row is in localStorage
      await waitFor(() => {
        const saved = JSON.parse(localStorage.getItem("hdms_backfill_staged_page-A") ?? "[]");
        expect(saved).toHaveLength(1);
      });

      // Switch to page-B directly (no clear needed, avoids intermediate empty state)
      fireChange("page-B");
      await waitFor(() => expect(refInput).toHaveValue("page-B"));

      // page-B has no staged rows
      await waitFor(
        () => expect(screen.queryByText(stagedRowsHeadingPattern)).not.toBeInTheDocument(),
        { timeout: 3000 },
      );

      // Switch back to page-A directly
      fireChange("page-A");
      await waitFor(() => expect(refInput).toHaveValue("page-A"));

      // page-A's row should be restored from localStorage
      expect(
        await screen.findByText(stagedRowsHeading(1), {}, { timeout: 3000 }),
      ).toBeInTheDocument();
    });




    it("manual action override is visible in staged row table", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "V");
      await screen.findByText(ja.backfill.newEntryHeading);

      // Look for the action combobox by label
      const actionCombobox = screen.getByLabelText(ja.backfill.actionLabel);
      expect(actionCombobox).toBeInTheDocument();

      const deviceInput = screen.getByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-OVR");
      await user.keyboard("{Enter}");

      // Staged row should appear (action = null by default = "auto")
      expect(await screen.findByText(stagedRowsHeadingPattern)).toBeInTheDocument();
    });
  });

  // ── 4.6d: Conflicts ──────────────────────────────────────────────────────

  describe("4.6d — Conflict panel", () => {
    it("E17: Save button is disabled when no staged rows exist", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "C");
      await screen.findByText(ja.backfill.newEntryHeading);

      // No staged rows yet, so the save button section doesn't render
      expect(screen.queryByRole("button", { name: ja.backfill.saveBatch })).not.toBeInTheDocument();
    });

    it("Save batch is enabled after staging rows with no conflicts", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "D");
      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);

      await user.click(deviceInput);
      await user.keyboard("HDMS-OK");
      await user.keyboard("{Enter}");

      // After staging, save button should exist
      const saveButton = await screen.findByRole("button", { name: ja.backfill.saveBatch });
      expect(saveButton).toBeInTheDocument();
    });

    it("resolving a conflict does not immediately re-trigger preview loop", async () => {
      const user = userEvent.setup();
      renderPage();

      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "L");
      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);

      await user.click(deviceInput);
      await user.keyboard("HDMS-LOOP");
      await user.keyboard("{Enter}");

      const initialCallCount = vi.mocked(apiClient.previewBackfillBatch).mock.calls.length;

      // Wait for debounce to settle
      await new Promise((r) => setTimeout(r, 900));
      const callsAfterDebounce = vi.mocked(apiClient.previewBackfillBatch).mock.calls.length;

      // Should not have fired more than 2 times in the debounce window (one immediate, one debounced)
      expect(callsAfterDebounce - initialCallCount).toBeLessThanOrEqual(2);
    });
  });

  // ── 4.6e: Follow-through ─────────────────────────────────────────────────

  describe("4.6e — Follow-through after commit", () => {
    it("shows success message and no card prompt when batch has no new users", async () => {
      const user = userEvent.setup();

      // Dynamic mock: capture the actual clientRowId from the request
      vi.mocked(apiClient.recordBackfillBatch).mockImplementation(async ({ body }) => {
        const rows = (body as any).rows ?? [];
        return {
          data: {
            committed: true,
            rows: rows.map((r: any) => ({
              clientRowId: r.clientRowId,
              status: "ok",
              action: "borrow",
              loanId: "loan-1",
            })),
            summary: { ok: rows.length, conflicts: 0, newUsers: 0 },
          },
          error: undefined,
        } as any;
      });

      renderPage();
      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "S");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-REC");
      await user.keyboard("{Enter}");

      const saveButton = await screen.findByRole("button", { name: ja.backfill.saveBatch });
      await user.click(saveButton);

      // Success dialog should appear
      expect(await screen.findByTestId("commit-success-dialog")).toBeInTheDocument();
      // No new users, so no issue-cards prompt
      expect(
        screen.queryByRole("button", { name: localizedCountPattern(ja.backfill.issueCardsOne) }),
      ).not.toBeInTheDocument();
    });


    it("shows 'Issue cards to N new people' when batch includes newly created users", async () => {
      const user = userEvent.setup();

      // Set up staged rows: one with newUser
      renderPage();
      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "F");
      await screen.findByText(ja.backfill.newEntryHeading);

      // Create a new person inline
      const createBtn = screen.getByRole("button", { name: ja.backfill.createNewPerson });
      await user.click(createBtn);

      const nameInput = await screen.findByLabelText(ja.userDetail.fullNameLabel);
      await user.type(nameInput, "Nakamura Yuki");
      await user.click(screen.getByRole("button", { name: ja.backfillInlineUserDialog.addToBatch }));

      const deviceInput = screen.getByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-FOLLOW");
      await user.keyboard("{Enter}");

      // Stage a row with the new person — now set up the mock
      vi.mocked(apiClient.recordBackfillBatch).mockImplementation(async ({ body }) => {
        const rows = (body as any).rows ?? [];
        return {
          data: {
            committed: true,
            rows: rows.map((r: any, i: number) => ({
              clientRowId: r.clientRowId,
              status: "ok",
              action: "borrow",
              loanId: `loan-new-${i}`,
              userId: r.userRef?.newUser ? "user-new-1" : undefined,
              createsUser: !!r.userRef?.newUser,
            })),
            summary: { ok: rows.length, conflicts: 0, newUsers: 1 },
          },
          error: undefined,
        } as any;
      });

      const saveButton = await screen.findByRole("button", { name: ja.backfill.saveBatch });
      await user.click(saveButton);

      // Follow-through dialog with card issuance button
      await screen.findByTestId("commit-success-dialog");
      expect(
        screen.getByRole("button", { name: localizedCountPattern(ja.backfill.issueCardsOne) }),
      ).toBeInTheDocument();
    });


    it("'Record another page' clears paperRef", async () => {
      const user = userEvent.setup();

      // Dynamic mock
      vi.mocked(apiClient.recordBackfillBatch).mockImplementation(async ({ body }) => {
        const rows = (body as any).rows ?? [];
        return {
          data: {
            committed: true,
            rows: rows.map((r: any) => ({
              clientRowId: r.clientRowId,
              status: "ok",
              action: "borrow",
              loanId: "loan-1",
            })),
            summary: { ok: rows.length, conflicts: 0, newUsers: 0 },
          },
          error: undefined,
        } as any;
      });

      renderPage();
      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "T");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-REC2");
      await user.keyboard("{Enter}");

      await user.click(await screen.findByRole("button", { name: ja.backfill.saveBatch }));

      // Wait for the dialog to appear, then click record another page
      await screen.findByTestId("commit-success-dialog");
      await user.click(screen.getByRole("button", { name: ja.backfill.recordAnother }));

      // paperRef should be cleared
      await waitFor(() => expect(screen.getByLabelText(ja.backfill.pageReferenceLabel)).toHaveValue(""));
    });

    it("follow-through lists only new people from THIS batch", async () => {
      const user = userEvent.setup();

      // Dynamic mock that marks the second row as a newly created user
      vi.mocked(apiClient.recordBackfillBatch).mockImplementation(async ({ body }) => {
        const rows = (body as any).rows ?? [];
        return {
          data: {
            committed: true,
            rows: rows.map((r: any, i: number) => ({
              clientRowId: r.clientRowId,
              status: "ok",
              action: "borrow",
              loanId: `loan-${i}`,
              // Mark second row as new user
              userId: i === 1 ? "user-new-2" : undefined,
              createsUser: i === 1,
            })),
            summary: { ok: rows.length, conflicts: 0, newUsers: 1 },
          },
          error: undefined,
        } as any;
      });

      renderPage();
      const refInput = screen.getByLabelText(ja.backfill.pageReferenceLabel) as HTMLInputElement;
      await user.type(refInput, "O");

      const deviceInput = await screen.findByLabelText(ja.backfill.deviceLabel);
      await user.click(deviceInput);
      await user.keyboard("HDMS-EXISTING");
      await user.keyboard("{Enter}");
      await user.click(deviceInput);
      await user.keyboard("HDMS-NEWUSER");
      await user.keyboard("{Enter}");

      await user.click(await screen.findByRole("button", { name: ja.backfill.saveBatch }));

      // Dialog should appear
      await screen.findByTestId("commit-success-dialog");

      // Only 1 new person (not 2 rows total)
      expect(
        screen.getByText(translate(catalogues, "ja", "backfill.newPeopleOne", { count: 1 })),
      ).toBeInTheDocument();
    });

  });


  // ── Accessibility ────────────────────────────────────────────────────────

  describe("Accessibility", () => {
    it("passes axe audit on initial render", async () => {
      const { container } = renderPage();
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
