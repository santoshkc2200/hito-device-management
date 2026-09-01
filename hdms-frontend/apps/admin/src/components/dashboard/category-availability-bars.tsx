import type { CategoryAvailability } from "@hdms/api-client";
import { Link } from "@tanstack/react-router";
import { Folder } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useT } from "@/i18n";
import { cn } from "@hdms/ui";

interface CategoryAvailabilityBarsProps {
  categories: CategoryAvailability[];
}

export function CategoryAvailabilityBars({
  categories,
}: CategoryAvailabilityBarsProps) {
  const t = useT();

  return (
    <Card className="h-full">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-semibold">
            {t("dashboard.categories.title")}
          </CardTitle>
          <Link
            to="/devices"
            className="text-xs font-medium text-primary hover:underline"
          >
            {t("dashboard.categories.viewAllDevices")}
          </Link>
        </div>
      </CardHeader>
      <CardContent>
        {categories.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-6 text-center text-muted-foreground">
            <Folder className="size-8 text-muted-foreground/50 mb-2" />
            <p className="text-sm">{t("dashboard.categories.noCategories")}</p>
          </div>
        ) : (
          <div className="space-y-4">
            {categories.map((cat) => {
              const total = cat.totalCount || 0;
              const available = cat.availableCount || 0;
              const pct = total > 0 ? Math.round((available / total) * 100) : 0;

              // Determine tone for the bar
              const toneClass =
                pct === 0
                  ? "bg-rose-500"
                  : pct < 30
                  ? "bg-amber-500"
                  : "bg-emerald-500";

              return (
                <div key={cat.categoryId} className="space-y-1.5" data-testid={`category-bar-${cat.categoryId}`}>
                  <div className="flex items-center justify-between text-sm">
                    <Link
                      to="/devices"
                      search={{ category: cat.categoryId }}
                      className="font-medium text-foreground hover:text-primary hover:underline transition-colors"
                    >
                      {cat.categoryName}
                    </Link>
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-semibold text-muted-foreground">
                        {t("dashboard.categories.availableCount", { available, total })}
                      </span>
                      <span className="text-xs font-mono font-medium text-foreground/80 w-10 text-right">
                        {pct}%
                      </span>
                    </div>
                  </div>

                  <div
                    className="relative h-2.5 w-full overflow-hidden rounded-full bg-secondary"
                    role="progressbar"
                    aria-valuenow={available}
                    aria-valuemin={0}
                    aria-valuemax={total}
                    aria-label={t("dashboard.categories.ariaProgress", {
                      category: cat.categoryName,
                      available,
                      total,
                      percent: pct,
                    })}
                  >
                    <div
                      className={cn("h-full transition-all duration-300 rounded-full", toneClass)}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
