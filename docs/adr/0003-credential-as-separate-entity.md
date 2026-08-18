# ADR-0003 — Credentials are separate entities from users and devices

**Status:** Accepted · **Date:** 2026-08-18

## Context

The obvious design is to store a barcode value on the user row and on the device
row. The requirement "once a barcode is lost, they can generate the same barcode
again" makes that design fail immediately.

## Decision

A `credentials` table polymorphic over subject (`user` or `device`), holding the
token hash, its kind, its status, and its issuance lineage. A subject may have
many credentials over time, and several active at once.

## Consequences

**Good**
- **A lost card is reissued without touching identity or history.** Revoke the
  old credential, mint a new one, link them with `replaces_id`. Loans hang off
  `user_id`, so nothing is lost.
- **A found old card is dead**, and scanning it produces an explicit, logged
  rejection rather than a silent failure — a real security signal.
- **Multiple credential kinds per subject** (QR card + RFID ID card) is a row,
  not a schema change. This is what makes the Phase 6 adoption of the hospital's
  existing RFID/NFC staff cards additive rather than a migration: issue the RFID
  credential alongside the QR one, both work, retire the QR card when convenient.
  Nobody is re-registered and no history moves.
- **Full issuance history** — who issued what, when, why, and what it replaced.
- **Unbound "blank card stock"** — credentials minted with no subject, printed in
  advance, bound by an administrator when they register a borrower. No printer
  needed at the desk, and the token is auditable from the moment it was printed.

**Bad**
- One extra join, and a polymorphic reference without a foreign key.
- Slightly more work to reason about "which credential is current".

## Alternatives

- **Barcode column on the entity** — rejected. Reissue would require either
  destroying history or overwriting the value, and multiple credential kinds
  would be impossible.
- **Barcode encodes the entity's UUID** — rejected. The identifier becomes the
  secret, so a lost card can never be revoked without changing the entity's
  primary key.
