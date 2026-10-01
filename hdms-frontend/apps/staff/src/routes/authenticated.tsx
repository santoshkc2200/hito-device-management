import { Outlet, createRoute, isRedirect, redirect } from "@tanstack/react-router";
import { isUnderMaintenance, maintenanceEnded } from "@hdms/ui";
import { currentStaffQueryOptions } from "@/lib/auth";
import { rootRoute } from "./root";

export const authenticatedRoute = createRoute({
  id: "authenticated",
  getParentRoute: () => rootRoute,
  beforeLoad: async ({ context, location }) => {
    let me;
    try {
      me = await context.queryClient.ensureQueryData(currentStaffQueryOptions);
    } catch (err) {
      if (isRedirect(err)) throw err;
      if (!isUnderMaintenance()) throw redirect({ to: "/login" });
      // /v1/staff/me is gated during a restore. The root shows the notice
      // meanwhile; the guard waits for the restore to end and asks again.
      // Redirecting would strand a signed-in member on the login page, and
      // throwing would replace the whole app with the router's error screen.
      await maintenanceEnded();
      try {
        me = await context.queryClient.fetchQuery(currentStaffQueryOptions);
      } catch {
        throw redirect({ to: "/login" });
      }
    }

    if (!me) {
      throw redirect({ to: "/login" });
    }

    if (!me.profileComplete && location.pathname !== "/complete-profile") {
      throw redirect({ to: "/complete-profile" });
    }

    if (me.mustChangePassword && location.pathname !== "/change-password") {
      throw redirect({ to: "/change-password" });
    }

    return { me };
  },
  component: () => <Outlet />,
});
