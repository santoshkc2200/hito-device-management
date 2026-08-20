import { createRoute } from "@tanstack/react-router";
import { FileCheck2 } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

const loansSearchSchema = z.object({
  status: z.enum(["active", "returned", "overdue"]).optional(),
  origin: z.enum(["kiosk", "backfill", "admin"]).optional(),
  q: z.string().optional(),
});

function LoansPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Loans</h1>
      <EmptyState
        icon={FileCheck2}
        title="Lending and loan management"
        explanation="Loan records, overdue view, origin badges, and override actions will land in task 4.8a."
      />
    </div>
  );
}

export const loansRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/loans",
  validateSearch: loansSearchSchema,
  component: LoansPlaceholder,
});
