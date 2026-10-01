// Command hdms-cli will hold import, seed, admin bootstrap and label export
// subcommands (docs/02-architecture.md). Phase 0 wired the entrypoint and
// the `seed` subcommand the local dev stack depends on; Phase 1 adds
// `admin bootstrap`, the only way an admin_accounts row is ever created
// (there is no self-service enrollment, matching every other
// administrator-only creation path in this system).
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/hito-hospital/hdms/internal/cliimport"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/notification"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/i18n"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/internal/platform/seed"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"golang.org/x/term"
	"golang.org/x/text/language"
)

func main() {
	localeFlag, filteredArgs := extractLocaleFlag(os.Args)
	cat := catalogueForFlag(localeFlag)

	if len(filteredArgs) < 2 {
		usage()
		os.Exit(2)
	}

	if err := run(filteredArgs[1], filteredArgs[2:], cat); err != nil {
		fmt.Fprintln(os.Stderr, "hdms-cli:", err)
		os.Exit(1)
	}
}

// extractLocaleFlag scans os.Args for a global --locale flag that overrides
// LANG. It supports both --locale ja and --locale=ja forms and strips the
// flag from the returned args so subcommand FlagSets do not see it.
func extractLocaleFlag(args []string) (string, []string) {
	var locale string
	remaining := []string{args[0]}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--locale" && i+1 < len(args) {
			locale = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--locale=") {
			locale = strings.TrimPrefix(arg, "--locale=")
			continue
		}
		remaining = append(remaining, arg)
	}
	return locale, remaining
}

// catalogueForFlag returns a catalogue for the --locale flag value when
// present, otherwise falls back to LANG via i18n.FromEnv. The flag overrides
// LANG, matching the spec's CLI locale resolution.
func catalogueForFlag(flagVal string) *i18n.Catalogue {
	if flagVal != "" {
		clean := strings.SplitN(flagVal, ".", 2)[0]
		matcher := language.NewMatcher([]language.Tag{language.Japanese, language.English})
		tag, _ := language.MatchStrings(matcher, clean)
		return i18n.New(tag)
	}
	return i18n.FromEnv()
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hdms-cli [--locale ja|en] <seed|migrate|worker|backup|snapshots|verify|restore|recovery unwrap|reconcile|retention|overdue-scan|weekly-digest|directory-sync|reservation-expiry|admin bootstrap|admin set-password|admin unlock|import devices|import users|export machine|export scenarios|kiosk register|kiosk rotate|kiosk pairing-code>")
}

