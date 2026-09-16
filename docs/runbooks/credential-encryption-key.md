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

Rotation is performed while the server is active using the internal CLI utility or migration script:

### Step-by-Step Rotation

1. **Generate the New Key:**
   ```bash
   NEW_KEY=$(openssl rand -base64 32)
   ```
2. **Execute Re-encryption in a Single Transaction:**
   Run the CLI rotation job passing both the current active key and the proposed new key:
   ```bash
   hdms-cli credentials rotate-encryption-key \
     --old-key="${HDMS_CREDENTIAL_ENC_KEY}" \
     --new-key="${NEW_KEY}"
   ```
   *Execution Details:*
   - Opens a database transaction.
   - Selects every `credentials` row where `token_enc IS NOT NULL` using `FOR UPDATE`.
   - Decrypts each `token_enc` with the old key, verifies the GCM tag, and re-encrypts it with the new key.
   - Updates the rows and commits the transaction.
3. **Update Server Environment:**
   Update `HDMS_CREDENTIAL_ENC_KEY` in the environment configuration file:
   ```bash
   sed -i "s|^HDMS_CREDENTIAL_ENC_KEY=.*|HDMS_CREDENTIAL_ENC_KEY=${NEW_KEY}|" /etc/hdms/hdms.env
   ```
4. **Restart the API Server:**
   ```bash
   systemctl restart hdms-backend
   ```
5. **Verify Rotation:**
   Verify that a staff user can view their QR code in the staff PWA and that the admin reveal endpoint functions without error:
   ```bash
   curl -s -H "Cookie: hdms_session=..." https://localhost:8443/v1/users/{id}/credentials/{credId}/reveal
   ```

---

## What Breaks If the Key Is Lost (Disaster Scenario)

If `HDMS_CREDENTIAL_ENC_KEY` is permanently lost, overwritten, or destroyed without a backup:

### What Still Works
- **Kiosk Borrowing & Returning Continues Operating:** Kiosk barcode scans resolve credentials against `token_hash` (`HMAC-SHA256(token, pepper)`). The HMAC pepper (`HDMS_TOKEN_PEPPER`) is distinct from the encryption key. Physical badges already printed will continue to scan, borrow, and return equipment without interruption.

### What Breaks
- **Staff Mobile PWA Cannot Display QR:** Any attempt by a staff member to view their digital badge (`GET /v1/staff/me/credential`) will fail with decryption errors.
- **Admin Badge Reveal & Reprinting Fails:** Administrators can no longer view or print active staff badges (`POST /v1/users/{id}/credentials/{credId}/reveal`).

### Recovery Procedure
Because AES-GCM ciphertext cannot be recovered without the key, recovery requires a mass re-issuance:
1. Generate and configure a fresh `HDMS_CREDENTIAL_ENC_KEY`.
2. Clear the unreadable ciphertexts:
   ```sql
   UPDATE credentials SET token_enc = NULL WHERE token_enc IS NOT NULL;
   ```
3. Issue new credentials for affected staff members as they report to the counter or through an automated reissue batch.
