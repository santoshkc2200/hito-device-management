import { useTranslator as useI18nTranslator, type Catalogues } from "@hdms/i18n";
import { en, type StaffCatalogue } from "./en";
import { ja } from "./ja";

export const catalogue: Catalogues<StaffCatalogue> = {
  ja,
  en,
};
export const catalogues = catalogue;

export function useTranslator() {
  return useI18nTranslator(catalogue);
}

export const useT = useTranslator;

export { en, ja };
export type { StaffCatalogue };
