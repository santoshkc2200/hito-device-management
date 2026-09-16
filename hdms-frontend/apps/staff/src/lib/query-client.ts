import { QueryClient } from "@tanstack/react-query";

// Module-level singleton so it can be shared between QueryClientProvider
// in App.tsx and the router's beforeLoad guards, which run outside React.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 10_000,
    },
  },
});
