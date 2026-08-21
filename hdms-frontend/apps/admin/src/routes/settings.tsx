import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import { Radio, Settings } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { Button } from "@/components/ui/button";
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
        <TabsContent value="kiosks" className="mt-4 flex flex-col gap-4">
          <div className="flex items-center justify-between rounded-xl border border-border bg-card p-5 shadow-xs">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-foreground text-sm">Scanner & Card Reader Diagnostic</span>
                <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary uppercase font-mono">
                  Diagnostic Tool
                </span>
              </div>
              <p className="text-xs text-muted-foreground max-w-xl">
                Test hardware USB barcode scanners, RFID/NFC wedge readers, and raw credential token grammar directly on this workstation.
              </p>
            </div>
            <Link to="/card-reader-test">
              <Button size="sm" variant="outline" className="gap-2">
                <Radio className="size-4 text-primary" />
                Launch Diagnostic Tool
              </Button>
            </Link>
          </div>
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
