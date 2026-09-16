import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import { installAuthInterceptors } from "./lib/auth";
import { queryClient } from "./lib/query-client";
import { router } from "./router";

installAuthInterceptors();

export function App() {
  return (
    <LocaleProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </LocaleProvider>
  );
}

export default App;
