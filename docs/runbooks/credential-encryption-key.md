# Runbook: Credential Encryption Key Management & Rotation

This operational runbook covers the configuration, access controls, rotation, and disaster recovery procedures for **`HDMS_CREDENTIAL_ENC_KEY`**, the server-side symmetric encryption key introduced in Phase 7 ([ADR-0016](../adr/0016-user-credential-tokens-stored-reversibly.md)) to protect encrypted credential tokens (`token_enc`).

---

## Key Overview & Specification

- **Purpose:** Encrypts and decrypts user and device credential plaintext tokens using AES-256-GCM.
- **Format:** 32-byte (256-bit) cryptographically random binary key, base64-encoded (44 characters).
- **Environment Variable:** `HDMS_CREDENTIAL_ENC_KEY`
- **Example Generation Command:**
  ```bash
  openssl rand -base64 32
  ```

---

## Where the Key Lives & Access Controls

1. **Storage Location:**
   - In production Docker Compose / VM environments: Stored in an environment file (`/etc/hdms/hdms.env` or Docker secrets volume) readable only by the `hdms` runtime user (`chmod 600`).
   - In cloud or managed container orchestrators: Injected as an encrypted environment variable from the secret manager (e.g. HashiCorp Vault, AWS Secrets Manager, or Azure Key Vault).
2. **Access Restrictions:**
   - **System Service Only:** Only the `hdms-backend` daemon process requires read access to this variable.
   - **No Database Access:** The key must never be stored inside PostgreSQL or committed to git.
   - **No Client Exposure:** The key is never transmitted to web frontends, kiosk terminals, or the staff mobile PWA.

---

## Key Rotation Procedure

Key rotation re-encrypts all existing ciphertext payloads (`token_enc` in the `credentials` table) under a new AES-256-GCM key without altering the underlying tokens or their HMAC hashes (`token_hash`).

> **Not yet implemented.** There is no rotation command in `hdms-cli` today — its
> subcommands are `seed`, `admin`, `import` and `kiosk`. The procedure below is the
> one a rotation job must follow; write it before the first rotation is needed, and
> delete this note when it ships.

### The procedure a rotation job must follow

1. **Generate the new key:**
   ```bash
   NEW_KEY=$(openssl rand -base64 32)
   ```
2. **Re-encrypt every ciphertext in one transaction**, with the old key still configured:
   - Open a database transaction.
   - Select every `credentials` row where `token_enc IS NOT NULL`, `FOR UPDATE`.
   - Decrypt each `token_enc` with the old key, verifying the GCM tag, and re-encrypt under the new key.
   - Update the rows and commit. A partial rotation must roll back — a half-rotated table is
     readable under neither key alone.
   - Hold the old key until the job has committed, so a failure is recoverable.
   `token_hash` is untouched: it is an HMAC under `HDMS_TOKEN_PEPPER`, not under this key, so
   scanning is unaffected throughout.
3. **Update the server environment:**
   ```bash
   sed -i "s|^HDMS_CREDENTIAL_ENC_KEY=.*|HDMS_CREDENTIAL_ENC_KEY=${NEW_KEY}|" /etc/hdms/hdms.env
   ```
4. **Restart the API server:**
   ```bash
   systemctl restart hdms-backend
   ```
5. **Verify:** a staff member can still see their QR in the staff PWA (`GET /v1/staff/me/credential`),
   and an administrator can still reveal a card (`POST /v1/credentials/{id}/reveal`).

---

## What Breaks If the Key Is Lost (Disaster Scenario)

If `HDMS_CREDENTIAL_ENC_KEY` is permanently lost, overwritten, or destroyed without a backup:

### What Still Works
- **Kiosk Borrowing & Returning Continues Operating:** Kiosk barcode scans resolve credentials against `token_hash` (`HMAC-SHA256(token, pepper)`). The HMAC pepper (`HDMS_TOKEN_PEPPER`) is distinct from the encryption key. Physical badges already printed will continue to scan, borrow, and return equipment without interruption.

### What Breaks
- **Staff Mobile PWA Cannot Display QR:** Any attempt by a staff member to view their digital badge (`GET /v1/staff/me/credential`) will fail with decryption errors.
- **Admin Badge Reveal & Reprinting Fails:** Administrators can no longer view or print active staff badges (`POST /v1/credentials/{id}/reveal`).

### Recovery Procedure
Because AES-GCM ciphertext cannot be recovered without the key, recovery requires a mass re-issuance:
1. Generate and configure a fresh `HDMS_CREDENTIAL_ENC_KEY`.
2. Clear the unreadable ciphertexts:
   ```sql
   UPDATE credentials SET token_enc = NULL WHERE token_enc IS NOT NULL;
   ```
3. Issue new credentials for affected staff members as they report to the counter or through an automated reissue batch.
