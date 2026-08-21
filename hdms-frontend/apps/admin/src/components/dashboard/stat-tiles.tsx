import { Link } from "@tanstack/react-router";
import { CheckCircle2, Clock, Layers, Wrench } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@hdms/ui";

interface StatTilesProps {
  availableCount: number;
  onLoanCount: number;
  overdueCount: number;
  maintenanceCount: number;
}

export function StatTiles({
  availableCount,
  onLoanCount,
  overdueCount,
  maintenanceCount,
}: StatTilesProps) {
  const stats = [
    {
      id: "available",
      label: "Available",
      count: availableCount,
      description: "Ready for checkout",
      icon: CheckCircle2,
      color: "text-emerald-600 dark:text-emerald-400",
      bgColor: "bg-emerald-50 dark:bg-emerald-950/30",
      to: "/devices" as const,
      search: { status: "available" as const },
    },
    {
      id: "on_loan",
      label: "On Loan",
      count: onLoanCount,
      description: "Active borrower custody",
      icon: Layers,
      color: "text-blue-600 dark:text-blue-400",
      bgColor: "bg-blue-50 dark:bg-blue-950/30",
      to: "/devices" as const,
      search: { status: "on_loan" as const },
    },
    {
      id: "overdue",
      label: "Overdue",
      count: overdueCount,
      description: "Past scheduled return date",
      icon: Clock,
      color: overdueCount > 0 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground",
      bgColor: overdueCount > 0 ? "bg-amber-50 dark:bg-amber-950/30" : "bg-muted/30",
      to: "/loans" as const,
      search: { status: "overdue" as const },
    },
    {
      id: "maintenance",
      label: "In Service / Maintenance",
      count: maintenanceCount,
      description: "Under repair or inspection",
      icon: Wrench,
      color: "text-purple-600 dark:text-purple-400",
      bgColor: "bg-purple-50 dark:bg-purple-950/30",
      to: "/devices" as const,
      search: { status: "maintenance" as const },
    },
  ];

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {stats.map((item) => {
        const Icon = item.icon;
        return (
          <Link
            key={item.id}
            to={item.to}
            search={item.search}
            data-testid={`stat-tile-${item.id}`}
            className="block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-xl transition-transform active:scale-[0.99]"
          >
            <Card className={cn(
              "h-full border transition-all hover:shadow-md hover:border-foreground/20",
              item.id === "overdue" && overdueCount > 0 && "border-amber-300 dark:border-amber-800"
            )}>
              <CardContent className="p-5">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium text-muted-foreground">
                    {item.label}
                  </span>
                  <div className={cn("flex size-9 items-center justify-center rounded-lg", item.bgColor)}>
                    <Icon className={cn("size-5", item.color)} />
                  </div>
                </div>
                <div className="mt-3 flex items-baseline gap-2">
                  <span className="text-3xl font-bold tracking-tight text-foreground">
                    {item.count}
                  </span>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {item.description}
                </p>
              </CardContent>
            </Card>
          </Link>
        );
      })}
    </div>
  );
}
