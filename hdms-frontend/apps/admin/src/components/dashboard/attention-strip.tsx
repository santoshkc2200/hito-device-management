import type { BackfillLastEntry, Kiosk, ScanRejectionSummary } from "@hdms/api-client";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CreditCard,
  DatabaseBackup,
  FileSpreadsheet,
  KeyRound,
  MonitorSmartphone,
  ShieldAlert,
  UserPlus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/status-badge";
import { BACKUP_STALE_MS } from "@/components/backups/format";
import { useT } from "@/i18n";
import { cn } from "@hdms/ui";

interface AttentionStripProps {
  turnedAwayCounts?: ScanRejectionSummary[];
  unboundCredentialCount?: number;
  lowStockThreshold?: number;
  kiosks?: Kiosk[];
  lastPaperEntry?: BackfillLastEntry;
  paperBacklogHours?: number;
  backup?: { lastSuccessAt?: string | null; recoveryKeyStatus?: "missing" | "unconfirmed" | "ready" | "outdated" };
}

function timeSince(date: Date, t: ReturnType<typeof useT>): { hours: number; days: number; text: string } {
  const ms = Date.now() - date.getTime();
  const hours = Math.max(0, Math.floor(ms / (1000 * 60 * 60)));
  const days = Math.floor(hours / 24);

  if (hours < 1) return { hours: 0, days: 0, text: t("dashboard.attention.justNow") };
  if (hours < 24) return { hours, days: 0, text: t("dashboard.attention.hoursAgo", { hours, plural: hours === 1 ? "" : "s" }) };
  return { hours, days, text: t("dashboard.attention.daysAgo", { days, plural: days === 1 ? "" : "s" }) };
}

