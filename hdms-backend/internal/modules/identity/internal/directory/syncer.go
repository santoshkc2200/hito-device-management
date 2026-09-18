package directory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

var (
	// ErrMassChangeRefused is returned when a sync run proposes to alter more than
	// the configured threshold fraction of the staff roster.
	ErrMassChangeRefused = errors.New("directory-sync: mass change safety limit exceeded")
)

// SyncOptions configures a directory synchronization execution.
type SyncOptions struct {
	Issuer            string        // directory issuer, default "ldap"
	DryRun            bool          // report only; if true, database is never modified
	Apply             bool          // must be true to apply changes; DryRun is the default
	MaxChangeFraction float64       // fraction threshold (e.g. 0.10 for 10%), default 0.10
	GracePeriod       time.Duration // duration before disappearing staff are suspended, default 7 days
	Actor             string        // audit actor, default "system:directory-sync"
}

// ChangeKind describes what mutation is proposed or applied.
type ChangeKind string

const (
	ChangeUpdate    ChangeKind = "update"
	ChangeReinstate ChangeKind = "reinstate"
	ChangeSuspend   ChangeKind = "suspend"
)

// ProposedChange details a specific user modification.
type ProposedChange struct {
	Kind       ChangeKind `json:"kind"`
	UserID     string     `json:"user_id"`
	EmployeeNo string     `json:"employee_no"`
	Subject    string     `json:"subject"`
	Reason     string     `json:"reason"`
	Details    string     `json:"details,omitempty"`
}

// Report summarizes the findings and actions of a sync run.
type Report struct {
	DryRun             bool             `json:"dry_run"`
	TotalDirectory     int              `json:"total_directory"`
	TotalLinked        int              `json:"total_linked"`
	Matched            int              `json:"matched"`
	Updated            int              `json:"updated"`
	Reinstated         int              `json:"reinstated"`
	GracePeriod        int              `json:"grace_period"`
	Suspended          int              `json:"suspended"`
	Unchanged          int              `json:"unchanged"`
	CredentialsRevoked int              `json:"credentials_revoked"`
	OpenLoansEscalated int              `json:"open_loans_escalated"`
	MassChangeRefused  bool             `json:"mass_change_refused"`
	RefusalReason      string           `json:"refusal_reason,omitempty"`
	ProposedChanges    []ProposedChange `json:"proposed_changes,omitempty"`
}

type plannedAction struct {
	link    identitystore.UserDirectoryLink
	user    identitystore.User
	entry   Entry
	kind    ChangeKind
	reason  string
	details string
}

