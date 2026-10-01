import * as React from "react";
import { useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";

export function Page({ children }: { children: React.ReactNode }) {
  const t = useT();
  const { locale, setLocale } = useLocale();
  return (
    <div className="min-h-dvh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-2xl items-center justify-between gap-4 px-4 py-4">
          <div>
            <h1 className="text-xl font-bold">{t("app.title")}</h1>
            <p className="text-sm text-muted-foreground">{t("app.subtitle")}</p>
          </div>
          <Button
            variant="outline"
            data-testid="language-toggle"
            aria-label={t("app.languageAria")}
            onClick={() => setLocale(locale === "ja" ? "en" : "ja")}
          >
            {t("app.language")}
          </Button>
        </div>
      </header>
      <main className="mx-auto max-w-2xl px-4 py-8">{children}</main>
    </div>
  );
}
