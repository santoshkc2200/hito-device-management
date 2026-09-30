import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouter } from "@tanstack/react-router";
import {
  Bell,
  Boxes,
  CalendarClock,
  FileCheck2,
  FileSpreadsheet,
  FileText,
  IdCardLanyard,
  LayoutDashboard,
  LogOut,
  Radio,
  Settings,
  ShieldAlert,
  Tags,
  Users as UsersIcon,
  UserPlus,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { currentAdminQueryOptions, logoutAdmin } from "@/lib/auth";
import { hasRoleAtLeast } from "@/lib/use-role";
import { ReauthDialog } from "@/components/reauth-dialog";
import { ForcedPasswordChangeDialog } from "@/components/forced-password-change-dialog";
import { ForcedTotpDialog } from "@/components/forced-totp-dialog";
import { useT } from "@/i18n";
import { cn } from "@hdms/ui";

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  minRole?: "viewer" | "technician" | "admin";
}

interface NavGroup {
  name: string;
  items: NavItem[];
}

export function AppShell() {
  const t = useT();
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const navigate = useNavigate();

  async function handleLogout() {
    await logoutAdmin();
    await router.invalidate();
    await navigate({ to: "/login" });
  }

  const role = admin?.role;

  const navGroups: NavGroup[] = React.useMemo(
    () => [
      {
        name: t("nav.groups.daily"),
        items: [
          { to: "/dashboard", label: t("nav.dashboard"), icon: LayoutDashboard },
          { to: "/backfill", label: t("nav.backfill"), icon: FileSpreadsheet, minRole: "technician" },
          { to: "/register", label: t("nav.register"), icon: UserPlus, minRole: "technician" },
        ],
      },
      {
        name: t("nav.groups.records"),
        items: [
          { to: "/devices", label: t("nav.devices"), icon: Boxes },
          { to: "/users", label: t("nav.users"), icon: UsersIcon },
          { to: "/loans", label: t("nav.loans"), icon: FileCheck2 },
          { to: "/reservations", label: t("nav.reservations"), icon: CalendarClock },
        ],
      },
      {
        name: t("nav.groups.insight"),
        items: [
          { to: "/reports", label: t("nav.reports"), icon: FileText },
          { to: "/audit", label: t("nav.audit"), icon: ShieldAlert, minRole: "admin" },
          { to: "/notifications", label: t("nav.notifications"), icon: Bell, minRole: "admin" },
        ],
      },
      {
        name: t("nav.groups.settings"),
        items: [
          { to: "/settings", label: t("nav.settings"), icon: Settings, minRole: "admin" },
          { to: "/credentials", label: t("nav.credentials"), icon: IdCardLanyard, minRole: "technician" },
          { to: "/labels", label: t("nav.labels"), icon: Tags, minRole: "technician" },
          { to: "/card-reader-test", label: t("nav.cardReaderTest"), icon: Radio, minRole: "technician" },
        ],
      },
    ],
    [t]
  );

  return (
    <div className="grid min-h-dvh grid-cols-[220px_1fr]">
      <aside className="flex flex-col border-r border-border bg-secondary">
        <div className="border-b border-border px-5 py-5">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            {t("nav.hospitalName")}
          </p>
          <p className="mt-0.5 text-sm font-semibold text-secondary-foreground">
            {t("nav.systemTitle")}
          </p>
        </div>
        <nav className="flex flex-1 flex-col gap-4 overflow-y-auto p-3">
          {navGroups.map((group) => {
            const visibleItems = group.items.filter((item) => hasRoleAtLeast(role, item.minRole));
            if (visibleItems.length === 0) return null;

            return (
              <div key={group.name} className="flex flex-col gap-1">
                <p className="px-3 text-[10px] font-bold tracking-wider text-muted-foreground uppercase">
                  {group.name}
                </p>
                {visibleItems.map((item) => (
                  <Link
                    key={item.to}
                    to={item.to}
                    className="flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm font-medium text-secondary-foreground/80 transition-colors hover:bg-accent hover:text-accent-foreground [&.active]:bg-accent [&.active]:text-accent-foreground"
                    activeProps={{ className: "active" }}
                  >
                    <item.icon className="size-4 shrink-0" />
                    {item.label}
                  </Link>
                ))}
              </div>
            );
          })}
        </nav>
        <div className="border-t border-border p-3">
          <div className="mb-1 truncate px-2 text-xs font-medium text-foreground">
            {admin?.fullName || admin?.email}
          </div>
          {admin?.role && (
            <div className="mb-2 truncate px-2 text-[10px] uppercase tracking-wider text-muted-foreground">
              {admin.role}
            </div>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-2.5"
            onClick={handleLogout}
          >
            <LogOut className="size-4" data-icon="inline-start" />
            {t("nav.signOut")}
          </Button>
        </div>
      </aside>
      <div className={cn("flex flex-col min-w-0")}>
        <main className="flex-1 overflow-y-auto p-8">
          <Outlet />
        </main>
      </div>

      {/* Global Auth Compliance & Re-authentication Modals */}
      <ReauthDialog />
      <ForcedPasswordChangeDialog />
      <ForcedTotpDialog />
    </div>
  );
}
