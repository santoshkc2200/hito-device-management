import { createRoute } from "@tanstack/react-router";
import { ShieldAlert } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

const auditSearchSchema = z.object({
  actor: z.string().optional(),
  action: z.string().optional(),
  subject: z.string().optional(),
  from: z.string().optional(),
  to: z.string().optional(),
});

function AuditPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Audit log</h1>
      <EmptyState
        icon={ShieldAlert}
        title="Audit event log"
        explanation="Full audit log viewer, diff inspector, and CSV export will land in task 4.9d."
      />
    </div>
  );
}

export const auditRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/audit",
  validateSearch: auditSearchSchema,
  component: AuditPlaceholder,
});
