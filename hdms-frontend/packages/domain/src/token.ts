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
export const CROCKFORD_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
export const ALPHABET = CROCKFORD_ALPHABET;

/** Adds the five check-only symbols so the check digit spans mod 37. */
export const CHECK_ALPHABET = ALPHABET + "*~$=U";

export const PAYLOAD_LENGTH = 10;


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

/** Mirrors Go's maxForeignLength: bounds a foreign credential value. */
const MAX_FOREIGN_LENGTH = 64;

/**
 * Canonicalises a scan for submission: an HDMS token (valid structure and
 * checksum) comes back in canonical form, and a foreign credential value
 * (an employee-ID barcode, a card UID) of 1 to 64 printable ASCII
 * characters comes back trimmed. A string in the HDMS namespace that fails
 * parseToken is a damaged HDMS token, not a foreign credential, so it
 * throws. Twin of Go's tokens.Scannable.
 */
export function canonicalScan(raw: string): string {
  const s = raw.trim();
  if (s.toUpperCase().startsWith(`${NAMESPACE}-`)) {
    return formatToken(parseToken(s));
  }
  if (s.length === 0 || s.length > MAX_FOREIGN_LENGTH || !/^[\x21-\x7e]+$/.test(s)) {
    throw new TokenParseError(
      "invalid-format",
      "not a token or a credential value",
    );
  }
  return s;
}

export interface TokenInspection {
  isValid: boolean;
  token?: Token;
  reason?: ParseErrorReason;
  errorMessage?: string;
}

/**
 * Inspects a token input and returns structural / checksum validation details
 * with friendly localized guidance suitable for attendant keypads.
 */
export function inspectToken(raw: string): TokenInspection {
  try {
    const token = parseToken(raw);
    return { isValid: true, token };
  } catch (err) {
    if (err instanceof TokenParseError) {
      let errorMessage = err.message;
      if (err.reason === "invalid-checksum") {
        errorMessage = "That code doesn't look right — check the last character.";
      } else if (err.reason === "invalid-payload") {
        errorMessage = "Token payload must be 10 characters from the Crockford Base32 alphabet.";
      } else if (err.reason === "invalid-format") {
        errorMessage = "Format must be HD-[U|D]-[10 characters]-[check digit].";
      } else if (err.reason === "invalid-hint") {
        errorMessage = "Invalid subject hint: must be U (user) or D (device).";
      } else if (err.reason === "invalid-namespace") {
        errorMessage = "Invalid prefix: must start with HD-.";
      }
      return {
        isValid: false,
        reason: err.reason,
        errorMessage,
      };
    }
    return {
      isValid: false,
      reason: "invalid-format",
      errorMessage: "Invalid token structure.",
    };
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
