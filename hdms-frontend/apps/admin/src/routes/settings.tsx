import { createRoute, useNavigate } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AdminAccountsPanel } from "@/components/admin-accounts-panel";
import { authenticatedRoute } from "./authenticated";

const settingsSearchSchema = z.object({
  tab: z.enum(["policy", "kiosks", "templates", "admins"]).optional(),
});

function SettingsPage() {
  const search = settingsRoute.useSearch();
  const navigate = useNavigate();
  const activeTab = search.tab ?? "policy";

  const handleTabChange = (value: string) => {
    navigate({
      to: "/settings",
      search: { tab: value as "policy" | "kiosks" | "templates" | "admins" },
      replace: true,
    });
  };

  return (
    <div className="flex flex-col gap-6 max-w-6xl">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">
          Settings
        </h1>
        <p className="text-sm text-muted-foreground">
          System configuration, hardware management, templates, and operator accounts.
        </p>
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList>
          <TabsTrigger value="policy">Categories & Policy</TabsTrigger>
          <TabsTrigger value="kiosks">Kiosks</TabsTrigger>
          <TabsTrigger value="templates">Templates</TabsTrigger>
          <TabsTrigger value="admins">Admin Accounts</TabsTrigger>
        </TabsList>
        <TabsContent value="policy" className="mt-4">
          <EmptyState
            icon={Settings}
            title="Categories & System Policy"
            explanation="Category management, default loan periods, and system thresholds will land in task 4.10a."
          />
        </TabsContent>
        <TabsContent value="kiosks" className="mt-4">
          <EmptyState
            icon={Settings}
            title="Kiosk Management"
            explanation="Kiosk registration, status monitoring, token rotation, and scanner configuration will land in task 4.10b."
          />
        </TabsContent>
        <TabsContent value="templates" className="mt-4">
          <EmptyState
            icon={Settings}
            title="Printing Templates"
            explanation="Label geometry presets and paper register slip layout configurations will land in task 4.10c."
          />
        </TabsContent>
        <TabsContent value="admins" className="mt-4">
          <AdminAccountsPanel />
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
