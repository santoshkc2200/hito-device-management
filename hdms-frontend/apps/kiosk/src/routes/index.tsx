import { getHealthz } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { rootRoute } from "./root";

// Calls /v1/healthz through the generated client, proving the contract
// pipeline end to end (docs/phases/phase-0-foundations.md, 0.7).
function HealthCheck() {
  const { data, error, isPending, refetch } = useQuery({
    queryKey: ["healthz"],
    queryFn: async () => {
      const { data, error } = await getHealthz();
      if (error) throw error;
      return data;
    },
  });

  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-4 p-8">
      <h1 className="text-2xl font-semibold">HDMS Kiosk</h1>
      <p className="text-muted-foreground">
        {isPending && "Checking API…"}
        {error && `API unreachable: ${error instanceof Error ? error.message : String(error)}`}
        {data && `API status: ${data.status}`}
      </p>
      <Button size="lg" variant="outline" onClick={() => refetch()}>
        Recheck
      </Button>
    </main>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HealthCheck,
});
