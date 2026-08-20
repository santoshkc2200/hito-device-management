import { createRoute } from "@tanstack/react-router";
import { FileSpreadsheet } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function BackfillPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Paper backfill</h1>
      <EmptyState
        icon={FileSpreadsheet}
        title="Paper register backfill"
        explanation="Single-row high-speed keyboard-only data entry for paper register logs will land in task 4.6."
      />
    </div>
  );
}

export const backfillRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/backfill",
  component: BackfillPlaceholder,
});
