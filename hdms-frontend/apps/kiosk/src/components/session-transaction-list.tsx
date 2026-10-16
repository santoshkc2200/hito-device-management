import { useEffect, useRef } from "react";
import { AlertCircle, CheckCircle2, Clock3 } from "lucide-react";
import { useLocale } from "@hdms/i18n";
import { useTranslator } from "@/i18n";
import { formatHumanDueDate } from "@/lib/date-format";
import { renderSessionMessage } from "@/lib/session-message";
import { errorMessage } from "@/lib/problem";
import type { SessionTransaction } from "@/lib/session-transactions";
import { ReturnByPanel, type ReturnByPanelProps } from "./return-by-panel";

interface SessionTransactionListProps {
  transactions: SessionTransaction[];
  sessionId?: string | null;
  onDueUpdated?: ReturnByPanelProps["onUpdated"];
  isFinishing?: boolean;
}

export function SessionTransactionList({ transactions, sessionId, onDueUpdated, isFinishing }: SessionTransactionListProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const elements = useRef(new Map<number, HTMLLIElement>());
  const previous = useRef(new Map<number, SessionTransaction>());
  useEffect(() => {
    const changed = transactions.filter((row) => previous.current.get(row.id) !== row).at(-1);
    previous.current = new Map(transactions.map((row) => [row.id, row]));
    if (changed) elements.current.get(changed.id)?.scrollIntoView?.({ block: "nearest" });
  }, [transactions]);
  return (
    <section className="w-full space-y-3 text-left" aria-label={t("scanList.title")}>
      <h3 className="text-xl font-bold">{t("scanList.title")} ({transactions.length})</h3>
      <ol data-testid="session-transaction-list" className="max-h-[45dvh] space-y-3 overflow-y-auto" aria-label={t("scanList.title")} aria-live="polite" aria-relevant="additions text">
        {transactions.map((row, index) => {
          const pending = row.status === "pending";
          const failed = row.status === "failed";
          const Icon = pending ? Clock3 : failed ? AlertCircle : CheckCircle2;
          const message = renderSessionMessage(row.message, locale);
          const problem = row.problem ? errorMessage(row.problem, t) : null;
          const reason = row.failureReason === "user-required" ? t("summary.userRequired") :
            row.failureReason === "result-unconfirmed" ? t("summary.unconfirmed") :
            row.failureReason === "session-expired" ? t("summary.sessionExpired") :
            message?.detail || problem?.detail || row.message?.detail || message?.title || problem?.title || row.message?.title || t("summary.failureUnknown");
          // An earlier borrow remains in the history after a return or a new borrow,
          // but its return date must no longer be editable.
          const superseded = transactions.slice(index + 1).some((later) =>
            later.device?.id === row.device?.id && (later.status === "returned" || later.status === "borrowed"));
          const editable = row.status === "borrowed" && !superseded && sessionId && row.loanId && row.dueAt && onDueUpdated;
          return (
            <li key={row.id} ref={(element) => {
              if (element) elements.current.set(row.id, element);
              else elements.current.delete(row.id);
            }} className={`rounded-xl border-2 bg-card p-4 ${failed ? "border-amber-500/50" : "border-border"}`}>
              <div className="flex items-start gap-3">
                <Icon aria-hidden="true" className={`mt-1 size-6 shrink-0 ${pending ? "text-muted-foreground" : failed ? "text-amber-700 dark:text-amber-300" : "text-green-700 dark:text-green-300"}`} />
                <div className="min-w-0 flex-1 space-y-1">
                  <p className="text-xl font-bold break-words">{row.device?.name || t("summary.item", { number: index + 1 })}</p>
                  {row.device && <p className="font-mono text-base text-muted-foreground break-words">{row.device.assetTag}</p>}
                  <p className="text-lg font-semibold">{pending ? t("scanList.awaitingId") : t(`summary.${row.status}`)}</p>
                  {failed && <p className="text-lg break-words">{reason}</p>}
                  {row.status === "borrowed" && row.dueAt && <p className="text-base text-muted-foreground">{formatHumanDueDate(row.dueAt, undefined, locale)}</p>}
                  {row.problem?.supportCode && <p className="text-sm text-muted-foreground">{t("common.supportCode", { code: row.problem.supportCode })}</p>}
                </div>
              </div>
              {editable && (
                <details open className="mt-3">
                  <summary className="min-h-12 cursor-pointer py-3 text-lg font-semibold text-primary">{t("returnBy.title")}</summary>
                  <fieldset disabled={isFinishing}>
                    <ReturnByPanel ariaLabel={`${t("returnBy.title")} · ${row.device?.assetTag || row.loanId}`} sessionId={sessionId!} loanId={row.loanId!} dueAt={row.dueAt!} latestReturnAt={row.latestReturnAt ?? null} onUpdated={onDueUpdated!} onActivity={() => {}} />
                  </fieldset>
                </details>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}
