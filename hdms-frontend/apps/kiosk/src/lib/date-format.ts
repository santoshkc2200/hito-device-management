export function formatHumanDueDate(
  dueAt?: string | null,
  baseDate = new Date()
): string | null {
  if (!dueAt || typeof dueAt !== "string" || dueAt.trim() === "") {
    return null;
  }

  try {
    const target = new Date(dueAt);
    if (isNaN(target.getTime())) {
      return null;
    }

    const timeStr = target.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });

    const isSameDay =
      target.getFullYear() === baseDate.getFullYear() &&
      target.getMonth() === baseDate.getMonth() &&
      target.getDate() === baseDate.getDate();

    const tomorrow = new Date(baseDate);
    tomorrow.setDate(tomorrow.getDate() + 1);

    const isTomorrow =
      target.getFullYear() === tomorrow.getFullYear() &&
      target.getMonth() === tomorrow.getMonth() &&
      target.getDate() === tomorrow.getDate();

    if (isSameDay) {
      return `Please return by today, ${timeStr}`;
    }

    if (isTomorrow) {
      return `Please return by tomorrow, ${timeStr}`;
    }

    const dateStr = target.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });

    return `Please return by ${dateStr}, ${timeStr}`;
  } catch {
    return null;
  }
}
