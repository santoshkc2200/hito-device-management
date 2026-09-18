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

export const routerBasepath = (() => {
  // Production serves the staff PWA under /staff (5.3a); dev serves at /.
  // VITE_BASE_PATH is set to /staff/ only for the production build.
  const raw = (import.meta as any).env?.VITE_BASE_PATH ?? "/";
  if (raw === "/") return "/";
  return raw.replace(/\/$/, "");
})();

export function createStaffRouter(history?: RouterHistory, qc: QueryClient = queryClient) {
  return createRouter({
    routeTree,
    basepath: routerBasepath,
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
