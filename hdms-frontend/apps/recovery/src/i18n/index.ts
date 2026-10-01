import { useTranslator, type Catalogues, type LeafKey } from "@hdms/i18n";
import { en, type RecoveryCatalogue } from "./en";
import { ja } from "./ja";

export const catalogues: Catalogues<RecoveryCatalogue> = { ja, en };

export type RecoveryKey = LeafKey<RecoveryCatalogue>;

export function useT() {
  return useTranslator(catalogues);
}

export type Translate = ReturnType<typeof useT>;

export { en, ja };
export type { RecoveryCatalogue };