func run(cmd string, args []string, cat *i18n.Catalogue) error {
	if cmd == "export" {
		return runExport(args, cat)
	}
	if cmd == "recovery" {
		return runRecovery(args, os.Stdin, os.Stdout)
	}

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch cmd {
	case "migrate":
		return db.Migrate(ctx, cfg.DatabaseURL)
	case "seed":
		return runSeed(ctx, cfg, args)
	case "backup":
		return runBackup(ctx, cfg, args)
	case "snapshots":
		return runSnapshots(ctx, cfg, args)
	case "verify":
		return runVerify(ctx, cfg, args)
	case "restore":
		return runRestore(ctx, cfg, args)
	case "reconcile":
		return runReconcile(ctx, cfg, args)
	case "retention":
		return runRetention(ctx, cfg, args)
	case "overdue-scan":
		return runOverdueScan(ctx, cfg, args)
	case "weekly-digest":
		return runWeeklyDigest(ctx, cfg, args)
	case "directory-sync":
		return runDirectorySync(ctx, cfg, args)
	case "reservation-expiry":
		return runReservationExpiry(ctx, cfg, args)
	case "worker":
		return runWorker(ctx, cfg, args)
	case "admin":
		return runAdmin(ctx, cfg, args, cat)
	case "import":
		return runImport(ctx, cfg, args, cat)
	case "kiosk":
		return runKiosk(ctx, cfg, args, cat)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func runSeed(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	scale := fs.Bool("scale", false, "populate pilot-scale synthetic data (~800 users, ~500 devices, 5 000 loans)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	isScale := *scale
	for _, a := range fs.Args() {
		if a == "scale" || a == "--scale" {
			isScale = true
		}
	}

	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if isScale {
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer pool.Close()
		fmt.Println("Seeding synthetic pilot-scale data (~800 users, ~500 devices, 5 000 loans)...")
		if err := seed.SeedRealisticScaleDataset(ctx, pool); err != nil {
			return fmt.Errorf("seed scale data: %w", err)
		}
		fmt.Println("Seeding completed successfully.")
	}
	return nil
}

// resticFor builds the restic client. The repository password is
// HDMS_BACKUP_ENC_KEY, base64-encoded so it is a printable password rather
// than raw bytes, and it travels in the subprocess environment — never argv,
// which is world-readable via /proc.
func resticFor(cfg config.Config) (backup.Restic, error) {
	if len(cfg.BackupEncKey) != 32 {
		return backup.Restic{}, fmt.Errorf("HDMS_BACKUP_ENC_KEY: missing or invalid backup encryption key; provide a base64-encoded 32-byte key generated with 'openssl rand -base64 32' (stored separately from the backups, e.g. hospital password manager + root-only env file)")
	}
	var extraEnv []string
	if cfg.RcloneConfig != "" {
		extraEnv = append(extraEnv, "RCLONE_CONFIG="+cfg.RcloneConfig)
	}
	return backup.Restic{
		Binary:   cfg.ResticBinary,
		Password: base64.StdEncoding.EncodeToString(cfg.BackupEncKey),
		ExtraEnv: extraEnv,
	}, nil
}

// runBackup performs one backup: pg_dump -Fc -Z0 into the local restic
// repository, a copy into every enabled destination, then retention per
// repository. Destinations come from backup_destinations; the local copy is
// always written. See docs/runbooks/nightly-backup.md.
func runBackup(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", "backup directory (default HDMS_BACKUP_DIR)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		dir = cfg.BackupDir
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	dests, err := backup.LoadEnabledDestinations(ctx, pool)
	if err != nil {
		return err
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool:         pool,
		DatabaseURL:  cfg.DatabaseURL,
		BackupDir:    dir,
		AllowedRoots: cfg.BackupAllowedRoots,
		Restic:       r,
		Destinations: dests,
		MetricsDir:   cfg.JobMetricsDir,
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Printf("Backup %s: snapshot %s, %d byte(s) added, %d destination(s)\n",
		rep.Outcome, rep.Snapshot, rep.AddedBytes, len(rep.Destinations))
	for _, d := range rep.Destinations {
		if d.Outcome != backup.OutcomeSuccess {
			fmt.Printf("  %s: %s — %s\n", d.Name, d.Outcome, d.Error)
		}
	}
	// A degraded run reached the local disk but not every offsite copy. Exit
	// non-zero so a timer or an operator sees it as a problem.
	if rep.Outcome != backup.OutcomeSuccess {
		return fmt.Errorf("backup completed %s; see job_runs for per-destination detail", rep.Outcome)
	}
	return nil
}

// repoFor resolves --from to a repository: empty means the local host
// repository, otherwise the named destination row.
func repoFor(ctx context.Context, cfg config.Config, pool *db.Pool, name string) (backup.Repo, error) {
	if strings.TrimSpace(name) == "" {
		return backup.LocalRepo(cfg.BackupDir), nil
	}
	dests, err := backup.LoadAllDestinations(ctx, pool)
	if err != nil {
		return backup.Repo{}, err
	}
	for _, d := range dests {
		if strings.EqualFold(d.Name, name) {
			return d.Resolve(cfg.BackupAllowedRoots)
		}
	}
	return backup.Repo{}, fmt.Errorf("no backup destination named %q", name)
}

func runSnapshots(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("snapshots", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	snaps, err := r.Snapshots(ctx, repo)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		fmt.Printf("%s  %s\n", s.ID, s.Time.UTC().Format(time.RFC3339))
	}
	return nil
}

func runVerify(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	subset := fs.Int("read-data-subset", 5, "percentage of data blobs to read (0 = metadata only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	if err := r.Check(ctx, repo, *subset); err != nil {
		return err
	}
	fmt.Printf("Repository %s verified (%d%% of data read)\n", repo.Location, *subset)
	return nil
}

// runRestore restores one snapshot into a database. It refuses to overwrite
// the live database without --force: a restore drill targets a scratch
// database, and a tool that makes overwriting production the easy path will
// eventually do it.
func runRestore(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	snapshot := fs.String("snapshot", "latest", "restic snapshot id, 'latest', or a legacy hdms-*.dump.gz.enc path")
	into := fs.String("into", "", "target database URL (required)")
	force := fs.Bool("force", false, "allow restoring over the live database named by HDMS_DATABASE_URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*into) == "" {
		return fmt.Errorf("usage: hdms-cli restore --into <database-url> [--from <destination>] [--snapshot <id>]")
	}
	if *into == cfg.DatabaseURL && !*force {
		return fmt.Errorf("refusing to restore over the live database; pass --force if that is genuinely intended")
	}
	if len(cfg.BackupEncKey) != 32 {
		return fmt.Errorf("HDMS_BACKUP_ENC_KEY: missing or invalid backup encryption key")
	}

	if backup.IsLegacyBackupFile(*snapshot) {
		if err := backup.RestoreLegacyFile(ctx, *snapshot, cfg.BackupEncKey, *into); err != nil {
			return err
		}
		fmt.Printf("Restored legacy backup %s into %s\n", *snapshot, redactURL(*into))
		return nil
	}

	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	if err := backup.RestoreInto(ctx, r, repo, *snapshot, *into); err != nil {
		return err
	}
	fmt.Printf("Restored snapshot %s from %s into %s\n", *snapshot, repo.Location, redactURL(*into))
	return nil
}

func redactURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[malformed database url]"
	}
	return u.Redacted()
}

// runReconcile implements 5.5e: nightly INV-3 assertion, report-only by
// design — it names disagreeing devices and exits non-zero, it never
// mutates custody. Invoked nightly by the systemd timer in
// deploy/systemd/hdms-reconcile.timer; see docs/runbooks/reconciliation.md.
func runReconcile(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli reconcile (no arguments)")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	report, err := jobs.Run(ctx, pool.Pool, time.Now().UTC(), cfg.JobMetricsDir)
	if err != nil {
		return err
	}
	fmt.Printf("Reconcile: %d device(s) checked, 0 mismatches\n", report.DevicesChecked)
	return nil
}

// runRetention implements 5.5e: the docs/09 retention policy. The mode is
// explicit configuration — HDMS_RETENTION_MODE (default "report") unless
// --mode overrides it for this run. Report-only until Q7 is confirmed in
// writing; enforce anonymises closed loans at 3 years, archived users with
// them, and deletes scan_events at 90 days. Invoked by the systemd timer
// in deploy/systemd/hdms-retention.timer; see docs/runbooks/retention.md.
func runRetention(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("retention", flag.ContinueOnError)
	modeFlag := fs.String("mode", "", "report (default, from HDMS_RETENTION_MODE) or enforce (deletes/anonymises — only with written Q7 approval)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli retention [--mode report|enforce]")
	}
	mode, err := jobs.ParseRetentionMode(*modeFlag, cfg.RetentionMode)
	if err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	summary, err := jobs.RunRetention(ctx, pool.Pool, time.Now().UTC(), mode, cfg.JobMetricsDir)
	if err != nil {
		return err
	}
	fmt.Printf("Retention (%s): %d closed-loan, %d user, %d scan-event, %d audit candidates; anonymised %d loan(s) + %d user(s), deleted %d scan event(s), %d audit event(s)\n",
		summary.Mode, summary.LoanCandidates, summary.UserCandidates, summary.ScanEventCandidates, summary.AuditCandidates,
		summary.LoansAnonymised, summary.UsersAnonymised, summary.ScanEventsDeleted, summary.AuditEventsDeleted)
	return nil
}

// runOverdueScan implements 6.2a: hourly overdue loan detection, outbox event emission,
// escalation deduplication, and job_runs reporting. Invoked by systemd timer
// deploy/systemd/hdms-overdue-scan.timer; see docs/runbooks/overdue-scan.md.
func runOverdueScan(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("overdue-scan", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli overdue-scan (no arguments)")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	report, err := jobs.RunOverdueScan(ctx, pool, time.Now().UTC(), cfg.JobMetricsDir)
	if err != nil {
		return err
	}
	fmt.Printf("Overdue scan: %d overdue loan(s) checked, %d event(s) published, %d skipped dedupe\n",
		report.OverdueCount, report.EventsPublished, report.SkippedDedupe)
	return nil
}

// runWeeklyDigest implements 6.2c: weekly admin summary digest of overdue equipment.
// Invoked by systemd timer deploy/systemd/hdms-weekly-digest.timer; see docs/runbooks/weekly-digest.md.
func runWeeklyDigest(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("weekly-digest", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli weekly-digest (no arguments)")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	smtpTransport := notification.NewSMTPTransport(notification.SMTPConfig{
		Host:         cfg.SMTPHost,
		Port:         cfg.SMTPPort,
		Username:     cfg.SMTPUsername,
		Password:     cfg.SMTPPassword,
		FromAddress:  cfg.SMTPFromAddress,
		ReplyAddress: cfg.SMTPReplyAddress,
	})

	notifSvc := notification.New(pool, clock.System{}, notification.Config{
		Transport:             smtpTransport,
		QuietHours:            notification.DefaultQuietHoursConfig(),
		HumanContact:          cfg.SMTPReplyAddress,
		DefaultReturnLocation: "Room 101",
		MaxAttempts:           3,
		Logger:                slog.Default(),
	})

	report, err := jobs.RunWeeklyDigest(ctx, pool, notifSvc, time.Now().UTC(), cfg.JobMetricsDir)
	if err != nil {
		return err
	}
	fmt.Printf("Weekly digest: %d overdue loan(s), %d admin(s) targeted, %d enqueued, %d skipped dedupe\n",
		report.OverdueLoansCount, report.AdminsTargeted, report.DigestsEnqueued, report.SkippedDedupe)
	return nil
}

// runDirectorySync implements 6.3b/6.3c: staff roster synchronization against LDAP / Active Directory.
// Dry-run is the DEFAULT. Applying changes requires an explicit --apply flag.
// Invoked by systemd timer deploy/systemd/hdms-directory-sync.timer; see docs/runbooks/directory-sync.md.
func runDirectorySync(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("directory-sync", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "apply changes to the database (default is dry-run)")
	dryRun := fs.Bool("dry-run", false, "report what would change without modifying data (default)")
	maxChangeFraction := fs.Float64("max-change-fraction", 0.10, "fraction of roster threshold for mass-change safety refusal (default 0.10)")
	gracePeriodDays := fs.Int("grace-period-days", 7, "grace period in days before leavers are suspended (default 7)")
	issuer := fs.String("issuer", "ldap", "directory issuer identifier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli directory-sync [--apply] [--dry-run] [--max-change-fraction <0.10>] [--grace-period-days <7>] [--issuer <ldap>]")
	}

	isDryRun := !*apply || *dryRun

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	auditSvc := audit.New(pool)

	ldapClient := identity.NewLDAPDirectoryClient(identity.LDAPConfig{
		URL:          cfg.LDAPURL,
		BindDN:       cfg.LDAPBindDN,
		BindPassword: cfg.LDAPBindPassword,
		BaseDN:       cfg.LDAPBaseDN,
		UserFilter:   cfg.LDAPUserFilter,
	})

	opts := identity.DirectorySyncOptions{
		Issuer:            *issuer,
		DryRun:            isDryRun,
		Apply:             *apply && !*dryRun,
		MaxChangeFraction: *maxChangeFraction,
		GracePeriod:       time.Duration(*gracePeriodDays) * 24 * time.Hour,
		Actor:             "system:directory-sync",
	}

	report, err := jobs.RunDirectorySync(ctx, pool, auditSvc, ldapClient, time.Now().UTC(), opts, cfg.JobMetricsDir)
	if err != nil {
		if report.MassChangeRefused {
			fmt.Printf("Directory sync REFUSED: %s\n", report.RefusalReason)
			for _, c := range report.ProposedChanges {
				fmt.Printf("  - [%s] %s (%s): %s\n", c.Kind, c.EmployeeNo, c.Subject, c.Reason)
			}
		}
		return err
	}

	modeStr := "DRY-RUN"
	if !report.DryRun {
		modeStr = "APPLIED"
	}

	fmt.Printf("Directory sync (%s): %d directory entries, %d linked users, %d updated, %d reinstated, %d suspended (%d creds revoked, %d open loans escalated), %d grace period, %d unchanged\n",
		modeStr, report.TotalDirectory, report.TotalLinked, report.Updated, report.Reinstated, report.Suspended,
		report.CredentialsRevoked, report.OpenLoansEscalated, report.GracePeriod, report.Unchanged)

	return nil
}

