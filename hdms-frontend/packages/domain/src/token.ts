/**
 * The TypeScript twin of hdms-backend/internal/platform/tokens: parses and
 * validates the HDMS credential token format from
 * docs/05-credentials-and-labeling.md.
 *
 *   HD-U-7K3M9QXA2F-4
 *   │  │ │            check character (Crockford Base32 mod-37)
 *   │  │ 10-char random payload, Crockford Base32
 *   │  subject hint: U = user, D = device
 *   namespace
 *
 * Only `parse`/`validate` exist here — tokens are always minted server-side
 * (see Go's Generate), never by the kiosk. The kiosk uses this module to
 * reject garbage scans locally, before a network round trip.
 *
 * A single golden fixture (fixtures/token-fixtures.json, at the repo root)
 * is checked by both implementations so they cannot drift; see token.test.ts.
 */

export const NAMESPACE = "HD";

/** Crockford Base32: excludes I, L, O, U to avoid look-alike confusion. */
const ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** Adds the five check-only symbols so the check digit spans mod 37. */
const CHECK_ALPHABET = ALPHABET + "*~$=U";

const PAYLOAD_LENGTH = 10;

export type SubjectHint = "U" | "D";

export interface Token {
  readonly hint: SubjectHint;
  readonly payload: string;
  readonly check: string;
}

export type ParseErrorReason =
  | "invalid-format"
  | "invalid-namespace"
  | "invalid-hint"
  | "invalid-payload"
  | "invalid-checksum";

export class TokenParseError extends Error {
  readonly reason: ParseErrorReason;

  constructor(reason: ParseErrorReason, message: string) {
    super(message);
    this.name = "TokenParseError";
    this.reason = reason;
  }
}

/** Renders a Token back to its canonical, uppercase, hyphenated form. */
export function formatToken(token: Token): string {
  return `${NAMESPACE}-${token.hint}-${token.payload}-${token.check}`;
}

/**
 * Parses and validates a scanned or typed string against the token grammar
 * and mod-37 checksum, tolerating whitespace, lowercase input, and
 * Crockford's I/L→1, O→0 substitution for easily-misread characters.
 *
 * Throws TokenParseError on any failure; the `reason` field names the
 * specific field that failed, mirroring the Go package's sentinel errors.
 */
export function parseToken(raw: string): Token {
  const s = normalize(raw);
  const parts = s.split("-");
  if (parts.length !== 4) {
    throw new TokenParseError(
      "invalid-format",
      "token does not have namespace-hint-payload-check structure",
    );
  }
  const [namespace, hintPart, payload, checkPart] = parts as [
    string,
    string,
    string,
    string,
  ];

  if (namespace !== NAMESPACE) {
    throw new TokenParseError("invalid-namespace", "unrecognised namespace");
  }

  if (hintPart.length !== 1 || (hintPart !== "U" && hintPart !== "D")) {
    throw new TokenParseError("invalid-hint", "unrecognised subject hint");
  }
  const hint = hintPart as SubjectHint;

  if (payload.length !== PAYLOAD_LENGTH || !isValidPayloadCharset(payload)) {
    throw new TokenParseError(
      "invalid-payload",
      "payload is the wrong length or contains invalid characters",
    );
  }

  if (checkPart.length !== 1 || !CHECK_ALPHABET.includes(checkPart)) {
    throw new TokenParseError(
      "invalid-checksum",
      "check character does not match the payload",
    );
  }

  const want = checkCharFor(payload);
  if (checkPart !== want) {
    throw new TokenParseError(
      "invalid-checksum",
      "check character does not match the payload",
    );
  }

  return { hint, payload, check: checkPart };
}

/** Reports whether `raw` is a structurally and checksum-valid token. */
export function validateToken(raw: string): boolean {
  try {
    parseToken(raw);
    return true;
  } catch {
    return false;
  }
}

function isValidPayloadCharset(payload: string): boolean {
  for (const ch of payload) {
    if (!ALPHABET.includes(ch)) return false;
  }
  return true;
}

/**
 * Computes the Crockford mod-37 check character for a payload already
 * confirmed to contain only alphabet characters, updating the running
 * remainder one base-32 digit at a time.
 */
function checkCharFor(payload: string): string {
  let acc = 0;
  for (const ch of payload) {
    const v = ALPHABET.indexOf(ch);
    if (v < 0) {
      throw new TokenParseError(
        "invalid-payload",
        "payload contains invalid characters",
      );
    }
    acc = (acc * 32 + v) % 37;
  }
  return CHECK_ALPHABET[acc] as string;
}

/**
 * Uppercases, trims, and applies Crockford's ambiguous-character
 * substitution (I/L→1, O→0). None of the characters this produces overlap
 * the check-only symbols (*~$=U), so it never corrupts a valid check
 * character.
 */
function normalize(raw: string): string {
  let s = raw.trim().toUpperCase();
  s = s.replace(/[IL]/g, "1").replace(/O/g, "0");
  return s;
}
