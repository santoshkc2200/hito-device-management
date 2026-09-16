import { createRoute, createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { queryClient } from "./lib/query-client";
import { authenticatedRoute } from "./routes/authenticated";
import { completeProfileRoute } from "./routes/complete-profile";
import { homeRoute } from "./routes/home";
import { loginRoute } from "./routes/login";
import { rootRoute } from "./routes/root";

export const devicesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices",
  component: () => <div>Devices</div>,
});

export const changePasswordRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/change-password",
  component: () => <div>Change Password</div>,
});

export const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    homeRoute,
    completeProfileRoute,
    devicesRoute,
    changePasswordRoute,
  ]),
]);

export function createStaffRouter(history?: RouterHistory, qc: QueryClient = queryClient) {
  return createRouter({
    routeTree,
    history,
    context: { queryClient: qc },
  });
}

export const router = createStaffRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
