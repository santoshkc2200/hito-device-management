import { getStaffDevice } from "@hdms/api-client";
import { formatDate, formatTime, useLocale } from "@hdms/i18n";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import { AvailabilityPill } from "@/components/device-availability";
import { ReservationForm } from "@/components/reservation-form";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

export function DeviceDetailPage() {
  const t = useT();
  const { locale } = useLocale();
  const { deviceId } = deviceDetailRoute.useParams();

  const deviceQuery = useQuery({
    queryKey: ["staff", "devices", deviceId],
    queryFn: async () => {
      const { data, error } = await getStaffDevice({ path: { id: deviceId } });
      if (error) throw error;
      return data;
    },
  });

  const device = deviceQuery.data;

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6 p-4">
      <nav>
        <Link
          to="/devices"
          className="inline-flex items-center text-sm font-medium text-primary hover:underline"
        >
          &larr; {t("devices.detail.backToDevices")}
        </Link>
      </nav>

      {deviceQuery.isLoading ? (
        <p className="py-8 text-center text-sm text-muted-foreground">{t("loading")}</p>
      ) : deviceQuery.isError ? (
        <div
          role="alert"
          className="rounded-xl border border-destructive/20 bg-destructive/10 p-4 text-center text-sm text-destructive"
        >
          {t("devices.connectionRequired")}
        </div>
      ) : !device ? (
        <p className="py-8 text-center text-sm text-muted-foreground">
          {t("devices.detail.notFound")}
        </p>
      ) : (
        <div className="flex flex-col gap-6">
          <header className="flex flex-col gap-2">
            <div className="flex items-start justify-between gap-3">
              <h1 className="text-2xl font-bold text-foreground break-words [overflow-wrap:anywhere]">
                {device.name}
              </h1>
            </div>

            {device.availability === "in_use" && device.expectedBackAt && (
              <p className="text-sm text-muted-foreground">
                {t("devices.expectedBackAt", {
                  time: `${formatDate(locale, device.expectedBackAt)} ${formatTime(locale, device.expectedBackAt)}`,
                })}
              </p>
            )}
          </header>

          <div className="rounded-xl border bg-card p-4 shadow-sm">
            <dl className="divide-y text-sm">
              <div className="flex justify-between py-2.5">
                <dt className="text-muted-foreground">{t("devices.detail.assetTag")}</dt>
                <dd className="font-mono font-medium text-foreground break-all">
                  {device.assetTag}
                </dd>
              </div>

              {device.model && (
                <div className="flex justify-between py-2.5">
                  <dt className="text-muted-foreground">{t("devices.detail.model")}</dt>
                  <dd className="text-right font-medium text-foreground break-words">
                    {device.model}
                  </dd>
                </div>
              )}

              {device.categoryName && (
                <div className="flex justify-between py-2.5">
                  <dt className="text-muted-foreground">{t("devices.detail.category")}</dt>
                  <dd className="text-right font-medium text-foreground break-words">
                    {device.categoryName}
                  </dd>
                </div>
              )}

              <div className="flex justify-between py-2.5">
                <dt className="text-muted-foreground">{t("devices.detail.status")}</dt>
                <dd>
                  <AvailabilityPill availability={device.availability} />
                </dd>
              </div>
            </dl>
          </div>
          <ReservationForm device={device} />
        </div>
      )}
    </div>
  );
}

export const deviceDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices/$deviceId",
  component: DeviceDetailPage,
});
