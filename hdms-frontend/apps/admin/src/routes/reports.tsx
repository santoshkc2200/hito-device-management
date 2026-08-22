import {
  type OperationalHealth,
  type OriginReport,
  type ReportSummary,
  getOperationalHealth,
  getReportSummary,
  getTransactionsByOrigin,
} from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";
import { createRoute, useNavigate } from "@tanstack/react-router";
import {
  Activity,
  AlertTriangle,
  BarChart3,
  Calendar,
  Clock,
  Download,
  FileSpreadsheet,
  Layers,
  TrendingUp,
  Users,
} from "lucide-react";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { EmptyState, ErrorState } from "@/components/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { authenticatedRoute } from "./authenticated";

const reportsSearchSchema = z.object({
  from: z.string().optional(),
  to: z.string().optional(),
  tab: z.enum(["summary", "origin", "health"]).optional(),
  bucket: z.enum(["day", "week", "month"]).optional(),
});
export type ReportsSearch = z.infer<typeof reportsSearchSchema>;

type DatePreset = "today" | "7d" | "30d" | "90d" | "custom";

function computePresetDates(preset: DatePreset): { from: string; to: string } {
  const now = new Date();
  const to = now.toISOString();

  let fromDate: Date;
  switch (preset) {
    case "today": {
      fromDate = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      break;
    }
    case "7d": {
      fromDate = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
      break;
    }
    case "90d": {
      fromDate = new Date(now.getTime() - 90 * 24 * 60 * 60 * 1000);
      break;
    }
    case "30d":
    default: {
      fromDate = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
      break;
    }
  }
  return { from: fromDate.toISOString(), to };
}

function formatDate(iso?: string | null): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