export function AttentionStrip({
  turnedAwayCounts = [],
  unboundCredentialCount,
  lowStockThreshold = 10,
  kiosks = [],
  lastPaperEntry,
  paperBacklogHours = 48,
  backup,
}: AttentionStripProps) {
  const t = useT();
  const backupNever = backup !== undefined && !backup.lastSuccessAt;
  const backupStale =
    backup !== undefined &&
    Boolean(backup.lastSuccessAt) &&
    Date.now() - new Date(backup.lastSuccessAt as string).getTime() > BACKUP_STALE_MS;

  // An unconfirmed key counts as not printed: nobody has shown it exists on paper.
  const recoveryKeyMissing =
    backup?.recoveryKeyStatus === "missing" || backup?.recoveryKeyStatus === "unconfirmed";
  const recoveryKeyOutdated = backup?.recoveryKeyStatus === "outdated";

  const revokedScans = turnedAwayCounts.find((t) => t.resolvedType === "revoked");
  const unboundScans = turnedAwayCounts.find((t) => t.resolvedType === "unbound");
  const unknownScans = turnedAwayCounts.find((t) => t.resolvedType === "unknown");
  const totalUnregisteredScans = (unboundScans?.totalScans || 0) + (unknownScans?.totalScans || 0);

  const isLowStock =
    typeof unboundCredentialCount === "number" &&
    unboundCredentialCount < lowStockThreshold;

  // Paper backlog check. A threshold of 0 means the hospital keeps no paper
  // ledger, so the warning is off.
  let isPaperBacklog = false;
  let paperTimeText = t("dashboard.attention.noPaperRecordsFound");
  if (paperBacklogHours > 0) {
    if (lastPaperEntry?.recordedAt) {
      const lastDate = new Date(lastPaperEntry.recordedAt);
      const { hours, text } = timeSince(lastDate, t);
      paperTimeText = text;
      if (hours >= paperBacklogHours) {
        isPaperBacklog = true;
      }
    } else {
      isPaperBacklog = true;
    }
  }

  // Quiet kiosk check (active kiosks with lastSeenAt > 24 hours ago or missing)
  const quietKiosks = kiosks.filter((k) => {
    if (k.status !== "active") return false;
    if (!k.lastSeenAt) return true;
    const lastSeen = new Date(k.lastSeenAt);
    const diffHours = (Date.now() - lastSeen.getTime()) / (1000 * 60 * 60);
    return diffHours >= 24;
  });

  const hasAnyAttention =
    backupNever ||
    backupStale ||
    recoveryKeyMissing ||
    recoveryKeyOutdated ||
    Boolean(revokedScans && revokedScans.totalScans > 0) ||
    totalUnregisteredScans > 0 ||
    isLowStock ||
    isPaperBacklog ||
    quietKiosks.length > 0 ||
    kiosks.length > 0;

  if (!hasAnyAttention) {
    return null;
  }

  return (
    <div className="flex flex-col gap-3" data-testid="attention-strip">
      {(backupNever || backupStale) && (
        <Card className="border-destructive/40">
          <CardContent className="flex items-center justify-between gap-3 py-3">
            <div className="flex items-center gap-2">
              <DatabaseBackup className="size-4 text-destructive" />
              <span className="text-sm font-medium">
                {backupNever ? t("dashboard.attention.backupNeverTitle") : t("dashboard.attention.backupStaleTitle")}
              </span>
            </div>
            <Button asChild variant="outline" size="sm">
              <Link to="/backups">{t("dashboard.attention.backupStaleAction")}</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      {(recoveryKeyMissing || recoveryKeyOutdated) && (
        <Card className="border-destructive/40">
          <CardContent className="flex items-center justify-between gap-3 py-3">
            <div className="flex items-center gap-2">
              <KeyRound className="size-4 text-destructive" />
              <span className="text-sm font-medium">
                {recoveryKeyOutdated ? t("dashboard.attention.recoveryKeyOutdatedTitle") : t("dashboard.attention.recoveryKeyMissingTitle")}
              </span>
            </div>
            <Button asChild variant="outline" size="sm">
              <Link to="/backups">{t("dashboard.attention.recoveryKeyAction")}</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      {/* 1. Revoked card scan alert */}
      {revokedScans && revokedScans.totalScans > 0 && (
        <Card
          data-testid="attention-revoked-scans"
          className="border-rose-300 bg-rose-50/70 dark:border-rose-900/60 dark:bg-rose-950/30"
        >
          <CardContent className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-rose-100 dark:bg-rose-900/50 text-rose-700 dark:text-rose-300 mt-0.5 sm:mt-0">
                <ShieldAlert className="size-5" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-rose-900 dark:text-rose-200 flex items-center gap-2">
                  {t("dashboard.attention.revokedScansTitle", { count: revokedScans.totalScans })}
                </h2>
                <p className="text-xs text-rose-700 dark:text-rose-300 mt-0.5">
                  {t("dashboard.attention.revokedScansDetail", {
                    scans: revokedScans.totalScans,
                    scanPlural: revokedScans.totalScans === 1 ? "" : "s",
                    tokens: revokedScans.distinctTokens,
                    tokenPlural: revokedScans.distinctTokens === 1 ? "" : "s",
                  })}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 2. Unregistered / Unbound card scans alert */}
      {totalUnregisteredScans > 0 && (
        <Card
          data-testid="attention-unregistered-scans"
          className="border-amber-300 bg-amber-50/70 dark:border-amber-900/60 dark:bg-amber-950/30"
        >
          <CardContent className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300 mt-0.5 sm:mt-0">
                <UserPlus className="size-5" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-amber-900 dark:text-amber-200">
                  {t("dashboard.attention.unregisteredScansTitle", { count: totalUnregisteredScans })}
                </h2>
                <p className="text-xs text-amber-700 dark:text-amber-300 mt-0.5">
                  {t("dashboard.attention.unregisteredScansDetail")}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 3. Low blank card stock */}
      {isLowStock && (
        <Card
          data-testid="attention-low-stock"
          className="border-amber-200 bg-amber-50/40 dark:border-amber-900/40 dark:bg-amber-950/20"
        >
          <CardContent className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300">
                <CreditCard className="size-5" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-foreground">
                  {t("dashboard.attention.lowStockTitle", { count: unboundCredentialCount })}
                </h2>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {t("dashboard.attention.lowStockDetail", { threshold: lowStockThreshold })}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 4. Paper backlog warning */}
      {isPaperBacklog && (
        <Card
          data-testid="attention-paper-backlog"
          className="border-orange-200 bg-orange-50/40 dark:border-orange-900/40 dark:bg-orange-950/20"
        >
          <CardContent className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-orange-100 dark:bg-orange-900/50 text-orange-700 dark:text-orange-300">
                <FileSpreadsheet className="size-5" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-foreground">
                  {t("dashboard.attention.paperBacklogTitle", { time: paperTimeText })}
                </h2>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {lastPaperEntry
                    ? t("dashboard.attention.paperBacklogDetail", {
                        paperRef: lastPaperEntry.paperRef ?? t("dashboard.attention.paperRefDefault"),
                        time: paperTimeText,
                        recordedBy: lastPaperEntry.recordedBy ?? t("dashboard.attention.recordedByDefault"),
                      })
                    : t("dashboard.attention.paperBacklogEmpty")}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 5. Kiosk status strip */}
      {kiosks.length > 0 && (
        <Card data-testid="kiosks-status-strip" className="bg-muted/20">
          <CardContent className="p-4">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 mb-3">
              <div className="flex items-center gap-2">
                <MonitorSmartphone className="size-4 text-primary" />
                <h2 className="text-sm font-semibold text-foreground">
                  {t("dashboard.attention.registeredKiosksTitle", { count: kiosks.length })}
                </h2>
              </div>
              {quietKiosks.length > 0 && (
                <span
                  data-testid="quiet-kiosks-badge"
                  className="inline-flex items-center gap-1 rounded-full bg-amber-100 dark:bg-amber-950 px-2 py-0.5 text-xs font-semibold text-amber-800 dark:text-amber-300"
                >
                  <AlertTriangle className="size-3" />
                  {t("dashboard.attention.quietKiosksBadge", {
                    count: quietKiosks.length,
                    plural: quietKiosks.length === 1 ? "" : "s",
                  })}
                </span>
              )}
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2.5">
              {kiosks.map((kiosk) => {
                const isQuiet = quietKiosks.some((q) => q.id === kiosk.id);
                const lastSeenText = kiosk.lastSeenAt
                  ? timeSince(new Date(kiosk.lastSeenAt), t).text
                  : t("dashboard.attention.neverSeen");

                return (
                  <div
                    key={kiosk.id}
                    data-testid={`kiosk-card-${kiosk.id}`}
                    className={cn(
                      "flex items-center justify-between rounded-lg border p-2.5 text-xs transition-colors bg-background",
                      isQuiet && "border-amber-300 dark:border-amber-900 bg-amber-50/30 dark:bg-amber-950/20"
                    )}
                  >
                    <div className="min-w-0 pr-2">
                      <div className="flex items-center gap-1.5">
                        <span className="font-semibold text-foreground truncate">
                          {kiosk.name}
                        </span>
                        <StatusBadge
                          label={kiosk.status}
                          tone={kiosk.status === "active" ? "success" : "muted"}
                        />
                      </div>
                      <div className="text-muted-foreground text-[11px] mt-0.5 truncate">
                        {t("dashboard.attention.kioskSeen", {
                          location: kiosk.location || t("dashboard.attention.locationNotSet"),
                          lastSeen: lastSeenText,
                        })}
                      </div>
                    </div>
                    {isQuiet && (
                      <div
                        title={t("dashboard.attention.quietKioskTooltip")}
                        className="flex size-6 shrink-0 items-center justify-center rounded-full bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300"
                      >
                        <AlertTriangle className="size-3.5" />
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