// runReservationExpiry implements 6.4e: automated expiry of no-show reservations past grace period.
// Invoked by systemd timer deploy/systemd/hdms-reservation-expiry.timer; see docs/runbooks/reservation-expiry.md.
func runReservationExpiry(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("reservation-expiry", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli reservation-expiry (no arguments)")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	auditSvc := audit.New(pool)
	resSvc := reservations.New(pool, auditSvc, clock.System{})
	settingsSvc := settings.New(pool, nil)

	smtpTransport := notification.NewSMTPTransport(notification.SMTPConfig{
		Host:         cfg.SMTPHost,
		Port:         cfg.SMTPPort,
		Username:     cfg.SMTPUsername,
		Password:     cfg.SMTPPassword,
		FromAddress:  cfg.SMTPFromAddress,
		ReplyAddress: cfg.SMTPReplyAddress,
	})

	notifSvc := notification.New(pool, clock.System{}, notification.Config{
		Transport:             smtpTransport,
		QuietHours:            notification.DefaultQuietHoursConfig(),
		HumanContact:          cfg.SMTPReplyAddress,
		DefaultReturnLocation: "Room 101",
		MaxAttempts:           3,
		Logger:                slog.Default(),
	})

	report, err := jobs.RunReservationExpiry(ctx, pool, resSvc, settingsSvc, notifSvc, time.Now().UTC(), cfg.JobMetricsDir)
	if err != nil {
		return err
	}

	fmt.Printf("Reservation expiry: %d reservation(s) expired (grace: %dm), %d notification(s) enqueued, %d skipped dedupe\n",
		report.ExpiredCount, report.GraceMinutes, report.NotifsEnqueued, report.SkippedDedupe)
	return nil
}

func runImport(ctx context.Context, cfg config.Config, args []string, cat *i18n.Catalogue) error {
	if len(args) < 1 || (args[0] != "devices" && args[0] != "users") {
		return fmt.Errorf("usage: hdms-cli import <devices|users> --file <path> [--dry-run] [--mint-credentials]")
	}
	resource := args[0]

	fs := flag.NewFlagSet("import "+resource, flag.ContinueOnError)
	file := fs.String("file", "", "CSV file to import (required)")
	dryRun := fs.Bool("dry-run", false, "report what would happen without writing")
	mintCredentials := fs.Bool("mint-credentials", false, "issue a QR credential for each newly created row")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	cleanPath := filepath.Clean(*file)
	// #nosec G703 -- operator-supplied --file path on a local CLI; the operator already has the shell's file access.
	f, err := os.Open(cleanPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", cleanPath, err)
	}
	defer func() { _ = f.Close() }()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	auditSvc := audit.New(pool)
	credentialsSvc := credentials.New(pool, auditSvc, cfg.TokenPepper, cfg.CredentialEncKey)

	var report cliimport.Report
	switch resource {
	case "devices":
		catalogSvc := catalog.New(pool, auditSvc)
		report, err = cliimport.ImportDevices(ctx, cliimport.DeviceImportDeps{
			Catalog:     catalogSvc,
			Credentials: credentialsSvc,
		}, f, *dryRun, *mintCredentials)
	case "users":
		identitySvc := identity.New(pool, auditSvc)
		report, err = cliimport.ImportUsers(ctx, cliimport.UserImportDeps{
			Identity:    identitySvc,
			Credentials: credentialsSvc,
		}, f, *dryRun, *mintCredentials)
	}
	if err != nil {
		return err
	}

	_ = report.Print(os.Stdout)
	// Operator-facing localized summary — CLI output today, the one place a
	// raw key is not the right fallback, so we route through the catalogue.
	// Error wrapping and stderr remain untranslated per spec.
	if !*dryRun {
		created, updated, _ := report.Counts()
		total := created + updated
		if total > 0 {
			fmt.Println(cat.T("cli.import.done", total))
		}
	}
	if _, _, rejected := report.Counts(); rejected > 0 {
		return fmt.Errorf("%d row(s) rejected", rejected)
	}
	return nil
}

func runAdmin(ctx context.Context, cfg config.Config, args []string, cat *i18n.Catalogue) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hdms-cli admin <bootstrap|set-password|unlock>")
	}

	switch args[0] {
	case "bootstrap":
		fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
		email := fs.String("email", "", "admin account email (required)")
		name := fs.String("name", "", "admin account full name (required)")
		role := fs.String("role", "admin", "admin role: admin | technician | viewer")
		passwordFlag := fs.String("password", "", "admin account password (optional; prompted if omitted)")
		totpSecretFlag := fs.String("totp-secret", "", "optional base32 TOTP secret (for test environments)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *email == "" || *name == "" {
			return fmt.Errorf("--email and --name are required")
		}
		switch *role {
		case "admin", "technician", "viewer", "superadmin", "operator":
		default:
			return fmt.Errorf("--role must be one of admin, technician, viewer")
		}

		password := *passwordFlag
		if password == "" {
			var err error
			password, err = promptPassword()
			if err != nil {
				return err
			}
		} else if len(password) < 12 {
			return fmt.Errorf("password must be at least 12 characters")
		}

		if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		auditSvc := audit.New(pool)
		authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL, auth.WithAudit(auditSvc))
		id, secret, otpauthURL, err := authSvc.CreateAdminAccountWithSecret(ctx, *email, *name, password, *role, *totpSecretFlag)
		if err != nil {
			return fmt.Errorf("create admin account: %w", err)
		}

		// Operator-facing output routed through the catalogue — only stdout
		// result lines are translated, log/error wrapping stays on stderr
		// untranslated per spec.
		fmt.Print(cat.T("Admin account created: %s (%s, role=%s)\n\n", id, *email, *role))
		fmt.Println(cat.T("Scan this into your authenticator app now — it will not be shown again:"))
		fmt.Println()
		fmt.Println(cat.T("  otpauth URL: %s", otpauthURL))
		fmt.Println(cat.T("  raw secret: %s", secret))
		fmt.Println()
		return nil

	case "set-password":
		fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
		emailFlag := fs.String("email", "", "admin account email (required)")
		passwordFlag := fs.String("password", "", "new password (optional; prompted if omitted — prefer the prompt, a flag leaks into shell history)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *emailFlag == "" {
			return fmt.Errorf("usage: hdms-cli admin set-password --email <email>")
		}
		password := *passwordFlag
		if password == "" {
			var err error
			password, err = promptPassword()
			if err != nil {
				return err
			}
		} else if len(password) < 12 {
			return fmt.Errorf("password must be at least 12 characters")
		}

		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		auditSvc := audit.New(pool)
		authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL, auth.WithAudit(auditSvc))
		if err := authSvc.SetAdminPasswordByEmail(ctx, *emailFlag, password); err != nil {
			return fmt.Errorf("set admin password: %w", err)
		}

		fmt.Println(cat.T("Password updated for %s — all existing sessions revoked. The TOTP secret is unchanged.", *emailFlag))
		return nil

	case "unlock":
		fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
		emailFlag := fs.String("email", "", "admin account email (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		email := *emailFlag
		if email == "" && len(fs.Args()) > 0 {
			email = fs.Args()[0]
		}
		if email == "" {
			return fmt.Errorf("usage: hdms-cli admin unlock --email <email>")
		}

		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		auditSvc := audit.New(pool)
		authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL, auth.WithAudit(auditSvc))
		if err := authSvc.UnlockAdminByEmail(ctx, email); err != nil {
			return fmt.Errorf("unlock admin account: %w", err)
		}

		fmt.Println(cat.T("Admin account unlocked: %s", email))
		return nil

	default:
		return fmt.Errorf("usage: hdms-cli admin <bootstrap|set-password|unlock>")
	}
}

