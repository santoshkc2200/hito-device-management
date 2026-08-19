import { QueryClient } from "@tanstack/react-query";

// Module-level singleton so it can be shared between the QueryClientProvider
// in App.tsx and the router's beforeLoad guards (see router.tsx), which run
// outside of React and need direct access to the cache.
export const queryClient = new QueryClient();
