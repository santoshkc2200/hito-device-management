import * as React from "react";
import { formatDate, formatTime, useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { useTranslator } from "@/i18n";
import { formatHumanDueDate } from "@/lib/date-format";
import { pickerDays, returnChips, timeSlots, type ReturnChip } from "@/lib/return-date-options";
import { executeSetDueDate, type SetDueDateInput, type SetDueDateResult } from "@/machine/session-machine";

export interface ReturnByPanelProps {
  sessionId: string;
  loanId: string;
  dueAt: string;
  latestReturnAt: string | null;
  onUpdated: (update: Extract<SetDueDateResult, { ok: true }>) => void;
  onActivity: () => void;
  setDueDate?: (input: SetDueDateInput) => Promise<SetDueDateResult>; // i18n-allow-literal
  now?: () => Date;
}

type Message = { kind: "conflict"; latest: Date } | { kind: "failed" } | null;

// The loan is already open with a default return date. This panel lets the
// borrower change it; walking away keeps the default.
export function ReturnByPanel({
  sessionId,
  loanId,
  dueAt,
  latestReturnAt,
  onUpdated,
  onActivity,
  setDueDate = executeSetDueDate,
  now = () => new Date(),
}: ReturnByPanelProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const [current, setCurrent] = React.useState(dueAt);
  const [latest, setLatest] = React.useState<Date | null>(latestReturnAt ? new Date(latestReturnAt) : null);
  const [saving, setSaving] = React.useState(false);
  const [message, setMessage] = React.useState<Message>(null);
  const [pickerOpen, setPickerOpen] = React.useState(false);
  const [day, setDay] = React.useState<Date | null>(null);

  React.useEffect(() => setCurrent(dueAt), [dueAt]);

  const choose = async (at: Date) => {
    onActivity();
    setSaving(true);
    setMessage(null);
    let result = await setDueDate({ sessionId, loanId, dueAt: at.toISOString() });
    if (!result.ok && result.conflict) {
      const bound = new Date(result.latestReturnAt);
      setLatest(bound);
      setMessage({ kind: "conflict", latest: bound });
      result = await setDueDate({ sessionId, loanId, dueAt: result.latestReturnAt });
    }
    setSaving(false);
    if (result.ok) {
      setCurrent(result.dueAt);
      setLatest(result.latestReturnAt ? new Date(result.latestReturnAt) : null);
      onUpdated(result);
      return;
    }
    if (!result.conflict) {
      setMessage({ kind: "failed" });
    }
  };

  const chipLabel = (chip: ReturnChip) =>
    chip.id === "latest" ? t("returnBy.latest", { time: formatTime(locale, chip.at) }) : t(`returnBy.${chip.id}` as any);

  const clock = now();
  const chips = returnChips(clock, latest);
  const days = pickerDays(clock, latest);
  const slots = day ? timeSlots(day, clock, latest) : [];

  return (
    <section
      data-testid="return-by-panel"
      aria-labelledby="return-by-title"
      className="w-full space-y-3 pt-3 border-t border-border/80"
      onPointerDown={onActivity}
    >
      <h2 id="return-by-title" className="text-lg font-bold text-foreground">
        {t("returnBy.title")}
      </h2>
      <p data-testid="return-by-current" className="text-sm font-semibold text-foreground/90" aria-live="polite">
        {saving ? t("returnBy.saving") : formatHumanDueDate(current, clock, locale)}
      </p>

      <div className="flex flex-wrap gap-2">
        {chips.map((chip) => (
          <Button
            key={chip.id}
            type="button"
            variant={new Date(current).getTime() === chip.at.getTime() ? "default" : "outline"}
            className="min-h-14 px-4 text-xl font-bold"
            disabled={chip.disabled || saving}
            onClick={() => void choose(chip.at)}
          >
            {chipLabel(chip)}
          </Button>
        ))}
        <Button
          type="button"
          variant="outline"
          className="min-h-14 px-4 text-xl font-bold"
          disabled={saving}
          aria-expanded={pickerOpen}
          onClick={() => {
            onActivity();
            setPickerOpen((open) => !open);
            setDay(days[0] ?? null);
          }}
        >
          {t("returnBy.other")}
        </Button>
      </div>

      {pickerOpen && (
        <div className="space-y-2">
          <div role="group" aria-label={t("returnBy.pickDay")} className="flex gap-2 overflow-x-auto pb-1">
            {days.map((d) => (
              <Button
                key={d.getTime()}
                type="button"
                variant={day?.getTime() === d.getTime() ? "default" : "outline"}
                className="min-h-14 shrink-0 px-3 text-xl font-bold"
                onClick={() => {
                  onActivity();
                  setDay(d);
                }}
              >
                {formatDate(locale, d)}
              </Button>
            ))}
          </div>
          <div role="group" aria-label={t("returnBy.pickTime")} className="grid grid-cols-4 gap-2 max-h-56 overflow-y-auto">
            {slots.map((s) => (
              <Button
                key={s.getTime()}
                type="button"
                variant="outline"
                className="min-h-14 text-xl font-bold"
                disabled={saving}
                onClick={() => void choose(s)}
              >
                {formatTime(locale, s)}
              </Button>
            ))}
          </div>
        </div>
      )}

      {message && (
        <p data-testid="return-by-message" role="status" className="text-sm font-semibold text-amber-700 dark:text-amber-400">
          {message.kind === "conflict"
            ? t("returnBy.conflict", { date: formatDate(locale, message.latest), time: formatTime(locale, message.latest) })
            : t("returnBy.failed")}
        </p>
      )}
    </section>
  );
}
