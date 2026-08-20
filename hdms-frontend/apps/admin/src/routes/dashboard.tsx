import { createRoute } from "@tanstack/react-router";
import { LayoutDashboard } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function DashboardPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Dashboard</h1>
      <EmptyState
        icon={LayoutDashboard}
        title="Dashboard overview"
        explanation="Real-time stat tiles, overdue loans list, live activity feed, and attention strips will land in task 4.7."
      />
    </div>
  );
}

export const dashboardRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/dashboard",
  component: DashboardPlaceholder,
});
