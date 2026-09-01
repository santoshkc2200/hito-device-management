import { useTranslator as useI18nTranslator, type Catalogues } from "@hdms/i18n";
import { en, type KioskCatalogue } from "./en";
import { ja } from "./ja";

export const catalogue: Catalogues<KioskCatalogue> = {
  ja,
  en,
};

export function useTranslator() {
  return useI18nTranslator(catalogue);
}

export { en, ja };
export type { KioskCatalogue };
