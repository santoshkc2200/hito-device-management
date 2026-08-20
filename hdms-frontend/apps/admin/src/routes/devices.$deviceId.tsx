import { createRoute } from "@tanstack/react-router";
import { Boxes } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function DeviceDetailPlaceholder() {
  const { deviceId } = deviceDetailRoute.useParams();

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Device detail</h1>
      <EmptyState
        icon={Boxes}
        title={`Device ${deviceId}`}
        explanation="Device attributes, loan history, credentials panel, and audit trail will land in task 4.3a."
      />
    </div>
  );
}

export const deviceDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices/$deviceId",
  component: DeviceDetailPlaceholder,
});
