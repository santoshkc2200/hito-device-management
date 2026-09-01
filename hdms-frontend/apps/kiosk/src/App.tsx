import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import { initKioskApi, queryClient } from "@/lib/api";
import { getKioskConfig } from "@/lib/kiosk-config";
import { ErrorBoundary } from "@/components/error-boundary";
import { router } from "./router";

initKioskApi();

export function App() {
  const config = getKioskConfig();
  const kioskName = config?.kioskName ?? "HDMS Kiosk";

  return (
    <ErrorBoundary kioskName={kioskName}>
      <LocaleProvider locale={config?.defaultLocale}>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </LocaleProvider>
    </ErrorBoundary>
  );
}
