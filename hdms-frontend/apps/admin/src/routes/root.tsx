import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { RouteErrorBoundary } from "@/components/states";

export interface RouterContext {
  queryClient: QueryClient;
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <div className="min-h-dvh">
      <RouteErrorBoundary>
        <Outlet />
      </RouteErrorBoundary>
    </div>
  ),
});
