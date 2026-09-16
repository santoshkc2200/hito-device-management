import { staffLogout } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { currentStaffQueryOptions } from "@/lib/auth";
import { authenticatedRoute } from "./authenticated";

export function SettingsPage() {
  const t = useT();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { locale, setLocale } = useLocale();
  const { data: meData } = useQuery(currentStaffQueryOptions);
  const context = settingsRoute.useRouteContext();
  const me = meData ?? context.me;

  const [isLoggingOut, setIsLoggingOut] = useState(false);

  const handleSignOut = async () => {
    setIsLoggingOut(true);
    try {
      await staffLogout();
    } catch {
      // ignore network errors on logout
    }
    queryClient.clear();
    await router.navigate({ to: "/login" });
  };

  const methodLabels: Record<"password" | "microsoft", string> = {
    password: t("settings.signInMethodPassword"),
    microsoft: t("settings.signInMethodMicrosoft"),
  };

  const signInMethodsText =
    me?.signInMethods && me.signInMethods.length > 0
      ? me.signInMethods.map((m) => methodLabels[m] || m).join(", ")
      : "—";

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6 p-4">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-bold">{t("settings.title")}</h1>
        <Link
          to="/"
          className="text-sm font-medium text-primary hover:underline"
        >
          {t("settings.backToHome")}
        </Link>
      </header>

      {/* Account Section */}
      <section className="flex flex-col gap-4 rounded-2xl border bg-card p-6 shadow-sm">
        <h2 className="text-lg font-semibold">{t("settings.accountSection")}</h2>
        <dl className="grid grid-cols-1 gap-3 text-sm">
          <div>
            <dt className="text-xs font-medium text-muted-foreground">
              {t("settings.nameLabel")}
            </dt>
            <dd className="mt-0.5 font-medium text-foreground">{me?.fullName}</dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-muted-foreground">
              {t("settings.employeeNoLabel")}
            </dt>
            <dd className="mt-0.5 font-mono text-foreground">{me?.employeeNo}</dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-muted-foreground">
              {t("settings.departmentLabel")}
            </dt>
            <dd className="mt-0.5 text-foreground">
              {me?.departmentName || t("settings.noDepartment")}
            </dd>
          </div>
          <div>
            <dt className="text-xs font-medium text-muted-foreground">
              {t("settings.signInMethodLabel")}
            </dt>
            <dd className="mt-0.5 text-foreground">{signInMethodsText}</dd>
          </div>
        </dl>
      </section>

      {/* Password Section - only when hasPassword */}
      {me?.hasPassword && (
        <section className="flex flex-col gap-3 rounded-2xl border bg-card p-6 shadow-sm">
          <h2 className="text-lg font-semibold">{t("settings.passwordSection")}</h2>
          <div>
            <Link
              to="/change-password"
              className="text-sm font-medium text-primary hover:underline"
            >
              {t("settings.changePassword")}
            </Link>
          </div>
        </section>
      )}

      {/* Language and Sign Out Section */}
      <section className="flex flex-col gap-4 rounded-2xl border bg-card p-6 shadow-sm">
        <div className="flex flex-col gap-2">
          <h2 className="text-lg font-semibold">{t("settings.languageSection")}</h2>
          <div className="flex gap-2">
            <Button
              type="button"
              variant={locale === "ja" ? "default" : "outline"}
              size="sm"
              onClick={() => setLocale("ja")}
              aria-pressed={locale === "ja"}
            >
              日本語
            </Button>
            <Button
              type="button"
              variant={locale === "en" ? "default" : "outline"}
              size="sm"
              onClick={() => setLocale("en")}
              aria-pressed={locale === "en"}
            >
              English
            </Button>
          </div>
        </div>

        <div className="pt-2">
          <Button
            type="button"
            variant="destructive"
            className="w-full"
            disabled={isLoggingOut}
            onClick={handleSignOut}
          >
            {isLoggingOut ? t("settings.signingOut") : t("settings.signOut")}
          </Button>
        </div>
      </section>
    </div>
  );
}

export const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  component: SettingsPage,
});
