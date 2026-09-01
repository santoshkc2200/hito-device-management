import { formatDate, formatTime, translate, type Locale } from "@hdms/i18n";
import { catalogue } from "@/i18n";

export interface DueDateParts {
  kind: "today" | "tomorrow" | "other";
  date: Date;
}

export function dueDateParts(
  dueAt?: string | null,
  baseDate = new Date()
): DueDateParts | null {
  if (!dueAt || typeof dueAt !== "string" || dueAt.trim() === "") {
    return null;
  }

  try {
    const target = new Date(dueAt);
    if (isNaN(target.getTime())) {
      return null;
    }

    const isSameDay =
      target.getFullYear() === baseDate.getFullYear() &&
      target.getMonth() === baseDate.getMonth() &&
      target.getDate() === baseDate.getDate();

    if (isSameDay) {
      return { kind: "today", date: target };
    }

    const tomorrow = new Date(baseDate);
    tomorrow.setDate(tomorrow.getDate() + 1);

    const isTomorrow =
      target.getFullYear() === tomorrow.getFullYear() &&
      target.getMonth() === tomorrow.getMonth() &&
      target.getDate() === tomorrow.getDate();

    if (isTomorrow) {
      return { kind: "tomorrow", date: target };
    }

    return { kind: "other", date: target };
  } catch {
    return null;
  }
}

export function formatHumanDueDate(
  dueAt?: string | null,
  baseDate = new Date(),
  locale: Locale = "en"
): string | null {
  const parts = dueDateParts(dueAt, baseDate);
  if (!parts) {
    return null;
  }

  const time = formatTime(locale, parts.date);

  if (parts.kind === "today") {
    return translate(catalogue, locale, "outcome.dueToday", { time });
  }

  if (parts.kind === "tomorrow") {
    return translate(catalogue, locale, "outcome.dueTomorrow", { time });
  }

  const date = formatDate(locale, parts.date);
  return translate(catalogue, locale, "outcome.dueOther", { date, time });
}
