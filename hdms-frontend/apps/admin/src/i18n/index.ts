import { useTranslator, type Catalogues } from "@hdms/i18n";
import { ja, type AdminMessages } from "./ja";
import { en } from "./en";

export const catalogues: Catalogues<AdminMessages> = { ja, en };

export type { AdminMessages };

export function useT() {
  return useTranslator(catalogues);
}
