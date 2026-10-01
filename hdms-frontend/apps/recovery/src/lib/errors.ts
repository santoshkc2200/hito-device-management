import type { Translate } from "@/i18n";
import type { RecoveryError } from "./api";

/** The message for an error no screen has a better word for. */
export function commonError(t: Translate, err: RecoveryError): string {
  return t(err.status === 0 ? "common.unreachable" : "common.unexpected");
}
