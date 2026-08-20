import { createRoute } from "@tanstack/react-router";
import { Radio } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function CardReaderTestPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Card reader test</h1>
      <EmptyState
        icon={Radio}
        title="Hardware scanner diagnostic"
        explanation="Side-by-side comparison of raw USB scanner input vs normalized credential tokens will land in task 4.5d."
      />
    </div>
  );
}

export const cardReaderTestRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/card-reader-test",
  component: CardReaderTestPlaceholder,
});
