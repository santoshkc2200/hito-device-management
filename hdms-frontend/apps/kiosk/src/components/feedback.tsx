import * as React from "react";
import type { Outcome, SessionMessage } from "@hdms/api-client";
import { getOutcomeFeedback } from "@/lib/feedback-config";

export interface FeedbackProps {
  outcome?: Outcome | null;
  message?: SessionMessage | null;
  className?: string;
  onDismiss?: () => void;
}

export function Feedback({
  outcome,
  message,
  className = "",
  onDismiss: _onDismiss,
}: FeedbackProps) {
  const lastOutcomeIdRef = React.useRef<string | null>(null);
  const [announcedCount, setAnnouncedCount] = React.useState(0);

  // Track outcome identity to fire once per outcome (avoid double-announce on re-renders)
  const currentOutcomeId = outcome && outcome.kind !== "duplicate"
    ? `${outcome.kind}_${outcome.device?.id ?? ""}_${outcome.dueAt ?? ""}_${message?.title ?? ""}`
    : null;

  React.useEffect(() => {
    if (currentOutcomeId && lastOutcomeIdRef.current !== currentOutcomeId) {
      lastOutcomeIdRef.current = currentOutcomeId;
      setAnnouncedCount((c) => c + 1);
    }
  }, [currentOutcomeId]);

  // If no outcome or outcome is duplicate, produce NO visual feedback
  if (!outcome || outcome.kind === "duplicate") {
    return null;
  }

  const feedbackConfig = getOutcomeFeedback(outcome.kind);
  const IconComponent = feedbackConfig.icon;

  const title = message?.title || feedbackConfig.word;
  const detail = message?.detail || (outcome.kind === "returned" ? "Return confirmed." : "Scan processed.");

  return (
    <div
      data-testid="feedback-banner"
      data-outcome-kind={outcome.kind}
      data-announced-count={announcedCount}
      className={`flex flex-col items-center text-center space-y-3 p-4 rounded-2xl ${feedbackConfig.entranceClass} ${className}`}
    >
      {/* Icon with clear visual boundary */}
      <div
        data-testid="feedback-icon-container"
        className={`p-3.5 rounded-full shadow-xs ${feedbackConfig.colorClasses.iconContainer}`}
      >
        <IconComponent data-testid="feedback-icon" className="size-10 stroke-[2.5]" aria-hidden="true" />
      </div>

      {/* Meaning explicitly paired with text and word */}
      <div className="flex flex-col items-center space-y-1">
        <span
          data-testid="feedback-word-badge"
          className={`font-mono text-xs font-bold uppercase tracking-wider px-3 py-0.5 rounded-full ${feedbackConfig.colorClasses.badge}`}
        >
          {feedbackConfig.word}
        </span>
        <h3
          data-testid="feedback-title"
          className="text-2xl font-bold tracking-tight text-foreground"
        >
          {title}
        </h3>
        {detail && (
          <p
            data-testid="feedback-detail"
            className="text-base text-muted-foreground max-w-md"
          >
            {detail}
          </p>
        )}
      </div>
    </div>
  );
}
