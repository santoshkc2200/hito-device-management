import { createRoute } from "@tanstack/react-router";
import { FileCheck2 } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function LoanDetailPlaceholder() {
  const { loanId } = loanDetailRoute.useParams();

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Loan detail</h1>
      <EmptyState
        icon={FileCheck2}
        title={`Loan ${loanId}`}
        explanation="Loan timeline, scan sources, force-return, write-off, and correct attribution actions will land in task 4.8b/c."
      />
    </div>
  );
}

export const loanDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/loans/$loanId",
  component: LoanDetailPlaceholder,
});
