import type { StaffDevice } from "@hdms/api-client";
import type { JSX } from "react";
import { useT } from "@/i18n";

const TONE: Record<string, string> = {
  available: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  in_use: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
};

export function AvailabilityPill(props: {
  availability: StaffDevice["availability"];
}): JSX.Element {
  const t = useT();
  const { availability } = props;
  const label =
    availability === "available"
      ? t("devices.available")
      : availability === "in_use"
        ? t("devices.inUse")
        : t("devices.unavailable");

  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
        TONE[availability] ?? "bg-muted text-muted-foreground"
      }`}
    >
      {label}
    </span>
  );
}
