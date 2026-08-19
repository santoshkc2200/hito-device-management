import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouter } from "@tanstack/react-router";
import {
  Boxes,
  IdCardLanyard,
  LogOut,
  Tags,
  Users as UsersIcon,
  UserPlus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { currentAdminQueryOptions, logoutAdmin } from "@/lib/auth";
import { cn } from "@hdms/ui";

const navItems = [
  { to: "/devices", label: "Devices", icon: Boxes },
  { to: "/users", label: "Users", icon: UsersIcon },
  { to: "/register", label: "Register borrower", icon: UserPlus },
  { to: "/credentials", label: "Credentials", icon: IdCardLanyard },
  { to: "/labels", label: "Labels", icon: Tags },
] as const;

export function AppShell() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const navigate = useNavigate();

  async function handleLogout() {
    await logoutAdmin();
    await router.invalidate();
    await navigate({ to: "/login" });
  }

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
        <nav className="flex flex-1 flex-col gap-1 p-3">
          {navItems.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              className="flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium text-secondary-foreground/80 transition-colors hover:bg-accent hover:text-accent-foreground [&.active]:bg-accent [&.active]:text-accent-foreground"
              activeProps={{ className: "active" }}
            >
              <item.icon className="size-4 shrink-0" />
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="border-t border-border p-3">
          <div className="mb-2 truncate px-2 text-xs text-muted-foreground">
            {admin?.email}
          </div>
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
      <div className={cn("flex flex-col")}>
        <main className="flex-1 overflow-y-auto p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
