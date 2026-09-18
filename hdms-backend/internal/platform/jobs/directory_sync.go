package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// RunDirectorySync executes staff roster synchronization (6.3b/6.3c).
//
// Like all scheduled jobs, it writes a job_runs row, exports a last-success metric
// for node_exporter textfiles, and is safe to execute on a schedule or manually.
// DryRun is the default; applying mutations requires an explicit Apply: true option.
func RunDirectorySync(
	ctx context.Context,
	pool *db.Pool,
	audit auditapi.Recorder,
	client identity.DirectoryClient,
	now time.Time,
	opts identity.DirectorySyncOptions,
	metricsDir string,
) (identity.DirectorySyncReport, error) {
	started := now.UTC()

	identitySvc := identity.New(pool, audit)
	report, syncErr := identitySvc.SyncDirectory(ctx, client, started, opts)

	finished := time.Now().UTC()

	if syncErr != nil {
		detail := map[string]any{
			"dry_run":             opts.DryRun || !opts.Apply,
			"mass_change_refused": report.MassChangeRefused,
			"refusal_reason":      report.RefusalReason,
			"error":               syncErr.Error(),
		}
		if len(report.ProposedChanges) > 0 {
			detail["proposed_changes"] = report.ProposedChanges
		}
		_ = recordJobRun(ctx, pool.Pool, "directory-sync", started, finished, "failure", detail)
		return report, syncErr
	}

	detail := map[string]any{
		"dry_run":              report.DryRun,
		"total_directory":      report.TotalDirectory,
		"total_linked":         report.TotalLinked,
		"matched":              report.Matched,
		"updated":              report.Updated,
		"reinstated":           report.Reinstated,
		"grace_period":         report.GracePeriod,
		"suspended":            report.Suspended,
		"unchanged":            report.Unchanged,
		"credentials_revoked":  report.CredentialsRevoked,
		"open_loans_escalated": report.OpenLoansEscalated,
	}
	if len(report.ProposedChanges) > 0 {
		detail["proposed_changes"] = report.ProposedChanges
	}

	if err := recordJobRun(ctx, pool.Pool, "directory-sync", started, finished, "success", detail); err != nil {
		return report, fmt.Errorf("jobs: record directory-sync job_run: %w", err)
	}

	if err := WriteLastSuccess(metricsDir, "directory-sync", finished); err != nil {
		return report, fmt.Errorf("jobs: write last success metric: %w", err)
	}

	return report, nil
}
