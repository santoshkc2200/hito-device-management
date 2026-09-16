import { getStaffDevices } from "@hdms/api-client";
import { formatTime, useLocale } from "@hdms/i18n";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { Input } from "@/components/ui/input";
import { AvailabilityPill } from "@/components/device-availability";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

export function DevicesPage() {
  const t = useT();
  const { locale } = useLocale();
  const [search, setSearch] = useState("");

  const devicesQuery = useQuery({
    queryKey: ["staff", "devices"],
    queryFn: async () => {
      const { data, error } = await getStaffDevices();
      if (error) throw error;
      return data;
    },
  });

  const devices = devicesQuery.data?.items;

  const filtered = useMemo(() => {
    if (!devices) return [];
    const q = search.trim().toLowerCase();
    if (!q) return devices;
    return devices.filter(
      (d) =>
        d.name.toLowerCase().includes(q) ||
        d.assetTag.toLowerCase().includes(q),
    );
  }, [devices, search]);

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6 p-4">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-bold">{t("devices.title")}</h1>
        <Link
          to="/"
          className="text-sm font-medium text-primary hover:underline"
        >
          {t("home.title")}
        </Link>
      </header>

      <div className="flex flex-col gap-4">
        <Input
          type="search"
          placeholder={t("devices.searchPlaceholder")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          aria-label={t("devices.searchPlaceholder")}
        />

        {devicesQuery.isLoading ? (
          <p className="py-8 text-center text-sm text-muted-foreground">{t("loading")}</p>
        ) : devicesQuery.isError ? (
          <div
            role="alert"
            className="rounded-xl border border-destructive/20 bg-destructive/10 p-4 text-center text-sm text-destructive"
          >
            {t("devices.connectionRequired")}
          </div>
        ) : filtered.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {t("devices.noDevices")}
          </p>
        ) : (
          <ul className="flex flex-col gap-3">
            {filtered.map((device) => (
              <li key={device.id}>
                <Link
                  to="/devices/$deviceId"
                  params={{ deviceId: device.id }}
                  className="flex flex-col gap-2 rounded-xl border bg-card p-4 shadow-sm transition-colors hover:bg-muted/40"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <span className="block font-semibold text-foreground break-words [overflow-wrap:anywhere]">
                        {device.name}
                      </span>
                      <span className="block font-mono text-xs text-muted-foreground break-all">
                        {device.assetTag}
                      </span>
                    </div>

                    <AvailabilityPill availability={device.availability} />
                  </div>

                  {device.availability === "in_use" && device.expectedBackAt && (
                    <div className="text-xs text-muted-foreground">
                      {t("devices.expectedBackAt", {
                        time: formatTime(
                          locale,
                          device.expectedBackAt,
                        ),
                      })}
                    </div>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

export const devicesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices",
  component: DevicesPage,
});
