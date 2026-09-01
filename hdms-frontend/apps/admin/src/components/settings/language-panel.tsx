import { toast } from "sonner";
import { LOCALES, useLocale, type Locale } from "@hdms/i18n";
import { useT } from "@/i18n";

const LABELS: Record<Locale, string> = { ja: "日本語", en: "English" };

export interface LanguagePanelProps {
  updateMyLocale: (body: { locale: Locale }) => Promise<unknown>; // i18n-allow-literal: TS return type, not JSX text
}

export function LanguagePanel({ updateMyLocale }: LanguagePanelProps) {
  const t = useT();
  const { locale, setLocale } = useLocale();

  const choose = async (next: Locale) => {
    const previous = locale;
    setLocale(next); // optimistic: the console switches under the click
    try {
      await updateMyLocale({ locale: next });
    } catch {
      setLocale(previous);
      toast.error(t("settings.language.saveFailed"));
    }
  };

  return (
    <fieldset>
      <legend>{t("settings.language.heading")}</legend>
      {LOCALES.map((option) => (
        <label key={option}>
          <input
            type="radio"
            name="locale"
            value={option}
            lang={option}
            checked={locale === option}
            onChange={() => void choose(option)}
          />
          {LABELS[option]}
        </label>
      ))}
    </fieldset>
  );
}
