# ADR-0017 — Backup format delegated to restic

**Status:** Accepted · **Date:** 2026-09-19 · **Related to** [ADR-0001](0001-modular-monolith-go.md), [ADR-0009](0009-staff-authentication-realm.md)

## Context

Phase 5.4a implemented database backups by writing a single encrypted file per run: `pg_dump -Fc` piped through `gzip` and encrypted with AES-256-GCM using `HDMS_BACKUP_ENC_KEY`, stored in a fixed local directory (`HDMS_BACKUP_DIR`) with fixed retention (30 daily and 12 monthly).

While this satisfied initial local disaster recovery requirements, operational experience and hospital requirements identified critical limitations:
1. **Configurable destinations:** The hospital requires replicating backups to offsite storage — including network-attached storage (LAN NAS shares) and institutional cloud storage (Google Drive, Microsoft OneDrive) — rather than keeping data only on the host disk.
2. **Bandwidth and storage efficiency:** Transferring a full compressed dump across the LAN or WAN every night creates excessive bandwidth usage and quickly exhausts remote quotas. The hospital needs deduplicated, incremental transfers where subsequent runs after small changes upload only the delta.
3. **Flexible retention:** Remote destinations have constrained storage quotas and require version-count retention (e.g. keeping the newest K snapshots, defaulting to K=2), while the local host retains long-term daily/monthly archives.
4. **Configurable schedules:** Backup timing and intervals need to be configurable per deployment rather than hardcoded to a single nightly 02:00 window.

## Decision

We delegate backup repository formatting, content-defined chunking deduplication, encryption at rest, retention policy evaluation, pruning, repository verification, and raw restore streaming to **restic** (version 0.14+, repository format version 2). We delegate cloud transport to **rclone** via restic's native rclone backend (`rclone:remote:path`).

HDMS retains ownership of schedule management, destination configuration, pipeline fan-out, process orchestration, operational observability, and the administrative console UI:

1. **Local repository as source of truth:** The primary repository lives at `${HDMS_BACKUP_DIR}/repo`. Each backup streams `pg_dump -Fc -Z0` (uncompressed custom format) directly into restic via standard input (`restic backup --stdin --stdin-filename hdms.dump`). Compression is handled internally by restic (repository format 2), ensuring that pre-compression does not destroy chunk-level deduplication.
2. **Fan-out via repository copying:** Offsite destinations are independent restic repositories. Remote repositories are initialized with `restic init --copy-chunker-params --from-repo <local>`, ensuring shared polynomial chunker parameters across repositories. A backup run dumps once to the local repository and then copies newly added snapshots to each enabled remote repository via `restic copy --from-repo <local> -r <dest>`.
3. **Independent retention:** The local repository enforces fixed long-term retention (30 daily, 12 monthly) via `restic forget --keep-daily 30 --keep-monthly 12 --prune`. Remote repositories enforce per-destination version-count retention via `restic forget --keep-last K --prune`.
4. **Subprocess execution and secret handling:** All restic and rclone interactions run as subprocesses using `exec.CommandContext` with explicit argument slices — never through a shell. The repository password is derived from `HDMS_BACKUP_ENC_KEY` (base64-encoded) and injected strictly through the subprocess environment as `RESTIC_PASSWORD`. It never appears in process arguments (`argv`), preventing exposure via `/proc`.
5. **Path validation:** Path destinations configured in the administrative console must resolve under allowlisted roots specified in `HDMS_BACKUP_ALLOWED_ROOTS`. Symlink traversal and parent directory escapes (`..`) outside allowed roots are rejected fail-closed.
6. **Observability and degradation:** Each run records its status in `job_runs` with one of three outcomes: `success`, `degraded`, or `failure`. If the local snapshot succeeds but any offsite destination fails, the run completes as `degraded` and logs the error in the `job_runs` detail JSON. A degraded run never updates the `hdms_backup_last_success_timestamp_seconds` Prometheus metric, ensuring alerting triggers if offsite synchronization fails.
7. **Legacy backward compatibility:** Legacy single-file backups (`hdms-*.dump.gz.enc`) remain in `${HDMS_BACKUP_DIR}`. They are never moved or modified by restic, age out naturally under their original 30-daily + 12-monthly rule, and remain restorable via `hdms-cli restore --snapshot <path>`.

## Consequences

**Good**
- **Dramatic bandwidth and storage savings:** Content-defined chunking (CDC) deduplication means a nightly run following minimal daily data changes transfers only a tiny fraction of the total database size to remote storage.
- **Audited cryptography:** Backup encryption uses restic's repository format 2 (AES-256 or ChaCha20-Poly1305 with authenticated encryption, key derivation via scrypt/Argon2id, and SHA-256 chunk hashing).
- **Zero cloud credentials in HDMS database:** Cloud OAuth tokens and provider API keys are managed by hospital IT using host-level `rclone config` (`sudo -u hdms rclone config`), completely isolated from the HDMS application database and memory.
- **Vendor-independent recovery:** Snapshots can be restored during a disaster recovery scenario using standard, publicly documented `restic` CLI commands without requiring HDMS application binaries or source code.
- **Safe and isolated pruning:** Retention pruning is executed natively by restic per destination repository with cryptographic blob integrity verification (`restic check`).

**Bad**
- **Host dependencies:** The host environment requires two additional binaries installed: `restic` (version floor 0.14+) and `rclone` (when cloud destinations are enabled).
- **Single master key:** `HDMS_BACKUP_ENC_KEY` serves as the restic repository password. Losing this key makes all local and remote snapshots permanently unrecoverable.
- **Unkeyed chunk identifiers:** Blob identifiers in restic are unkeyed SHA-256 hashes inside an encrypted index rather than an application-keyed HMAC. While protected by repository envelope encryption, chunk identities are structural rather than domain-salted.
- **Dual restore paths:** `hdms-cli restore` must maintain branching logic to handle both legacy AES-GCM single files and restic snapshots until all pre-cutover legacy archives age out.

## Alternatives

- **Implement chunking and object storage in-house:** Rejected. Writing a custom content-defined chunking store, deduplication index, and reference-counted garbage collector across multiple remote backends represents high-consequence storage engineering. Hand-rolled implementations carry severe risks of data loss, snapshot corruption, and lack adversarial security audits.
- **Physical `pg_basebackup --incremental` with WAL archiving:** Rejected for now. While physical basebackups with WAL streaming provide near-zero Recovery Point Objectives (RPO), the primary hospital requirement here is minimizing offsite upload bandwidth and providing snapshot-level storage on diverse LAN/cloud endpoints. Full physical archiving requires specialized infrastructure and remains planned under 5.4d.
- **Native cloud SDKs and OAuth in HDMS:** Rejected. Embedding native Google Drive and OneDrive SDKs into HDMS would require managing OAuth redirect flows, client credentials, token refreshing, and per-provider SDK maintenance inside the backend, increasing attack surface with no benefit over `rclone`.

## What would make us revisit this

If the hospital migrates to managed cloud database infrastructure (e.g. AWS RDS, Azure Database for PostgreSQL, or Google Cloud SQL) with automated point-in-time recovery (PITR) and cross-region replication built into the cloud fabric, or if database size exceeds petabyte scale requiring dedicated block-level distributed storage engines.
