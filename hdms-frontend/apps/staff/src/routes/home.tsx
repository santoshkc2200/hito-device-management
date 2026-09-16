import { getStaffMeCredential, getStaffMeLoans } from "@hdms/api-client";
import { formatDate, useLocale } from "@hdms/i18n";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import { MyQr } from "@/components/my-qr";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

export function HomePage() {
  const t = useT();
  const { locale } = useLocale();

  const credentialQuery = useQuery({
    queryKey: ["staff", "me", "credential"],
    queryFn: async () => {
      const { data, error } = await getStaffMeCredential();
      if (error) throw error;
      return data;
    },
  });

  const loansQuery = useQuery({
    queryKey: ["staff", "me", "loans"],
    queryFn: async () => {
      const { data, error } = await getStaffMeLoans();
      if (error) throw error;
      return data;
    },
  });

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
        ) : null}
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
