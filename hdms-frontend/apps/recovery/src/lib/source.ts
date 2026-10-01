import type { Translate } from "@/i18n";
import type { Source } from "./api";

export function sourceTitle(t: Translate, s: Source): string {
  if (s.kind === "local") return t("sources.local");
  if (s.kind === "destination" && s.name) return s.name;
  return t("sources.folder");
}

export function sourceDetail(t: Translate, s: Source): string {
  return s.kind === "local" ? t("sources.localHint") : s.folder;
}