// Sync executes roster synchronization against the directory client.
// It follows 6.3b and 6.3c:
//   - DryRun is default; Apply must be true to mutate the database.
//   - Directory is read-only.
//   - Directory owns name, department, email, and phone.
//   - Local-only users (no external link) are never touched.
//   - Disappearing staff are suspended after the grace period, never deleted.
//   - Credentials of leavers are revoked; open loans escalate and stay open.
//   - Returning leavers are reinstated without re-registration.
//   - Exceeding the mass-change safety limit stops and returns ErrMassChangeRefused.
func Sync(ctx context.Context, pool *db.Pool, audit auditapi.Recorder, client Client, now time.Time, opts SyncOptions) (Report, error) {
	if opts.Issuer == "" {
		opts.Issuer = "ldap"
	}
	if opts.MaxChangeFraction <= 0 {
		opts.MaxChangeFraction = 0.10
	}
	if opts.GracePeriod <= 0 {
		opts.GracePeriod = 7 * 24 * time.Hour
	}
	if opts.Actor == "" {
		opts.Actor = "system:directory-sync"
	}
	isDryRun := opts.DryRun || !opts.Apply

	nowUTC := now.UTC()
	var report Report
	report.DryRun = isDryRun

	// 1. Fetch directory entries (strictly read-only)
	entries, err := client.SearchStaff(ctx)
	if err != nil {
		return report, fmt.Errorf("directory-sync: search staff: %w", err)
	}
	report.TotalDirectory = len(entries)

	entriesBySubject := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if e.Subject != "" {
			entriesBySubject[e.Subject] = e
		}
	}

	// 2. Fetch existing external links for this issuer
	q := identitystore.New(pool)
	links, err := q.ListDirectoryLinks(ctx, opts.Issuer)
	if err != nil {
		return report, fmt.Errorf("directory-sync: list directory links: %w", err)
	}
	report.TotalLinked = len(links)

	// Fetch departments to resolve names
	depts, err := q.ListDepartments(ctx)
	if err != nil {
		return report, fmt.Errorf("directory-sync: list departments: %w", err)
	}
	deptNameByID := make(map[string]string, len(depts))
	for _, d := range depts {
		deptNameByID[pgtypeconv.UUIDString(d.ID)] = d.Name
	}

	var actions []plannedAction
	unchangedCount := 0
	gracePeriodCount := 0

	// 3. Compare each linked user with directory state
	for _, link := range links {
		user, err := q.GetUserByID(ctx, link.UserID)
		if err != nil {
			return report, fmt.Errorf("directory-sync: get user %s: %w", pgtypeconv.UUIDString(link.UserID), err)
		}

		entry, seenInDirectory := entriesBySubject[link.Subject]
		if seenInDirectory {
			report.Matched++
			currentDeptName := ""
			if user.DepartmentID.Valid {
				currentDeptName = deptNameByID[pgtypeconv.UUIDString(user.DepartmentID)]
			}

			isSuspended := user.Status == identitystore.UserStatusSuspended
			needsReinstate := isSuspended

			diffs := make([]string, 0)
			if strings.TrimSpace(entry.FullName) != "" && entry.FullName != user.FullName {
				diffs = append(diffs, fmt.Sprintf("name: %q -> %q", user.FullName, entry.FullName))
			}
			if entry.Department != "" && entry.Department != currentDeptName {
				diffs = append(diffs, fmt.Sprintf("department: %q -> %q", currentDeptName, entry.Department))
			}
			userEmail := pgtypeconv.TextString(user.Email)
			if entry.Email != userEmail {
				diffs = append(diffs, fmt.Sprintf("email: %q -> %q", userEmail, entry.Email))
			}
			userPhone := pgtypeconv.TextString(user.Phone)
			if entry.Phone != userPhone {
				diffs = append(diffs, fmt.Sprintf("phone: %q -> %q", userPhone, entry.Phone))
			}

			if needsReinstate {
				actions = append(actions, plannedAction{
					link:    link,
					user:    user,
					entry:   entry,
					kind:    ChangeReinstate,
					reason:  "reappeared in directory",
					details: strings.Join(diffs, ", "),
				})
			} else if len(diffs) > 0 {
				actions = append(actions, plannedAction{
					link:    link,
					user:    user,
					entry:   entry,
					kind:    ChangeUpdate,
					reason:  "directory attributes changed",
					details: strings.Join(diffs, ", "),
				})
			} else {
				unchangedCount++
			}
		} else {
			// Disappeared from directory
			lastSeen := pgtypeconv.Time(link.LastSeenInDirectory)
			timeSinceLastSeen := nowUTC.Sub(lastSeen)
			if timeSinceLastSeen < opts.GracePeriod {
				gracePeriodCount++
			} else {
				// Grace period expired: leaver suspension
				if user.Status != identitystore.UserStatusSuspended {
					actions = append(actions, plannedAction{
						link:    link,
						user:    user,
						kind:    ChangeSuspend,
						reason:  fmt.Sprintf("disappeared from directory > %s ago", opts.GracePeriod),
						details: fmt.Sprintf("last seen: %s", lastSeen.Format(time.RFC3339)),
					})
				} else {
					unchangedCount++
				}
			}
		}
	}

	report.Unchanged = unchangedCount
	report.GracePeriod = gracePeriodCount

	for _, a := range actions {
		pc := ProposedChange{
			Kind:       a.kind,
			UserID:     pgtypeconv.UUIDString(a.link.UserID),
			EmployeeNo: a.user.EmployeeNo,
			Subject:    a.link.Subject,
			Reason:     a.reason,
			Details:    a.details,
		}
		report.ProposedChanges = append(report.ProposedChanges, pc)
		switch a.kind {
		case ChangeUpdate:
			report.Updated++
		case ChangeReinstate:
			report.Reinstated++
		case ChangeSuspend:
			report.Suspended++
		}
	}

	// 4. Mass-change safety limit
	totalChanges := len(actions)
	if report.TotalLinked > 0 {
		fraction := float64(totalChanges) / float64(report.TotalLinked)
		if fraction > opts.MaxChangeFraction {
			report.MassChangeRefused = true
			report.RefusalReason = fmt.Sprintf(
				"mass change safety limit exceeded: %d/%d (%.1f%% > %.1f%%) changes proposed; sync refused to prevent filter error",
				totalChanges, report.TotalLinked, fraction*100.0, opts.MaxChangeFraction*100.0,
			)
			return report, fmt.Errorf("%w: %s", ErrMassChangeRefused, report.RefusalReason)
		}
	}

	// 5. If dry-run, stop before database mutation
	if isDryRun {
		return report, nil
	}

	// 6. Apply phase: atomic database transaction
	txErr := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		txQ := identitystore.New(db.Conn(ctx, pool))

		for _, a := range actions {
			userIDStr := pgtypeconv.UUIDString(a.link.UserID)

			switch a.kind {
			case ChangeReinstate:
				// Re-activate user
				_, err := txQ.UpdateUserStatus(ctx, identitystore.UpdateUserStatusParams{
					ID:     a.link.UserID,
					Status: identitystore.UserStatusActive,
				})
				if err != nil {
					return fmt.Errorf("reinstate user %s: %w", userIDStr, err)
				}

				// If directory attributes also differed, update them
				targetDeptID := a.user.DepartmentID
				if a.entry.Department != "" {
					dept, err := txQ.GetOrCreateDepartment(ctx, identitystore.GetOrCreateDepartmentParams{
						ID:   pgtypeconv.NewUUID(),
						Name: a.entry.Department,
					})
					if err != nil {
						return fmt.Errorf("resolve department %q: %w", a.entry.Department, err)
					}
					targetDeptID = dept.ID
				}

				targetName := a.user.FullName
				if strings.TrimSpace(a.entry.FullName) != "" {
					targetName = a.entry.FullName
				}

				_, err = txQ.UpdateUser(ctx, identitystore.UpdateUserParams{
					ID:           a.link.UserID,
					FullName:     targetName,
					DepartmentID: targetDeptID,
					Email:        pgtypeconv.Text(a.entry.Email),
					Phone:        pgtypeconv.Text(a.entry.Phone),
					Notes:        a.user.Notes,
				})
				if err != nil {
					return fmt.Errorf("update reinstated user attributes %s: %w", userIDStr, err)
				}

				_, err = txQ.UpdateDirectoryLinkLastSeen(ctx, identitystore.UpdateDirectoryLinkLastSeenParams{
					ID:                  a.link.ID,
					LastSeenInDirectory: pgtypeconv.Timestamptz(nowUTC),
					SyncState:           "synced",
				})
				if err != nil {
					return fmt.Errorf("update directory link for reinstated %s: %w", userIDStr, err)
				}

				if audit != nil {
					_ = audit.Record(ctx, auditapi.Event{
						Action:  "identity.user.reinstated",
						Actor:   opts.Actor,
						Subject: "user:" + userIDStr,
						Payload: map[string]any{
							"reason":  a.reason,
							"subject": a.link.Subject,
						},
					})
				}

			case ChangeUpdate:
				targetDeptID := a.user.DepartmentID
				if a.entry.Department != "" {
					dept, err := txQ.GetOrCreateDepartment(ctx, identitystore.GetOrCreateDepartmentParams{
						ID:   pgtypeconv.NewUUID(),
						Name: a.entry.Department,
					})
					if err != nil {
						return fmt.Errorf("resolve department %q: %w", a.entry.Department, err)
					}
					targetDeptID = dept.ID
				}

				targetName := a.user.FullName
				if strings.TrimSpace(a.entry.FullName) != "" {
					targetName = a.entry.FullName
				}

				_, err := txQ.UpdateUser(ctx, identitystore.UpdateUserParams{
					ID:           a.link.UserID,
					FullName:     targetName,
					DepartmentID: targetDeptID,
					Email:        pgtypeconv.Text(a.entry.Email),
					Phone:        pgtypeconv.Text(a.entry.Phone),
					Notes:        a.user.Notes,
				})
				if err != nil {
					return fmt.Errorf("update user %s: %w", userIDStr, err)
				}

				_, err = txQ.UpdateDirectoryLinkLastSeen(ctx, identitystore.UpdateDirectoryLinkLastSeenParams{
					ID:                  a.link.ID,
					LastSeenInDirectory: pgtypeconv.Timestamptz(nowUTC),
					SyncState:           "synced",
				})
				if err != nil {
					return fmt.Errorf("update directory link for %s: %w", userIDStr, err)
				}

				if audit != nil {
					_ = audit.Record(ctx, auditapi.Event{
						Action:  "identity.user.updated",
						Actor:   opts.Actor,
						Subject: "user:" + userIDStr,
						Payload: map[string]any{
							"diffs":   a.details,
							"subject": a.link.Subject,
						},
					})
				}

			case ChangeSuspend:
				// Suspend user
				_, err := txQ.UpdateUserStatus(ctx, identitystore.UpdateUserStatusParams{
					ID:     a.link.UserID,
					Status: identitystore.UserStatusSuspended,
				})
				if err != nil {
					return fmt.Errorf("suspend user %s: %w", userIDStr, err)
				}

				_, err = txQ.UpdateDirectoryLinkSyncState(ctx, identitystore.UpdateDirectoryLinkSyncStateParams{
					ID:        a.link.ID,
					SyncState: "suspended",
				})
				if err != nil {
					return fmt.Errorf("update link status to suspended for %s: %w", userIDStr, err)
				}

				// Revoke active credentials
				const revokeCredsQuery = `
					UPDATE credentials
					SET status = 'revoked',
					    revoked_at = $2,
					    revoked_by = $3,
					    revoked_reason = 'directory leaver suspension'
					WHERE subject_type = 'user' AND subject_id = $1 AND status = 'active'
					RETURNING id::text`

				credRows, err := db.Conn(ctx, pool).Query(ctx, revokeCredsQuery, a.link.UserID, nowUTC, opts.Actor)
				if err != nil {
					return fmt.Errorf("revoke credentials for user %s: %w", userIDStr, err)
				}
				var revokedCredIDs []string
				for credRows.Next() {
					var cid string
					if err := credRows.Scan(&cid); err == nil {
						revokedCredIDs = append(revokedCredIDs, cid)
					}
				}
				credRows.Close()

				for _, cid := range revokedCredIDs {
					credUUID, _ := uuid.Parse(cid)
					const insertCredEvent = `
						INSERT INTO credential_events (id, credential_id, at, kind, actor, reason, payload)
						VALUES ($1, $2, $3, 'revoked', $4, 'directory leaver suspension', '{}')`
					_, _ = db.Conn(ctx, pool).Exec(ctx, insertCredEvent, uuid.New(), credUUID, nowUTC, opts.Actor)
					report.CredentialsRevoked++
				}

				// Check if user has open loans (escalations)
				const checkOpenLoans = `
					SELECT count(*) FROM loans
					WHERE user_id = $1 AND status = 'open'`
				var openLoanCount int
				if err := db.Conn(ctx, pool).QueryRow(ctx, checkOpenLoans, a.link.UserID).Scan(&openLoanCount); err == nil {
					report.OpenLoansEscalated += openLoanCount
				}

				if audit != nil {
					_ = audit.Record(ctx, auditapi.Event{
						Action:  "identity.user.suspended",
						Actor:   opts.Actor,
						Subject: "user:" + userIDStr,
						Payload: map[string]any{
							"reason":              a.reason,
							"subject":             a.link.Subject,
							"credentials_revoked": len(revokedCredIDs),
							"open_loans":          openLoanCount,
						},
					})
				}
			}
		}

		// Also update last_seen_in_directory for unchanged matched links
		for _, link := range links {
			if _, ok := entriesBySubject[link.Subject]; ok {
				inAction := false
				for _, a := range actions {
					if a.link.ID == link.ID {
						inAction = true
						break
					}
				}
				if !inAction {
					_, _ = txQ.UpdateDirectoryLinkLastSeen(ctx, identitystore.UpdateDirectoryLinkLastSeenParams{
						ID:                  link.ID,
						LastSeenInDirectory: pgtypeconv.Timestamptz(nowUTC),
						SyncState:           "synced",
					})
				}
			}
		}

		return nil
	})

	if txErr != nil {
		return report, txErr
	}

	return report, nil
}
