import {
  getStaffMeCredential,
  getStaffMeLoans,
  getStaffMeReservations,
  cancelStaffReservation,
} from "@hdms/api-client";
import { formatDate, formatTime, useLocale } from "@hdms/i18n";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { MyQr } from "@/components/my-qr";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

export function HomePage() {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const [cancelError, setCancelError] = useState<string | null>(null);

  const cancelMutation = useMutation({
    mutationFn: async (id: string) => {
      const { error } = await cancelStaffReservation({ path: { id } });
      if (error) throw error;
    },
    onSuccess: () => {
      setCancelTarget(null);
      setCancelError(null);
      queryClient.invalidateQueries({ queryKey: ["staff", "me", "reservations"] });
    },
    onError: () => setCancelError(t("booking.cancelFailed")),
  });

  const credentialQuery = useQuery({
    queryKey: ["staff", "me", "credential"],
    queryFn: async () => {
      const { data, error } = await getStaffMeCredential();
      // No active card yet (e.g. an account created with a password) is a
      // normal state, not a failure: resolve to null so it isn't retried.
      if (error?.type?.endsWith("/no-credential")) return null;
      if (error) throw error;
      return data;
    },
  });

  const reservationsQuery = useInfiniteQuery({
    queryKey: ["staff", "me", "reservations"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await getStaffMeReservations({ query: pageParam ? { cursor: pageParam } : {} });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (lastPage) => lastPage?.nextCursor ?? undefined,
  });

  const loansQuery = useQuery({
    queryKey: ["staff", "me", "loans"],
    queryFn: async () => {
      const { data, error } = await getStaffMeLoans();
      if (error) throw error;
      return data;
    },
  });

  const reservations = reservationsQuery.data?.pages.flatMap((page) => page?.items ?? []) ?? [];
  const loans = loansQuery.data?.items ?? [];

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6 p-4">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-bold">{t("home.title")}</h1>
        <div className="flex items-center gap-4">
          <Link
            to="/devices"
            className="text-sm font-medium text-primary hover:underline"
          >
            {t("devices.title")}
          </Link>
          <Link
            to="/settings"
            className="text-sm font-medium text-primary hover:underline"
          >
            {t("settings.title")}
          </Link>
        </div>
      </header>

      <section className="flex flex-col items-center justify-center rounded-2xl border bg-card p-6 shadow-sm">
        {credentialQuery.isLoading ? (
          <p className="text-sm text-muted-foreground">{t("loading")}</p>
        ) : credentialQuery.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {t("home.loadError")}
          </p>
        ) : credentialQuery.data?.token ? (
          <MyQr token={credentialQuery.data.token} />
        ) : (
          <p className="text-center text-sm text-muted-foreground">
            {t("home.noCredential")}
          </p>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">{t("home.myReservations")}</h2>
        {cancelError && <p role="alert" className="text-sm text-destructive">{cancelError}</p>}

        {reservationsQuery.isLoading ? (
          <p className="text-sm text-muted-foreground">{t("loading")}</p>
        ) : reservationsQuery.isError && reservations.length === 0 ? (
          <p role="alert" className="text-sm text-destructive">
            {t("home.loadError")}
          </p>
        ) : reservations.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">
            {t("home.noReservations")}
          </p>
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-muted/50 text-xs font-medium text-muted-foreground">
                <tr>
                  <th className="px-3 py-2">{t("home.deviceName")}</th>
                  <th className="px-3 py-2">{t("home.reservedFrom")}</th>
                  <th className="px-3 py-2">{t("home.reservedUntil")}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {reservations.map((reservation) => (
                  <tr key={reservation.id} className="hover:bg-muted/30">
                    <td className="max-w-[140px] px-3 py-2.5 font-medium break-words">
                      {reservation.deviceName}
                      {cancelTarget === reservation.id ? (
                        <div className="mt-2 flex flex-wrap gap-2">
                          <Button
                            size="sm"
                            variant="destructive"
                            disabled={cancelMutation.isPending}
                            onClick={() => cancelMutation.mutate(reservation.id)}
                          >
                            {t("booking.confirmCancel")}
                          </Button>
                          <Button size="sm" variant="outline" disabled={cancelMutation.isPending} onClick={() => setCancelTarget(null)}>
                            {t("booking.keepReservation")}
                          </Button>
                        </div>
                      ) : (
                        <button
                          type="button"
                          className="mt-2 block text-xs font-medium text-destructive underline underline-offset-2"
                          disabled={cancelMutation.isPending}
                          onClick={() => { setCancelError(null); setCancelTarget(reservation.id); }}
                        >
                          {t("booking.cancel")}
                        </button>
                      )}
                    </td>
                    <td className="px-2 py-2.5 text-muted-foreground">
                      {formatDate(locale, reservation.startAt)}
                      <span className="block">{formatTime(locale, reservation.startAt)}</span>
                    </td>
                    <td className="px-2 py-2.5 text-muted-foreground">
                      {formatDate(locale, reservation.endAt)}
                      <span className="block">{formatTime(locale, reservation.endAt)}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {reservationsQuery.isFetchNextPageError && (
              <p role="alert" className="border-t px-3 pt-3 text-sm text-destructive">
                {t("home.loadMoreFailed")}
              </p>
            )}
            {reservationsQuery.hasNextPage && (
              <div className="border-t p-3 text-center">
                <Button variant="outline" disabled={reservationsQuery.isFetchingNextPage} onClick={() => reservationsQuery.fetchNextPage()}>
                  {reservationsQuery.isFetchingNextPage ? t("loading") : t("home.loadMoreReservations")}
                </Button>
              </div>
            )}
          </div>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">{t("home.myLoans")}</h2>

        {loansQuery.isLoading ? (
          <p className="text-sm text-muted-foreground">{t("loading")}</p>
        ) : loansQuery.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {t("home.loadError")}
          </p>
        ) : loans.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">{t("home.noLoans")}</p>
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-muted/50 text-xs font-medium text-muted-foreground">
                <tr>
                  <th className="px-3 py-2">{t("home.deviceName")}</th>
                  <th className="px-3 py-2">{t("home.borrowedAt")}</th>
                  <th className="px-3 py-2">{t("home.dueAt")}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {loans.map((loan) => (
                  <tr key={loan.id} className="hover:bg-muted/30">
                    <td className="max-w-[140px] px-3 py-2.5 font-medium break-words">
                      {loan.deviceName}
                    </td>
                    <td className="px-3 py-2.5 text-muted-foreground whitespace-nowrap">
                      {formatDate(locale, loan.borrowedAt)}
                    </td>
                    <td className="px-3 py-2.5 text-muted-foreground whitespace-nowrap">
                      {loan.dueAt ? formatDate(locale, loan.dueAt) : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

export const homeRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  component: HomePage,
});
