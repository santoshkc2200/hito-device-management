// Return-date choices for the kiosk's "Return by" panel. Everything is in
// the kiosk's local time; the API carries ISO instants, so converting at
// the edges is enough.

export const END_OF_DAY_HOUR = 17;
export const SLOT_MINUTES = 15;
const UNBOUNDED_DAYS = 31;

export type ReturnChipId = "today" | "tomorrow" | "plus3" | "plus7" | "latest";

export interface ReturnChip {
  id: ReturnChipId;
  at: Date;
  disabled: boolean;
}

function atHour(base: Date, addDays: number, hour: number): Date {
  const d = new Date(base);
  d.setDate(d.getDate() + addDays);
  d.setHours(hour, 0, 0, 0);
  return d;
}

function startOfDay(d: Date): Date {
  return atHour(d, 0, 0);
}

export function returnChips(now: Date, latest: Date | null): ReturnChip[] {
  const candidates: { id: ReturnChipId; at: Date }[] = [
    { id: "today", at: atHour(now, 0, END_OF_DAY_HOUR) },
    { id: "tomorrow", at: atHour(now, 1, END_OF_DAY_HOUR) },
    { id: "plus3", at: atHour(now, 3, END_OF_DAY_HOUR) },
    { id: "plus7", at: atHour(now, 7, END_OF_DAY_HOUR) },
  ];
  const chips: ReturnChip[] = candidates
    .filter((c) => c.at.getTime() > now.getTime())
    .map((c) => ({ ...c, disabled: latest !== null && c.at.getTime() > latest.getTime() }));
  if (latest !== null && chips.every((c) => c.disabled)) {
    chips.unshift({ id: "latest", at: latest, disabled: false });
  }
  return chips;
}

export function pickerDays(now: Date, latest: Date | null): Date[] {
  const first = startOfDay(now);
  const last = latest ? startOfDay(latest) : atHour(now, UNBOUNDED_DAYS - 1, 0);
  const days: Date[] = [];
  for (let d = first; d.getTime() <= last.getTime(); d = atHour(d, 1, 0)) {
    days.push(d);
  }
  return days;
}

export function timeSlots(day: Date, now: Date, latest: Date | null): Date[] {
  const slots: Date[] = [];
  const start = startOfDay(day);
  for (let i = 0; i < (24 * 60) / SLOT_MINUTES; i++) {
    const slot = new Date(start);
    slot.setMinutes(i * SLOT_MINUTES);
    if (slot.getTime() <= now.getTime()) continue;
    if (latest !== null && slot.getTime() > latest.getTime()) break;
    slots.push(slot);
  }
  return slots;
}
