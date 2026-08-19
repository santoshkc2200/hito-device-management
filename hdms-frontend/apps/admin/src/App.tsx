import { client } from "@hdms/api-client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { Toaster } from "@/components/ui/sonner";
import { installAuthInterceptors } from "@/lib/auth";
import { queryClient } from "@/lib/query-client";
import { router } from "./router";

client.setConfig({ baseUrl: import.meta.env.VITE_API_BASE_URL });
installAuthInterceptors();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  );
}