func runKiosk(ctx context.Context, cfg config.Config, args []string, cat *i18n.Catalogue) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hdms-cli kiosk <register --name --location|rotate <id>|pairing-code <id>>")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL)

	switch args[0] {
	case "register":
		fs := flag.NewFlagSet("kiosk register", flag.ContinueOnError)
		name := fs.String("name", "", "kiosk name (required)")
		location := fs.String("location", "", "kiosk location")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return fmt.Errorf("--name is required")
		}

		id, token, err := authSvc.RegisterKiosk(ctx, *name, *location)
		if err != nil {
			return fmt.Errorf("register kiosk: %w", err)
		}
		fmt.Print(cat.T("Kiosk registered: %s (%s)\n\n", id, *name))
		fmt.Println(cat.T("Bearer token — shown once, store it now:"))
		fmt.Println()
		fmt.Println(" ", token)
		fmt.Println()
		return nil

	case "rotate":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("usage: hdms-cli kiosk rotate <id>")
		}
		token, err := authSvc.RotateKioskToken(ctx, args[1])
		if err != nil {
			return fmt.Errorf("rotate kiosk token: %w", err)
		}
		fmt.Println(cat.T("New bearer token — shown once, store it now:"))
		fmt.Println()
		fmt.Println(" ", token)
		fmt.Println()
		return nil

	case "pairing-code":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("usage: hdms-cli kiosk pairing-code <id>")
		}
		code, expiresAt, err := authSvc.IssuePairingCode(ctx, args[1])
		if err != nil {
			return fmt.Errorf("issue pairing code: %w", err)
		}
		fmt.Print(cat.T("Pairing code (expires %s):\n\n", expiresAt.Format(time.RFC3339)))
		fmt.Println(" ", code)
		fmt.Println()
		return nil

	default:
		return fmt.Errorf("usage: hdms-cli kiosk <register --name --location|rotate <id>|pairing-code <id>>")
	}
}

