import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouter } from "@tanstack/react-router";
import {
  Boxes,
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
import type { AdminRole } from "@hdms/api-client";
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

const NAV_GROUPS: NavGroup[] = [
  {
    name: "Daily",
    items: [
      { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
      { to: "/backfill", label: "Paper backfill", icon: FileSpreadsheet, minRole: "technician" },
      { to: "/register", label: "Register borrower", icon: UserPlus, minRole: "technician" },
    ],
  },
  {
    name: "Records",
    items: [
      { to: "/devices", label: "Devices", icon: Boxes },
      { to: "/users", label: "Users", icon: UsersIcon },
      { to: "/loans", label: "Loans", icon: FileCheck2 },
    ],
  },
  {
    name: "Insight",
    items: [
      { to: "/reports", label: "Reports", icon: FileText },
      { to: "/audit", label: "Audit log", icon: ShieldAlert, minRole: "admin" },
    ],
  },
  {
    name: "Settings",
    items: [
      { to: "/settings", label: "Settings", icon: Settings, minRole: "admin" },
      { to: "/credentials", label: "Credentials", icon: IdCardLanyard, minRole: "technician" },
      { to: "/labels", label: "Labels", icon: Tags, minRole: "technician" },
      { to: "/card-reader-test", label: "Card reader test", icon: Radio, minRole: "technician" },
    ],
  },
];

// Role hierarchy rank for cosmetic navigation filtering.
// Note: This is purely for UI ergonomics and UX clarity.
// Actual access control and security enforcement are enforced server-side per endpoint in 4.1a.
function getRoleRank(role?: AdminRole | string): number {
  switch (role) {
    case "superadmin":
    case "admin":
      return 3;
    case "operator":
    case "technician":
      return 2;
    case "viewer":
      return 1;
    default:
      return 0;
  }
}

function hasMinRole(userRole: AdminRole | string | undefined, minRole?: "viewer" | "technician" | "admin"): boolean {
  if (!minRole) return true;
  const userRank = getRoleRank(userRole);
  const requiredRank = getRoleRank(minRole);
  return userRank >= requiredRank;
}

export function AppShell() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const navigate = useNavigate();

  async function handleLogout() {
    await logoutAdmin();
    await router.invalidate();
    await navigate({ to: "/login" });
  }

  const role = admin?.role;

  return (
    <div className="grid min-h-dvh grid-cols-[220px_1fr]">
      <aside className="flex flex-col border-r border-border bg-secondary">
        <div className="border-b border-border px-5 py-5">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            Hito Hospital
          </p>
          <p className="mt-0.5 text-sm font-semibold text-secondary-foreground">
            Device management
          </p>
        </div>
        <nav className="flex flex-1 flex-col gap-4 overflow-y-auto p-3">
          {NAV_GROUPS.map((group) => {
            const visibleItems = group.items.filter((item) => hasMinRole(role, item.minRole));
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
            Sign out
          </Button>
        </div>
      </aside>
      <div className={cn("flex flex-col min-w-0")}>
        <main className="flex-1 overflow-y-auto p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
