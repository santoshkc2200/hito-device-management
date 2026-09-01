import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { router } from "@/router";

let currentTestAdmin: apiClient.Admin | undefined;

function mockSignedInAdmin(overrides: Partial<apiClient.Admin> = {}) {
  const admin: apiClient.Admin = {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role: "admin",
    status: "active",
    ...overrides,
  };
  currentTestAdmin = admin;
  return admin;
}

function renderAdminApp() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  if (currentTestAdmin) {
    queryClient.setQueryData(currentAdminQueryKey, currentTestAdmin);
  }

  const history = createMemoryHistory({ initialEntries: ["/dashboard"] });
  const testRouter = createRouter({
    routeTree: router.routeTree,
    history,
    context: { queryClient },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={testRouter} />
    </QueryClientProvider>
  );
}

describe("admin locale", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    currentTestAdmin = undefined;
    document.documentElement.lang = "";
  });

  it("takes the signed-in administrator's stored preference", async () => {
    mockSignedInAdmin({ locale: "en" });
    renderAdminApp();
    expect(await screen.findByText("Dashboard")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("en");
  });

  it("defaults to Japanese when the account has no preference", async () => {
    mockSignedInAdmin({});
    renderAdminApp();
    expect(await screen.findByText("Dashboard")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("ja");
  });
});
