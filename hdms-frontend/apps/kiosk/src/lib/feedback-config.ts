import type { Outcome, OutcomeKind } from "@hdms/api-client";
import { AlertCircle, CalendarCheck, CheckCircle2, Clock, Copy, CornerDownLeft, UserCheck, Users, type LucideIcon } from "lucide-react";

export type SoundId = "borrow" | "return" | "reject" | "accepted" | "none";

export interface OutcomeFeedbackEntry {
  kind: OutcomeKind;
  word: string;
  icon: LucideIcon;
  colorClasses: {
    iconContainer: string;
    badge: string;
    text: string;
  };
  soundId: SoundId;
  entranceClass: string;
  durationMs: number;
}

export const OUTCOME_FEEDBACK_MAP: Record<OutcomeKind, OutcomeFeedbackEntry> = {
  borrowed: {
    kind: "borrowed",
    word: "Borrowed",
    icon: CheckCircle2,
    colorClasses: {
      iconContainer: "bg-green-500/15 text-green-600 dark:text-green-400",
      badge: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-200",
      text: "text-green-700 dark:text-green-300",
    },
    soundId: "borrow",
    entranceClass: "animate-feedback-success",
    durationMs: 4000,
  },
  // Phase 6.4d: a collected reservation is an ordinary loan, so it shares
  // the borrow sound and success animation — only the word and the icon
  // tell the reserver their booking is what just worked.
  reservation_collected: {
    kind: "reservation_collected",
    word: "Collected",
    icon: CalendarCheck,
    colorClasses: {
      iconContainer: "bg-green-500/15 text-green-600 dark:text-green-400",
      badge: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-200",
      text: "text-green-700 dark:text-green-300",
    },
    soundId: "borrow",
    entranceClass: "animate-feedback-success",
    durationMs: 4000,
  },
  returned: {
    kind: "returned",
    word: "Returned",
    icon: CornerDownLeft,
    colorClasses: {
      iconContainer: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
      badge: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-200",
      text: "text-blue-700 dark:text-blue-300",
    },
    soundId: "return",
    entranceClass: "animate-feedback-info",
    durationMs: 4000,
  },
  rejected: {
    kind: "rejected",
    word: "Blocked",
    icon: AlertCircle,
    colorClasses: {
      iconContainer: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
      badge: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
      text: "text-amber-800 dark:text-amber-200",
    },
    soundId: "reject",
    entranceClass: "animate-feedback-reject",
    durationMs: 8000,
  },
  device_pending: {
    kind: "device_pending",
    word: "Device Held",
    icon: Clock,
    colorClasses: {
      iconContainer: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
      badge: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200",
      text: "text-sky-700 dark:text-sky-300",
    },
    soundId: "accepted",
    entranceClass: "animate-feedback-info",
    durationMs: 0,
  },
  user_identified: {
    kind: "user_identified",
    word: "User Identified",
    icon: UserCheck,
    colorClasses: {
      iconContainer: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
      badge: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200",
      text: "text-sky-700 dark:text-sky-300",
    },
    soundId: "accepted",
    entranceClass: "animate-feedback-info",
    durationMs: 0,
  },
  user_switched: {
    kind: "user_switched",
    word: "User Switched",
    icon: Users,
    colorClasses: {
      iconContainer: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
      badge: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200",
      text: "text-sky-700 dark:text-sky-300",
    },
    soundId: "accepted",
    entranceClass: "animate-feedback-info",
    durationMs: 0,
  },
  duplicate: {
    kind: "duplicate",
    word: "Duplicate",
    icon: Copy,
    colorClasses: {
      iconContainer: "bg-muted text-muted-foreground",
      badge: "bg-muted text-muted-foreground",
      text: "text-muted-foreground",
    },
    soundId: "none",
    entranceClass: "",
    durationMs: 0,
  },
};

export const ALL_OUTCOME_KINDS: OutcomeKind[] = [
  "borrowed",
  "returned",
  "rejected",
  "device_pending",
  "user_identified",
  "user_switched",
  "duplicate",
];

export function getOutcomeFeedback(kind?: OutcomeKind | string | null): OutcomeFeedbackEntry {
  if (kind && kind in OUTCOME_FEEDBACK_MAP) {
    return OUTCOME_FEEDBACK_MAP[kind as OutcomeKind];
  }
  return {
    kind: (kind as OutcomeKind) ?? "rejected",
    word: "Notice",
    icon: AlertCircle,
    colorClasses: {
      iconContainer: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
      badge: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
      text: "text-amber-800 dark:text-amber-200",
    },
    soundId: "reject",
    entranceClass: "animate-feedback-reject",
    durationMs: 8000,
  };
}

// outcomeSoundKey identifies one outcome for audio feedback, so the sound
// plays once per transaction. Keyed by loan rather than due date: changing a
// loan's return date updates the outcome in place and must not replay it.
export function outcomeSoundKey(outcome: Outcome): string {
  return `outcome_${outcome.kind}_${outcome.loanId ?? outcome.device?.id ?? ""}`;
}
