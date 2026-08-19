import { createRoute, redirect } from "@tanstack/react-router";
import { authenticatedRoute } from "./authenticated";

export const indexRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/devices" });
  },
});
