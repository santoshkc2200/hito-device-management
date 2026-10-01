import { createRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { updateMyLocale } from "@hdms/api-client";
import type { Locale } from "@hdms/i18n";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AdminAccountsPanel } from "@/components/admin-accounts-panel";
import { PolicyPanel } from "@/components/settings/policy-panel";
import { KiosksPanel } from "@/components/settings/kiosks-panel";
import { DepartmentManagerPanel } from "@/components/settings/department-manager-panel";
import { LanguagePanel } from "@/components/settings/language-panel";
import { authenticatedRoute } from "./authenticated";
import { useT } from "@/i18n";

const settingsSearchSchema = z.object({
  tab: z.enum(["policy", "kiosks", "departments", "admins", "language"]).optional(),
});

async function updateMyLocaleForPanel(body: { locale: Locale }) {
  const { data, error } = await updateMyLocale({ body });
  if (error) throw error;
  return data;
}

function SettingsPage() {
  const t = useT();
  const search = settingsRoute.useSearch();
  const navigate = useNavigate();
  const activeTab = search.tab ?? "policy";

  const handleTabChange = (value: string) => {
    navigate({
      to: "/settings",
      search: { tab: value as "policy" | "kiosks" | "departments" | "admins" | "language" },
      replace: true,
    });
  };

  return (
    <div className="flex flex-col gap-6 max-w-6xl">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">
          {t("nav.settings")}
        </h1>
        <p className="text-sm text-muted-foreground">{t("settingsPage.subtitle")}</p>
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList>
          <TabsTrigger value="policy">{t("settingsPage.tabPolicy")}</TabsTrigger>
          <TabsTrigger value="kiosks">{t("settingsPage.tabKiosks")}</TabsTrigger>
          <TabsTrigger value="departments">{t("settingsPage.tabDepartments")}</TabsTrigger>
          <TabsTrigger value="admins">{t("settingsPage.tabAdmins")}</TabsTrigger>
          <TabsTrigger value="language">{t("settingsPage.tabLanguage")}</TabsTrigger>
        </TabsList>
        <TabsContent value="policy" className="mt-4">
          <PolicyPanel />
        </TabsContent>
        <TabsContent value="kiosks" className="mt-4">
          <KiosksPanel />
        </TabsContent>
        <TabsContent value="departments" className="mt-4">
          <DepartmentManagerPanel />
        </TabsContent>
        <TabsContent value="admins" className="mt-4">
          <AdminAccountsPanel />
        </TabsContent>
        <TabsContent value="language" className="mt-4">
          <LanguagePanel updateMyLocale={updateMyLocaleForPanel} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  validateSearch: settingsSearchSchema,
  component: SettingsPage,
});
