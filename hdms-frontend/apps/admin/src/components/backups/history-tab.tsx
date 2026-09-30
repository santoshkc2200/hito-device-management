import { listBackupRuns, type BackupRun } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useQuery } from "@tanstack/react-query";
import { Fragment, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { outcomeTone } from "./overview-tab";

type RepoResult = { name?: string; outcome?: string; error?: string };

function repoResults(run: BackupRun): RepoResult[] {
  const d = run.detail as { destinations?: RepoResult[]; repositories?: RepoResult[]; error?: string };
  const rows = d.destinations ?? d.repositories ?? [];
  return d.error ? [...rows, { outcome: "failure", error: d.error }] : rows;
}

export function HistoryTab() {
  const t = useT();
  const { locale } = useLocale();
  const [open, setOpen] = useState<number | null>(null);
  const runsQuery = useQuery({
    queryKey: ["backup", "runs"],
    queryFn: async () => {
      const res = await listBackupRuns({ query: { limit: 20 } });
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
  const runs = useMemo(() => runsQuery.data ?? [], [runsQuery.data]);

  if (!runsQuery.isPending && runs.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("backups.history.empty")}</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("backups.history.columns.startedAt")}</TableHead>
          <TableHead>{t("backups.history.columns.job")}</TableHead>
          <TableHead>{t("backups.history.columns.outcome")}</TableHead>
          <TableHead>{t("backups.history.columns.details")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {runs.map((run) => {
          const details = repoResults(run);
          const expanded = open === run.id;
          return (
            <Fragment key={run.id}>
              <TableRow>
                <TableCell>{formatDateTime(run.startedAt, locale)}</TableCell>
                <TableCell>{t(`backups.history.jobs.${run.job}` as never)}</TableCell>
                <TableCell>
                  <Badge variant={outcomeTone(run.outcome)}>{t(`backups.outcome.${run.outcome}` as never)}</Badge>
                </TableCell>
                <TableCell>
                  {details.length > 0 && (
                    <Button variant="ghost" size="sm" onClick={() => setOpen(expanded ? null : run.id)}>
                      {expanded ? t("backups.history.hideDetails") : t("backups.history.showDetails")}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
              {expanded && (
                <TableRow key={`${run.id}-detail`}>
                  <TableCell colSpan={4}>
                    <ul className="flex flex-col gap-1 text-xs">
                      {details.map((d, i) => (
                        <li key={i} className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{d.name ?? t("backups.history.destination")}</span>
                          <Badge variant={outcomeTone(d.outcome)}>{t(`backups.outcome.${d.outcome ?? "failure"}` as never)}</Badge>
                          {d.error && <span className="text-destructive">{d.error}</span>}
                        </li>
                      ))}
                    </ul>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          );
        })}
      </TableBody>
    </Table>
  );
}
