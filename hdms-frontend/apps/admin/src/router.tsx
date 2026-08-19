import { createRouter } from "@tanstack/react-router";
import { queryClient } from "@/lib/query-client";
import { authenticatedRoute } from "./routes/authenticated";
import { credentialsRoute } from "./routes/credentials";
import { devicesRoute } from "./routes/devices";
import { indexRoute } from "./routes/index";
import { labelsRoute } from "./routes/labels";
import { loginRoute } from "./routes/login";
import { registerRoute } from "./routes/register";
import { rootRoute } from "./routes/root";
import { usersRoute } from "./routes/users";

const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    indexRoute,
    devicesRoute,
    usersRoute,
    registerRoute,
    credentialsRoute,
    labelsRoute,
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