function downloadFile(url: string, filename: string) {
  const link = document.createElement("a");
  link.href = url;
  link.setAttribute("download", filename);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  toast.success(`Exporting ${filename}`);
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary Tab Component
// ─────────────────────────────────────────────────────────────────────────────

function SummaryTabContent({ summary }: { summary: ReportSummary }) {
  return (
    <div className="space-y-6">
      {/* 4 Stat Tiles */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total Transactions
            </CardTitle>
            <Layers className="h-4 w-4 text-primary" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="total-loans">
              {summary.totalLoans}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Loans initiated within selected window
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Open Loans
            </CardTitle>
            <Clock className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="open-loans">
              {summary.openLoans}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Currently in circulation
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Overdue Loans
            </CardTitle>
            <AlertTriangle className="h-4 w-4 text-destructive" />
          </CardHeader>
          <CardContent>
            <div className="flex items-baseline gap-2">
              <span className="text-2xl font-bold text-destructive" data-testid="overdue-count">
                {summary.overdueCount}
              </span>
              <span className="text-xs text-muted-foreground">
                ({(summary.overdueRate * 100).toFixed(1)}% rate)
              </span>
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Past expected return deadline
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Avg Loan Duration
            </CardTitle>
            <TrendingUp className="h-4 w-4 text-emerald-600" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="avg-duration">
              {summary.averageLoanDurationHours !== undefined && summary.averageLoanDurationHours !== null
                ? `${summary.averageLoanDurationHours.toFixed(1)}h`
                : "—"}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Average custody holding time
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Category Utilisation & Top Borrowers */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* Category Utilisation Table */}
        <Card className="lg:col-span-7">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Category Utilisation</CardTitle>
            <CardDescription>
              Fleet utilization percentage based on cumulative hours held vs total fleet capacity.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            {summary.utilisationByCategory.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">
                No category data available in this timeframe.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Category</TableHead>
                    <TableHead className="text-right">Live Fleet</TableHead>
                    <TableHead className="text-right">Loans</TableHead>
                    <TableHead className="text-right">Avg Hours</TableHead>
                    <TableHead className="w-36 text-right">Utilisation %</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {summary.utilisationByCategory.map((cat) => (
                    <TableRow key={cat.categoryId}>
                      <TableCell className="font-medium">{cat.categoryName}</TableCell>
                      <TableCell className="text-right">{cat.deviceCount}</TableCell>
                      <TableCell className="text-right">{cat.loanCount}</TableCell>
                      <TableCell className="text-right">
                        {cat.averageDurationHours !== undefined && cat.averageDurationHours !== null
                          ? `${cat.averageDurationHours.toFixed(1)}h`
                          : "—"}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-2">
                          <div className="w-16 bg-secondary rounded-full h-2 overflow-hidden">
                            <div
                              className="bg-primary h-2 rounded-full"
                              style={{ width: `${Math.min(100, Math.max(0, cat.utilisationPct))}%` }}
                            />
                          </div>
                          <span className="text-xs font-medium w-10 text-right">
                            {cat.utilisationPct.toFixed(1)}%
                          </span>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Top Borrowers Table */}
        <Card className="lg:col-span-5">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Top Borrowers</CardTitle>
            <CardDescription>
              Staff members with the highest transaction volume in this window.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            {summary.topBorrowers.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">
                No borrowing activity recorded in this timeframe.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Staff Member</TableHead>
                    <TableHead>Employee No</TableHead>
                    <TableHead className="text-right">Loan Count</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {summary.topBorrowers.map((borrower) => (
                    <TableRow key={borrower.userId}>
                      <TableCell className="font-medium">{borrower.fullName}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {borrower.employeeNo || "—"}
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        <Badge variant="secondary">{borrower.loanCount}</Badge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Origin Trends Tab Component
// ─────────────────────────────────────────────────────────────────────────────

function OriginTabContent({ report }: { report: OriginReport }) {
  if (report.buckets.length === 0) {
    return (
      <EmptyState
        icon={BarChart3}
        title="No transaction history"
        explanation="There were no loan transactions recorded during the selected period."
      />
    );
  }

  return (
    <div className="space-y-6">
      {/* Visual representation of buckets */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold">Origin Breakdown over Time</CardTitle>
          <CardDescription>
            Comparison of transaction sources (Kiosk, Paper Backfill, Admin Overrides, Bulk Import).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {report.buckets.map((b) => (
              <div key={b.periodStart} className="space-y-1.5">
                <div className="flex items-center justify-between text-xs">
                  <span className="font-medium">{formatDate(b.periodStart)}</span>
                  <span className="text-muted-foreground">{b.total} transactions</span>
                </div>
                {b.total > 0 ? (
                  <div className="w-full bg-secondary h-4 rounded flex overflow-hidden">
                    {b.counts.map((c) => {
                      const pct = (c.count / b.total) * 100;
                      if (pct <= 0) return null;
                      const bg =
                        c.origin === "kiosk"
                          ? "bg-emerald-500"
                          : c.origin === "paper"
                          ? "bg-amber-500"
                          : c.origin === "admin"
                          ? "bg-blue-500"
                          : "bg-purple-500";
                      return (
                        <div
                          key={c.origin}
                          style={{ width: `${pct}%` }}
                          className={`${bg} h-full transition-all`}
                          title={`${c.origin}: ${c.count} (${pct.toFixed(0)}%)`}
                        />
                      );
                    })}
                  </div>
                ) : (
                  <div className="w-full bg-secondary/50 h-4 rounded flex items-center justify-center text-[10px] text-muted-foreground">
                    0 transactions
                  </div>
                )}
              </div>
            ))}
          </div>

          <div className="flex items-center justify-center gap-6 mt-6 pt-4 border-t text-xs text-muted-foreground">
            <div className="flex items-center gap-2">
              <div className="h-3 w-3 rounded bg-emerald-500" />
              <span>Kiosk</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-3 w-3 rounded bg-amber-500" />
              <span>Paper Backfill</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-3 w-3 rounded bg-blue-500" />
              <span>Admin Override</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-3 w-3 rounded bg-purple-500" />
              <span>Bulk Import</span>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Bucket breakdown Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold">Period Totals</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Period Start</TableHead>
                <TableHead className="text-right">Kiosk</TableHead>
                <TableHead className="text-right">Paper</TableHead>
                <TableHead className="text-right">Admin</TableHead>
                <TableHead className="text-right">Import</TableHead>
                <TableHead className="text-right font-semibold">Total</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {report.buckets.map((b) => {
                const getOriginCount = (orig: string) =>
                  b.counts.find((c) => c.origin === orig)?.count || 0;
                return (
                  <TableRow key={b.periodStart}>
                    <TableCell className="font-medium">{formatDate(b.periodStart)}</TableCell>
                    <TableCell className="text-right">{getOriginCount("kiosk")}</TableCell>
                    <TableCell className="text-right">{getOriginCount("paper")}</TableCell>
                    <TableCell className="text-right">{getOriginCount("admin")}</TableCell>
                    <TableCell className="text-right">{getOriginCount("import")}</TableCell>
                    <TableCell className="text-right font-bold">{b.total}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Operational Health Tab Component
// ─────────────────────────────────────────────────────────────────────────────

function HealthTabContent({ health }: { health: OperationalHealth }) {
  return (
    <div className="space-y-6">
      {/* 3 Stat Tiles */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total Scans Attempted
            </CardTitle>
            <Activity className="h-4 w-4 text-primary" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="total-scans">
              {health.totalScans}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Live barcode & token scans across all kiosks
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Manual Entries
            </CardTitle>
            <Clock className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="manual-scans">
              {health.manualEntryCount}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Typing fallback used instead of optical scanning
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Camera Fallbacks
            </CardTitle>
            <TrendingUp className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold" data-testid="camera-scans">
              {health.cameraFallbackCount}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Webcam scanned when USB scanner was unavailable
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Scans by Source & Rejection Reasons */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* Scans by Source */}
        <Card className="lg:col-span-6">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Scans by Input Source</CardTitle>
            <CardDescription>
              Hardware scanner reliability vs fallback channels.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {health.scansBySource.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">
                No scan events recorded in this timeframe.
              </div>
            ) : (
              <div className="space-y-4">
                {health.scansBySource.map((s) => {
                  const pct = health.totalScans > 0 ? (s.count / health.totalScans) * 100 : 0;
                  return (
                    <div key={s.source} className="space-y-1.5">
                      <div className="flex items-center justify-between text-sm">
                        <span className="font-medium capitalize">{s.source}</span>
                        <span className="text-muted-foreground">
                          {s.count} ({pct.toFixed(1)}%)
                        </span>
                      </div>
                      <div className="w-full bg-secondary h-2.5 rounded-full overflow-hidden">
                        <div
                          className="bg-primary h-full rounded-full"
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

        {/* Turnaway / Rejection Reasons */}
        <Card className="lg:col-span-6">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Staff Turnaway Signals</CardTitle>
            <CardDescription>
              Reasons staff members were turned away at kiosks (e.g. unissued badges).
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            {health.rejectionReasons.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground">
                No rejections recorded. Kiosks operated with 100% success.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Rejection Reason</TableHead>
                    <TableHead>Classification</TableHead>
                    <TableHead className="text-right">Turnaways</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {health.rejectionReasons.map((rr, idx) => (
                    <TableRow key={idx}>
                      <TableCell className="font-medium">
                        {rr.reason || "Unrecognised or invalid token"}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="capitalize">
                          {rr.resolvedType || "unknown"}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right font-bold text-destructive">
                        {rr.count}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Main Reports Page
// ─────────────────────────────────────────────────────────────────────────────

function ReportsPage() {
  const navigate = useNavigate({ from: reportsRoute.fullPath });
  const search = reportsRoute.useSearch();

  const activeTab = search.tab || "summary";
  const activeBucket = search.bucket || "day";

  // Initial default range: last 30 days
  const defaultDates = useMemo(() => computePresetDates("30d"), []);
  const fromIso = search.from || defaultDates.from;
  const toIso = search.to || defaultDates.to;

  const [preset, setPreset] = useState<DatePreset>("30d");
  const [customFrom, setCustomFrom] = useState<string>(fromIso.slice(0, 10));
  const [customTo, setCustomTo] = useState<string>(toIso.slice(0, 10));

  const handlePresetChange = (newPreset: DatePreset) => {
    setPreset(newPreset);
    if (newPreset === "custom") return;
    const { from, to } = computePresetDates(newPreset);
    setCustomFrom(from.slice(0, 10));
    setCustomTo(to.slice(0, 10));
    navigate({
      search: (prev) => ({
        ...prev,
        from,
        to,
      }),
    });
  };

  const handleApplyCustomDates = () => {
    if (!customFrom || !customTo) return;
    const fromDate = new Date(`${customFrom}T00:00:00.000Z`);
    const toDate = new Date(`${customTo}T23:59:59.999Z`);
    if (fromDate > toDate) {
      toast.error("'From' date must be before 'To' date");
      return;
    }
    navigate({
      search: (prev) => ({
        ...prev,
        from: fromDate.toISOString(),
        to: toDate.toISOString(),
      }),
    });
  };

  const handleTabChange = (newTab: string) => {
    navigate({
      search: (prev) => ({
        ...prev,
        tab: newTab as "summary" | "origin" | "health",
      }),
    });
  };

  const handleBucketChange = (newBucket: string) => {
    navigate({
      search: (prev) => ({
        ...prev,
        bucket: newBucket as "day" | "week" | "month",
      }),
    });
  };

  // Queries
  const summaryQuery = useQuery({
    queryKey: ["reports", "summary", fromIso, toIso],
    queryFn: async () => {
      const { data, error } = await getReportSummary({
        query: {
          from: fromIso,
          to: toIso,
        },
      });
      if (error) throw error;
      return data;
    },
    enabled: activeTab === "summary",
  });

  const originQuery = useQuery({
    queryKey: ["reports", "origin", fromIso, toIso, activeBucket],
    queryFn: async () => {
      const { data, error } = await getTransactionsByOrigin({
        query: {
          from: fromIso,
          to: toIso,
          bucket: activeBucket,
        },
      });
      if (error) throw error;
      return data;
    },
    enabled: activeTab === "origin",
  });

  const healthQuery = useQuery({
    queryKey: ["reports", "health", fromIso, toIso],
    queryFn: async () => {
      const { data, error } = await getOperationalHealth({
        query: {
          from: fromIso,
          to: toIso,
        },
      });
      if (error) throw error;
      return data;
    },
    enabled: activeTab === "health",
  });

  return (
    <div className="flex flex-col gap-6" data-testid="reports-page">
      {/* Header with Title and Export Controls */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Reports & Analytics</h1>
          <p className="text-sm text-muted-foreground">
            Utilization metrics, transaction origin trends, and operational kiosk health.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" className="gap-2" data-testid="export-csv-dropdown">
                <Download className="h-4 w-4" />
                <span>Export CSV</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() =>
                  downloadFile(
                    `/v1/reports/loans.csv?from=${encodeURIComponent(fromIso)}&to=${encodeURIComponent(toIso)}`,
                    `loans-report-${customFrom}-to-${customTo}.csv`
                  )
                }
                data-testid="export-loans-csv"
              >
                <FileSpreadsheet className="mr-2 h-4 w-4 text-primary" />
                <span>Export Loans (.csv)</span>
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() =>
                  downloadFile(`/v1/reports/devices.csv`, `devices-export-${new Date().toISOString().slice(0, 10)}.csv`)
                }
                data-testid="export-devices-csv"
              >
                <Layers className="mr-2 h-4 w-4 text-primary" />
                <span>Export Devices (.csv)</span>
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() =>
                  downloadFile(`/v1/reports/users.csv`, `users-export-${new Date().toISOString().slice(0, 10)}.csv`)
                }
                data-testid="export-users-csv"
              >
                <Users className="mr-2 h-4 w-4 text-primary" />
                <span>Export Users (.csv)</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Date Range Selector Toolbar */}
      <Card className="bg-card/50">
        <CardContent className="p-4 flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
              <Calendar className="h-4 w-4" />
              <span>Timeframe:</span>
            </div>
            <div className="flex items-center gap-1.5">
              {(["today", "7d", "30d", "90d", "custom"] as const).map((p) => (
                <Button
                  key={p}
                  size="sm"
                  variant={preset === p ? "default" : "outline"}
                  onClick={() => handlePresetChange(p)}
                  className="h-8 text-xs capitalize"
                  data-testid={`preset-${p}`}
                >
                  {p === "7d" ? "7 Days" : p === "30d" ? "30 Days" : p === "90d" ? "90 Days" : p}
                </Button>
              ))}
            </div>
          </div>

          {preset === "custom" && (
            <div className="flex items-center gap-2 text-xs">
              <div className="flex items-center gap-1">
                <Label htmlFor="custom-from" className="text-xs">From:</Label>
                <Input
                  id="custom-from"
                  type="date"
                  value={customFrom}
                  onChange={(e) => setCustomFrom(e.target.value)}
                  className="h-8 w-36 text-xs"
                />
              </div>
              <div className="flex items-center gap-1">
                <Label htmlFor="custom-to" className="text-xs">To:</Label>
                <Input
                  id="custom-to"
                  type="date"
                  value={customTo}
                  onChange={(e) => setCustomTo(e.target.value)}
                  className="h-8 w-36 text-xs"
                />
              </div>
              <Button size="sm" onClick={handleApplyCustomDates} className="h-8 text-xs">
                Apply
              </Button>
            </div>
          )}

          <div className="text-xs text-muted-foreground">
            Showing data from <span className="font-medium text-foreground">{formatDate(fromIso)}</span> to{" "}
            <span className="font-medium text-foreground">{formatDate(toIso)}</span>
          </div>
        </CardContent>
      </Card>

      {/* Main Tabs */}
      <Tabs value={activeTab} onValueChange={handleTabChange} className="space-y-6">
        <TabsList className="grid grid-cols-3 w-full max-w-md">
          <TabsTrigger value="summary" className="gap-2" data-testid="tab-summary">
            <TrendingUp className="h-4 w-4" />
            <span>Summary</span>
          </TabsTrigger>
          <TabsTrigger value="origin" className="gap-2" data-testid="tab-origin">
            <BarChart3 className="h-4 w-4" />
            <span>Origin Trends</span>
          </TabsTrigger>
          <TabsTrigger value="health" className="gap-2" data-testid="tab-health">
            <Activity className="h-4 w-4" />
            <span>Operational Health</span>
          </TabsTrigger>
        </TabsList>

        {/* Tab 1: Summary */}
        <TabsContent value="summary" className="space-y-6">
          {summaryQuery.isLoading ? (
            <div className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-28 rounded-xl" />
                ))}
              </div>
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                <Skeleton className="h-64 rounded-xl" />
                <Skeleton className="h-64 rounded-xl" />
              </div>
            </div>
          ) : summaryQuery.isError || !summaryQuery.data ? (
            <ErrorState
              title="Could not load report summary"
              detail="Failed to compute utilization and duration metrics. Please try again."
              onRetry={() => summaryQuery.refetch()}
            />
          ) : (
            <SummaryTabContent summary={summaryQuery.data} />
          )}
        </TabsContent>

        {/* Tab 2: Origin Over Time */}
        <TabsContent value="origin" className="space-y-6">
          <div className="flex items-center justify-between">
            <div className="text-sm font-medium text-muted-foreground">
              Grouping interval:
            </div>
            <div className="flex items-center gap-1.5">
              {(["day", "week", "month"] as const).map((b) => (
                <Button
                  key={b}
                  size="sm"
                  variant={activeBucket === b ? "default" : "outline"}
                  onClick={() => handleBucketChange(b)}
                  className="h-8 text-xs capitalize"
                  data-testid={`bucket-${b}`}
                >
                  {b}
                </Button>
              ))}
            </div>
          </div>

          {originQuery.isLoading ? (
            <div className="space-y-4">
              <Skeleton className="h-72 rounded-xl" />
              <Skeleton className="h-48 rounded-xl" />
            </div>
          ) : originQuery.isError || !originQuery.data ? (
            <ErrorState
              title="Could not load origin trends"
              detail="Failed to aggregate transaction counts by origin. Please try again."
              onRetry={() => originQuery.refetch()}
            />
          ) : (
            <OriginTabContent report={originQuery.data} />
          )}
        </TabsContent>

        {/* Tab 3: Operational Health */}
        <TabsContent value="health" className="space-y-6">
          {healthQuery.isLoading ? (
            <div className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-28 rounded-xl" />
                ))}
              </div>
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                <Skeleton className="h-64 rounded-xl" />
                <Skeleton className="h-64 rounded-xl" />
              </div>
            </div>
          ) : healthQuery.isError || !healthQuery.data ? (
            <ErrorState
              title="Could not load operational health metrics"
              detail="Failed to fetch scan breakdown and turnaway counts. Please try again."
              onRetry={() => healthQuery.refetch()}
            />
          ) : (
            <HealthTabContent health={healthQuery.data} />
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const reportsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/reports",
  validateSearch: reportsSearchSchema,
  component: ReportsPage,
});
