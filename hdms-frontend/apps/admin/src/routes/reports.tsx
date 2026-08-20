import { createRoute } from "@tanstack/react-router";
import { FileText } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

const reportsSearchSchema = z.object({
  from: z.string().optional(),
  to: z.string().optional(),
  tab: z.enum(["summary", "origin", "health"]).optional(),
});

function ReportsPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Reports</h1>
      <EmptyState
        icon={FileText}
        title="Operational and utilization reports"
        explanation="Utilization metrics, origin-over-time trends, and CSV streaming exports will land in task 4.9."
      />
    </div>
  );
}

export const reportsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/reports",
  validateSearch: reportsSearchSchema,
  component: ReportsPlaceholder,
});
