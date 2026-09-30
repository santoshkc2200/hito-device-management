import { formatDate, formatTime, translate, type Locale } from "@hdms/i18n";
import type { SessionMessage } from "@hdms/api-client";
import { catalogue, type KioskCatalogue } from "@/i18n";

/**
 * The server keys the kiosk can word itself. A scan response carries its
 * message as a code plus display-safe args (the backend rule in
 * docs/superpowers/specs/2026-08-31-i18n-l10n-design.md); its English
 * title/detail are only a fallback, and showing them would put English on
 * an otherwise Japanese screen.
 */
type SessionMessageKey = Exclude<keyof KioskCatalogue["sessionMessage"], "fallback" | "deviceStatus">;

function isSessionMessageKey(key: string | undefined): key is SessionMessageKey {
  return (
    !!key &&
    key !== "fallback" &&
    key !== "deviceStatus" &&
    Object.prototype.hasOwnProperty.call(catalogue.en.sessionMessage, key)
  );
}

function formatStamp(locale: Locale, iso: string | undefined): string | null {
  if (!iso) return null;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  return `${formatDate(locale, date)} ${formatTime(locale, date)}`;
}

export interface RenderedSessionMessage {
  title: string;
  detail: string;
}

/**
 * Renders a server SessionMessage in the kiosk's active locale, or returns
 * null when the message carries no key this catalogue knows — the caller
 * then falls back to its own localized default, never the server English.
 */
export function renderSessionMessage(
  message: SessionMessage | null | undefined,
  locale: Locale
): RenderedSessionMessage | null {
  const key = message?.key;
  if (!isSessionMessageKey(key)) return null;

  // Keys are built at runtime from a server code, so they bypass LeafKey.
  const t = (path: string, params?: Record<string, string | number>) =>
    translate(catalogue, locale, path as never, params);
  const args = message?.args ?? {};
  const base = `sessionMessage.${key}`;

  const device = args.deviceName || t("sessionMessage.fallback.device");
  const holder = args.holderName || t("sessionMessage.fallback.holder");
  const status = args.deviceStatus
    ? t(`sessionMessage.deviceStatus.${args.deviceStatus}`) || t("sessionMessage.fallback.status")
    : t("sessionMessage.fallback.status");
  const borrowedAt = formatStamp(locale, args.borrowedAt);
  const reservationStartAt = formatStamp(locale, args.reservationStartAt);
  const revokedAt = formatStamp(locale, args.revokedAt);

  const params: Record<string, string> = {
    device,
    holder,
    status,
    department: args.holderDepartment ? t(`${base}.department`, { department: args.holderDepartment }) : "",
    since: borrowedAt ? t(`${base}.since`, { date: borrowedAt }) : "",
    reservedFor: args.reservedForName ? t(`${base}.reservedFor`, { name: args.reservedForName }) : "",
    from: reservationStartAt ? t(`${base}.from`, { date: reservationStartAt }) : "",
    on: revokedAt ? t(`${base}.on`, { date: revokedAt }) : "",
  };

  return {
    title: t(`${base}.title`, params),
    detail: t(`${base}.detail`, params),
  };
}
