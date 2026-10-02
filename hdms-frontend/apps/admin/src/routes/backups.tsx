import { createRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { HistoryTab } from "@/components/backups/history-tab";
import { OverviewTab } from "@/components/backups/overview-tab";
import { RestoreBanner } from "@/components/backups/restore-banner";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { useT } from "@/i18n";
import { RoleGate } from "@/lib/use-role";
import { authenticatedRoute } from "./authenticated";

type BackupTab = "overview" | "destinations" | "snapshots" | "history";

export function BackupsPage() {
  const t = useT();
  const [tab, setTab] = useState<BackupTab>("overview");
  return (
    <RoleGate minRole="admin">
      <div className="flex max-w-6xl flex-col gap-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">{t("nav.backups")}</h1>
          <p className="text-sm text-muted-foreground">{t("backups.subtitle")}</p>
        </div>
        <RestoreBanner />
        <Tabs value={tab} onValueChange={(v) => setTab(v as BackupTab)}>
          <TabsList>
            <TabsTrigger value="overview">{t("backups.tabs.overview")}</TabsTrigger>
            <TabsTrigger value="destinations">{t("backups.tabs.destinations")}</TabsTrigger>
            <TabsTrigger value="snapshots">{t("backups.tabs.snapshots")}</TabsTrigger>
            <TabsTrigger value="history">{t("backups.tabs.history")}</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="mt-4"><OverviewTab /></TabsContent>
          <TabsContent value="destinations" className="mt-4"><DestinationsTab /></TabsContent>
          <TabsContent value="snapshots" className="mt-4"><SnapshotsTab /></TabsContent>
          <TabsContent value="history" className="mt-4"><HistoryTab /></TabsContent>
        </Tabs>
      </div>
    </RoleGate>
  );
}

export const backupsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/backups",
  component: BackupsPage,
});
