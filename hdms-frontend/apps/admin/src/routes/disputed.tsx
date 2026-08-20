import { createRoute } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function DisputedLoansPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Disputed loans</h1>
      <EmptyState
        icon={AlertCircle}
        title="Disputed loan records"
        explanation="Conflicted and disputed paper backfill loan records view will land in task 4.8a."
      />
    </div>
  );
}

export const disputedRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/disputed",
  component: DisputedLoansPlaceholder,
});
