import { CheckCircle2, AlertCircle } from "lucide-react";
import type { SessionUser } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { useTranslator } from "@/i18n";
import { renderSessionMessage } from "@/lib/session-message";
import { errorMessage } from "@/lib/problem";
import { formatHumanDueDate } from "@/lib/date-format";
import type { SessionTransaction } from "@/lib/session-transactions";
import { ScreenFrame } from "./screen-frame";

interface SessionSummaryScreenProps {
  transactions: SessionTransaction[];
  user: SessionUser | null;
  kioskName: string;
  onClose: () => void;
}

export function SessionSummaryScreen({ transactions, user, kioskName, onClose }: SessionSummaryScreenProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const failed = transactions.filter((row) => row.status === "failed").length;
  return (
    <ScreenFrame kioskName={kioskName} scannerFresh={false}>
      <div className="flex w-full max-w-3xl flex-col gap-6 py-4" data-testid="session-summary-screen">
        <div className="space-y-2 text-center">
          <h2 className="text-kiosk-prompt font-bold">{t("summary.title")}</h2>
          {user && <p className="text-kiosk-body font-semibold">{user.fullName}</p>}
          <p className="text-lg text-muted-foreground">{t("summary.counts", { total: transactions.length, successful: transactions.length - failed, failed })}</p>
        </div>
        {transactions.length === 0 ? <p className="text-center text-kiosk-body">{t("summary.empty")}</p> : (
          <ol className="max-h-[55dvh] space-y-3 overflow-y-auto" aria-label={t("summary.transactions")}>
            {transactions.map((row, index) => {
              const isFailed = row.status === "failed";
              const localized = renderSessionMessage(row.message, locale);
              const problem = row.problem ? errorMessage(row.problem, t) : null;
              const reason = row.failureReason === "user-required" ? t("summary.userRequired") :
                row.failureReason === "result-unconfirmed" ? t("summary.unconfirmed") :
                row.failureReason === "session-expired" ? t("summary.sessionExpired") :
                localized?.detail || problem?.detail || row.message?.detail || localized?.title || problem?.title || row.message?.title || t("summary.failureUnknown");
              const due = row.status === "borrowed" ? formatHumanDueDate(row.dueAt, undefined, locale) : null;
              return (
                <li key={row.id} className={`rounded-xl border-2 bg-card p-5 ${isFailed ? "border-amber-500/50" : "border-border"}`}>
                  <div className="flex items-start gap-4">
                    {isFailed ? <AlertCircle className="mt-1 size-7 shrink-0 text-amber-700 dark:text-amber-300" aria-hidden="true" /> : <CheckCircle2 className="mt-1 size-7 shrink-0 text-green-700 dark:text-green-300" aria-hidden="true" />}
                    <div className="min-w-0 flex-1 space-y-1">
                      <p className="text-sm font-semibold text-muted-foreground">{t("summary.item", { number: index + 1 })}</p>
                      {row.device && <p className="text-xl font-bold break-words">{row.device.name} <span className="font-mono text-base font-normal text-muted-foreground">{row.device.assetTag}</span></p>}
                      <p className={`text-xl font-bold ${isFailed ? "text-amber-800 dark:text-amber-200" : "text-green-800 dark:text-green-200"}`}>{t(`summary.${row.status}`)}</p>
                      {isFailed && <p className="text-lg break-words">{reason}</p>}
                      {due && <p className="text-lg text-muted-foreground">{due}</p>}
                      {row.problem?.supportCode && <p className="text-sm text-muted-foreground">{t("common.supportCode", { code: row.problem.supportCode })}</p>}
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        )}
        <Button type="button" size="lg" className="min-h-16 w-full text-xl font-bold" onClick={onClose} data-testid="close-session-button">{t("summary.closeSession")}</Button>
      </div>
    </ScreenFrame>
  );
}
