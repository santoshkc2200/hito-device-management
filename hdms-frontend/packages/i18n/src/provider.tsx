import * as React from "react";
import { DEFAULT_LOCALE, type Locale } from "./locale";
import { translate, type Catalogues, type LeafKey, type MessageTree } from "./translate";

interface LocaleContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
}

let defaultLocaleFallback: Locale = DEFAULT_LOCALE;

export function setDefaultLocaleFallback(locale: Locale): void {
  defaultLocaleFallback = locale;
}

const LocaleContext = React.createContext<LocaleContextValue | null>(null);

export interface LocaleProviderProps {
  /**
   * The anchor locale — the kiosk's configured default, or the signed-in
   * administrator's preference. Changing it resets any in-session selection,
   * which is how the kiosk returns to its default on the idle timeout.
   */
  locale?: Locale;
  onLocaleChange?: (locale: Locale) => void;
  children: React.ReactNode;
}

export function LocaleProvider({
  locale: anchor = DEFAULT_LOCALE,
  onLocaleChange,
  children,
}: LocaleProviderProps) {
  const [locale, setLocaleState] = React.useState<Locale>(anchor);

  // Re-anchoring on prop change is the reset: the kiosk raises its configured
  // default when a session ends, and whatever the last person selected is gone.
  React.useEffect(() => {
    setLocaleState(anchor);
  }, [anchor]);

  React.useEffect(() => {
    if (typeof document !== "undefined") {
      document.documentElement.lang = locale;
    }
  }, [locale]);

  const setLocale = React.useCallback(
    (next: Locale) => {
      setLocaleState(next);
      onLocaleChange?.(next);
    },
    [onLocaleChange]
  );

  const value = React.useMemo(() => ({ locale, setLocale }), [locale, setLocale]);

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale(): LocaleContextValue {
  const ctx = React.useContext(LocaleContext);
  if (ctx) return ctx;
  return {
    locale: defaultLocaleFallback,
    setLocale: () => {},
  };
}

export function useTranslator<M extends MessageTree>(catalogues: Catalogues<M>) {
  const { locale } = useLocale();
  return React.useCallback(
    (key: LeafKey<M>, params?: Record<string, string | number>) =>
      translate(catalogues, locale, key, params),
    [catalogues, locale]
  );
}
