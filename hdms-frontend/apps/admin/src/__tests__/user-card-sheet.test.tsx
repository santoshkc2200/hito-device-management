import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { it, expect, vi } from "vitest";
import type { User } from "@hdms/api-client";
import { UserCardSheetDialog } from "@/components/user-card-sheet-dialog";

vi.mock("@/components/label-templates", () => ({
  LabelSheet: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  StaffCardLabel: ({ token }: { token: string }) => <span data-testid="card-token">{token}</span>,
}));

it("prints employee numbers without requiring issued credentials", async () => {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <UserCardSheetDialog
        open
        onOpenChange={vi.fn()}
        users={[
          { id: "user-1", fullName: "One", employeeNo: "001234" },
          { id: "user-2", fullName: "Two", employeeNo: "HH-L01" },
        ] as User[]}
        departmentName={new Map()}
      />
    </QueryClientProvider>,
  );
  expect((await screen.findAllByTestId("card-token")).map((el) => el.textContent)).toEqual(["001234", "HH-L01"]);
});
