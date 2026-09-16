import { Outlet, createRoute, isRedirect, redirect } from "@tanstack/react-router";
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
      throw redirect({ to: "/login" });
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
