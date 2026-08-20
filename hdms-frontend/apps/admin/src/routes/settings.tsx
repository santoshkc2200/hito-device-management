import { createRoute } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import { z } from "zod";
import { EmptyState } from "@/components/states";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { authenticatedRoute } from "./authenticated";

const settingsSearchSchema = z.object({
  tab: z.enum(["policy", "kiosks", "templates", "admins"]).optional(),
});

function SettingsPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Settings</h1>
      <Tabs defaultValue="policy">
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
          <EmptyState
            icon={Settings}
            title="Admin Accounts"
            explanation="Admin user management, role assignments, password resets, and TOTP re-enrolments will land in task 4.1b."
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  validateSearch: settingsSearchSchema,
  component: SettingsPlaceholder,
});
