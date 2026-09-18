// Package jobs holds the 5.5e scheduled jobs behind `hdms-cli reconcile`
// and `hdms-cli retention` (docs/phases/phase-5/5.5-observability.md).
//
// Conventions, shared with the 5.4a backup job (docs/phases/phase-5/README.md):
// scheduled work is a `hdms-cli` subcommand run by a systemd timer, never a
// goroutine inside the API; every run is idempotent and safe to run by hand;
// every run writes a `job_runs` row reporting success positively, so alerts
// fire on the *absence* of a recent success, never on "no error was seen".
//
//   - reconcile asserts INV-3 (devices.status = 'on_loan' iff an open,
//     non-disputed loan exists). It is SELECT-only plus its job_runs row:
//     it reports custody disagreements, it never mutates custody.
//   - retention enforces the docs/09-security-privacy-ops.md policy (closed
//     loans anonymised at 3 years, archived users anonymised with the
//     history that references them, scan_events deleted at 90 days). It
//     runs report-only until Q7 is confirmed: the mode is explicit
//     configuration (HDMS_RETENTION_MODE, "report" by default), not a
//     commented-out line.
package jobs
