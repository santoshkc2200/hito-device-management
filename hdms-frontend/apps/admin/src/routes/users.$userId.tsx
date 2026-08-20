import { createRoute } from "@tanstack/react-router";
import { Users as UsersIcon } from "lucide-react";
import { EmptyState } from "@/components/states";
import { authenticatedRoute } from "./authenticated";

function UserDetailPlaceholder() {
  const { userId } = userDetailRoute.useParams();

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">User profile</h1>
      <EmptyState
        icon={UsersIcon}
        title={`User ${userId}`}
        explanation="Borrower profile, issued credentials, held devices history, and provenance will land in task 4.4c."
      />
    </div>
  );
}

export const userDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/users/$userId",
  component: UserDetailPlaceholder,
});