func promptPassword() (string, error) {
	fmt.Print("Password: ")
	pw1, err := term.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Print("Confirm password: ")
	pw2, err := term.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(pw1) != string(pw2) {
		return "", fmt.Errorf("passwords did not match")
	}
	if len(pw1) < 12 {
		return "", fmt.Errorf("password must be at least 12 characters")
	}
	return string(pw1), nil
}

func runExport(args []string, cat *i18n.Catalogue) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hdms-cli export <machine|scenarios|all> [--out <path>] [--domain-dir <path>]")
	}

	target := args[0]
	fs := flag.NewFlagSet("export "+target, flag.ContinueOnError)
	out := fs.String("out", "", "output file path (optional)")
	domainDir := fs.String("domain-dir", "", "path to packages/domain directory (optional)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *out != "" && target == "all" {
		return fmt.Errorf("--out is not supported with target \"all\" (it writes two files); use --domain-dir or export machine/scenarios individually")
	}

	getDomainDir := func() string {
		if *domainDir != "" {
			return *domainDir
		}
		candidates := []string{
			"../hdms-frontend/packages/domain",
			"hdms-frontend/packages/domain",
			"packages/domain",
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				return c
			}
		}
		return "../hdms-frontend/packages/domain"
	}

	exportFile := func(filename string, data []byte) error {
		var outPath string
		if *out != "" {
			outPath = filepath.Clean(*out)
		} else {
			outPath = filepath.Clean(filepath.Join(getDomainDir(), filename))
		}

		// #nosec G703 -- operator-supplied --out path on a local CLI; not reachable over HTTP.
		if err := os.MkdirAll(filepath.Dir(outPath), 0750); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
		// #nosec G703 -- operator-supplied --out path on a local CLI; not reachable over HTTP.
		if err := os.WriteFile(outPath, data, 0600); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		fmt.Println(cat.T("Exported %s", outPath))
		return nil
	}

	switch target {
	case "machine":
		data, err := checkout.ExportMachineJSON()
		if err != nil {
			return err
		}
		return exportFile("session-machine.json", data)

	case "scenarios":
		data, err := checkout.ExportScenariosJSON()
		if err != nil {
			return err
		}
		return exportFile("scenarios.json", data)

	case "all":
		mdata, err := checkout.ExportMachineJSON()
		if err != nil {
			return err
		}
		if err := exportFile("session-machine.json", mdata); err != nil {
			return err
		}
		sdata, err := checkout.ExportScenariosJSON()
		if err != nil {
			return err
		}
		return exportFile("scenarios.json", sdata)

	default:
		return fmt.Errorf("usage: hdms-cli export <machine|scenarios|all> [--out <path>] [--domain-dir <path>]")
	}
}
