import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { queryClient } from "./lib/query-client";
import { authenticatedRoute } from "./routes/authenticated";
import { changePasswordRoute } from "./routes/change-password";
import { completeProfileRoute } from "./routes/complete-profile";
import { devicesRoute } from "./routes/devices";
import { deviceDetailRoute } from "./routes/devices.$deviceId";
import { homeRoute } from "./routes/home";
import { loginRoute } from "./routes/login";
import { rootRoute } from "./routes/root";
import { settingsRoute } from "./routes/settings";

export { changePasswordRoute };

export const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    homeRoute,
    completeProfileRoute,
    devicesRoute,
    deviceDetailRoute,
    settingsRoute,
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
