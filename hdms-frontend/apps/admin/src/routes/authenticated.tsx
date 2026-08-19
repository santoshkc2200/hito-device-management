import { createRoute, redirect } from "@tanstack/react-router";
import { AppShell } from "@/components/app-shell";
import { currentAdminQueryOptions } from "@/lib/auth";
import { rootRoute } from "./root";

// Pathless layout route: every screen behind the admin nav is a child of
// this one. beforeLoad guards the whole subtree in one place rather than
// per-route, and runs on every navigation into it (not just app boot),
// so a session that expires mid-session still bounces to /login.
export const authenticatedRoute = createRoute({
  id: "authenticated",
  getParentRoute: () => rootRoute,
  beforeLoad: async ({ context }) => {
    try {
      await context.queryClient.ensureQueryData(currentAdminQueryOptions);
    } catch {
      throw redirect({ to: "/login" });
    }
  },
  component: AppShell,
});
