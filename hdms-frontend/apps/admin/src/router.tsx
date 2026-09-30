import { createRouter } from "@tanstack/react-router";
import { queryClient } from "@/lib/query-client";
import { auditRoute } from "./routes/audit";
import { authenticatedRoute } from "./routes/authenticated";
import { backfillRoute } from "./routes/backfill";
import { backupsRoute } from "./routes/backups";
import { cardReaderTestRoute } from "./routes/card-reader-test";
import { credentialsRoute } from "./routes/credentials";
import { dashboardRoute } from "./routes/dashboard";
import { deviceDetailRoute } from "./routes/devices.$deviceId";
import { devicesRoute } from "./routes/devices";
import { disputedRoute } from "./routes/disputed";
import { indexRoute } from "./routes/index";
import { labelsRoute } from "./routes/labels";
import { loanDetailRoute } from "./routes/loans.$loanId";
import { loansRoute } from "./routes/loans";
import { loginRoute } from "./routes/login";
import { notificationsRoute } from "./routes/notifications";
import { registerRoute } from "./routes/register";
import { reportsRoute } from "./routes/reports";
import { reservationsRoute } from "./routes/reservations";
import { rootRoute } from "./routes/root";
import { settingsRoute } from "./routes/settings";
import { userDetailRoute } from "./routes/users.$userId";
import { usersRoute } from "./routes/users";

const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    indexRoute,
    dashboardRoute,
    backfillRoute,
    devicesRoute,
    deviceDetailRoute,
    usersRoute,
    userDetailRoute,
    registerRoute,
    loansRoute,
    loanDetailRoute,
    reservationsRoute,
    disputedRoute,
    reportsRoute,
    auditRoute,
    notificationsRoute,
    settingsRoute,
    backupsRoute,
    credentialsRoute,
    labelsRoute,
    cardReaderTestRoute,
  ]),
]);

export const routerBasepath = (() => {
  // Production serves the admin console under /admin (5.3a); dev serves at /.
  // VITE_BASE_PATH is set to /admin/ only for the production build.
  const raw = (import.meta as any).env?.VITE_BASE_PATH ?? "/";
  if (raw === "/") return "/";
  return raw.replace(/\/$/, "");
})();

export const router = createRouter({ routeTree, basepath: routerBasepath, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
