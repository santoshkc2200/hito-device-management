import { useLocale, type Locale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";

const LABELS: Record<Locale, string> = { ja: "日本語", en: "English" };

/**
 * Reachable without being easy to hit by accident: it sits in the frame's
 * chrome rather than the content area, and it is a single toggle, so a
 * mis-tap is undone by a second tap rather than opening a menu over the
 * screen someone is trying to read.
 */
export function LanguageToggle() {
  const { locale, setLocale } = useLocale();
  const next: Locale = locale === "ja" ? "en" : "ja";

  return (
    <Button
      variant="ghost"
      size="sm"
      className="min-h-12 min-w-12 text-sm font-semibold"
      lang={next}
      aria-label={`Switch to ${LABELS[next]}`}
      onClick={() => setLocale(next)}
      data-testid="language-toggle"
    >
      {LABELS[next]}
    </Button>
  );
}
