# ADR-0016 — User credential tokens stored reversibly

**Status:** Accepted · **Date:** 2026-09-15 · **Reverses choice (b) in** [docs/05](05-credentials-and-labeling.md)

## Context

In the original security design ([docs/05-credentials-and-labeling.md](05-credentials-and-labeling.md)), a fundamental distinction was drawn between device credentials and user credentials:
- **Device tokens (Choice a):** Stored reversibly using symmetric encryption (AES-GCM under a server deployment key) so that damaged equipment stickers could be reprinted with the identical barcode.
- **User tokens (Choice b):** Stored strictly as one-way keyed hashes (`HMAC-SHA256(token, pepper)`). Plaintext tokens were available only at the exact moment of issue and were never retained. Reprinting a user card was impossible; any lost or damaged card required revoking the existing credential and issuing a new token.

Phase 7 introduced two requirements that broke Choice (b):
1. **The Staff Phone PWA:** Staff must be able to view their borrower QR code on demand on their smartphone screen. If user tokens are one-way hashed, the server cannot supply the QR code payload to the staff app.
2. **Administrator on-demand badge generation:** Administrators must be able to view and print active staff QR badges without invalidating existing physical cards that staff may already possess.

## Decision

We **reverse Choice (b)** and store user credential tokens reversibly alongside their lookup hashes:

1. **Reversible encryption (`token_enc`):** Every active user credential row stores `token_enc`, encrypted using AES-256-GCM under the server secret `HDMS_CREDENTIAL_ENC_KEY`.
2. **Preserved HMAC indexing:** The `token_hash` column remains `HMAC-SHA256(token, pepper)`. Kiosk barcode scans continue to perform fast, constant-time indexed lookups against `token_hash`. Decryption is never performed on the checkout path.
3. **Admin reveal capability:** An administrator can reveal any active user credential token (`POST /v1/credentials/{id}/reveal`) to preview or print a badge.
4. **Staff self-service reveal:** An authenticated staff member can retrieve their own active credential token (`GET /v1/staff/me/credential`) to display on their device.

## Consequences

**The Cost (Security Impact)**
- **Expanded blast radius of key compromise:** Previously, compromising the database and the server filesystem revealed only device tokens; user bearer tokens could not be recovered. Under ADR-0016, compromising both the database and `HDMS_CREDENTIAL_ENC_KEY` allows an attacker to reconstruct every active staff borrower barcode in the hospital.

**Compensating Controls**
To mitigate this risk, three strict controls are enforced:
1. **No bulk reveal endpoint:** There is no API to export or reveal tokens in bulk. Tokens must be queried individually by authorized actors.
2. **Mandatory audit logging:** Every invocation of the admin reveal endpoint writes an immutable row to `audit_events` with the actor, the credential's subject, the credential id, the request id and the timestamp, and the credential's own event history gains a `revealed` entry.
3. **Retained revoke-and-issue workflow:** Physical security is not relaxed. If a staff member physically loses a printed card, administrators do not "reprint" the existing token. They revoke the lost credential and issue a fresh one, immediately blacklisting the lost barcode at all kiosks.

## Alternatives

- **Ephemeral TOTP or dynamic QR on phone screens:** Generate time-based rotating tokens on the phone. Rejected: the physical kiosks use fast 2D hardware barcode scanners running in keyboard wedge mode without real-time online validation against mobile authenticator seeds. Furthermore, physical printed badges must remain permanent, so dual-mode issuance would fragment custody verification.
- **Store credentials only on the phone client:** Store the token in local storage on the staff device during onboarding and never in the backend. Rejected: a staff member logging in from a replacement phone or clearing browser cache would lose their QR code and require administrative intervention.

## What would make us revisit this

If the hospital deploys the RFID/NFC staff badge integration (Phase 6.1). Physical RFID card UIDs are read directly from existing employee badges and cannot be minted or decrypted by HDMS, rendering reversible QR token storage obsolete for physical cards.
