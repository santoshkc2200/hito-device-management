package checkout

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/tokens"
)

// errPreviewRollback is the sentinel PreviewPaperBatch returns from the
// transaction function after a clean run: TxManager.Do treats it as a
// failure and rolls back, which is exactly what a preview wants — every
// row was validated (and written, so even intra-batch interactions are
// seen) and none of it is kept.
var errPreviewRollback = errors.New("checkout: preview complete, rolling back")

// conflictResolutions is the fixed menu the UI may offer for a custody
// conflict (FR-75). The application never picks one.
var conflictResolutions = []string{
	checkoutapi.ResolveTruncateExisting,
	checkoutapi.ResolveChangeDevice,
	checkoutapi.ResolveDiscardRow,
	checkoutapi.ResolveRecordDisputed,
}

// ResolveHistorical answers borrow/return/conflict for a device, a person
// and a past instant, against custody as it stood at that instant (FR-74).
// It is the same machine.ResolveAction the live scan path consults, over
// lending.CustodyAt instead of a live pending device, so the kiosk and the
// backfill screen's auto-detection cannot disagree.
func (s *Service) ResolveHistorical(ctx context.Context, deviceID, userID string, at time.Time) (checkoutapi.HistoricalAction, error) {
	if at.After(s.clock.Now()) {
		return "", checkoutapi.ErrHistoricalTimeInFuture
	}
	holder, err := s.deps.Loans.CustodyAt(ctx, deviceID, at)
	if errors.Is(err, lendingapi.ErrLoanNotFound) {
		return checkoutapi.HistoricalBorrow, nil
	}
	if err != nil {
		return "", fmt.Errorf("checkout: custody at: %w", err)
	}
	switch machine.ResolveAction(holder.UserID, userID) {
	case machine.ActionBorrow:
		return checkoutapi.HistoricalBorrow, nil
	case machine.ActionReturn:
		return checkoutapi.HistoricalReturn, nil
	default:
		return checkoutapi.HistoricalConflict, nil
	}
}

// PreviewPaperBatch validates a staged batch without writing anything: the
// identical applyPaperBatch run RecordPaperBatch performs, inside a
// transaction that is always rolled back (2.4b.4). A stale preview can
// therefore never act as a grant — the commit re-runs everything.
func (s *Service) PreviewPaperBatch(ctx context.Context, batch checkoutapi.PaperBatch, actor string) (checkoutapi.PaperBatchResult, error) {
	if err := validateBatchShape(batch); err != nil {
		return checkoutapi.PaperBatchResult{}, err
	}

	var result checkoutapi.PaperBatchResult
	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		res, err := s.applyPaperBatch(ctx, batch, actor)
		if err != nil {
			return err
		}
		result = res
		return errPreviewRollback
	})
	if errors.Is(err, errPreviewRollback) {
		return result, nil
	}
	return checkoutapi.PaperBatchResult{}, err
}

// RecordPaperBatch commits a batch in one transaction — all rows or none.
// Any row that is still conflicting or unresolved rejects the whole batch:
// the PaperBatchError carries the complete per-row result so the
// administrator fixes exactly what is wrong and saves the page again. A
// half-saved page is worse than an unsaved one, because they cannot tell
// which half went in.
func (s *Service) RecordPaperBatch(ctx context.Context, batch checkoutapi.PaperBatch, actor string) (checkoutapi.PaperBatchResult, error) {
	if err := validateBatchShape(batch); err != nil {
		return checkoutapi.PaperBatchResult{}, err
	}

	var result checkoutapi.PaperBatchResult
	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		res, err := s.applyPaperBatch(ctx, batch, actor)
		if err != nil {
			return err
		}
		rejected := &checkoutapi.PaperBatchError{Result: res}
		if rejected.HasConflicts() || rejected.HasUnresolved() {
			return rejected // rolls the transaction back with it
		}
		result = res
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "backfill.batch_recorded", Subject: "paper:" + batch.PaperRef,
			Payload: map[string]any{"rows": len(batch.Rows), "paperRef": batch.PaperRef},
		})
	})
	if err != nil {
		var rejected *checkoutapi.PaperBatchError
		if errors.As(err, &rejected) {
			return rejected.Result, rejected
		}
		return checkoutapi.PaperBatchResult{}, err
	}
	result.Committed = true
	return result, nil
}

// validateBatchShape checks the batch-level facts that are about the
// request, not any row: a paperRef (INV-14: every row carries provenance),
// at least one row, and unique client row ids so per-row results can be
// keyed unambiguously.
func validateBatchShape(batch checkoutapi.PaperBatch) error {
	if strings.TrimSpace(batch.PaperRef) == "" {
		return checkoutapi.ErrPaperRefRequired
	}
	if len(batch.Rows) == 0 {
		return checkoutapi.ErrEmptyPaperBatch
	}
	seen := make(map[string]struct{}, len(batch.Rows))
	for _, row := range batch.Rows {
		if _, dup := seen[row.ClientRowID]; dup {
			return fmt.Errorf("%w: duplicate clientRowId %q", checkoutapi.ErrPaperRowMalformed, row.ClientRowID)
		}
		seen[row.ClientRowID] = struct{}{}
	}
	return nil
}

// paperRowWork is one row's resolved state partway through applyPaperBatch.
type paperRowWork struct {
	index    int // position in batch.Rows; also names the row's SAVEPOINT
	row      checkoutapi.PaperRow
	paperRef string // the batch's page reference, stamped on every write
	device   catalogapi.DeviceSummary
	user     identityapi.UserSummary
	userID   string
	creates  bool // this row's person did not exist before the batch

	// covering is the loan holding the device at the row's out-time, in
	// this transaction's view — including rows earlier in the same batch.
	covering *lendingapi.Loan
}

// applyPaperBatch is the one validation-and-write pass both preview and
// commit run (2.4b's "preview and commit cannot drift" rule). It must be
// called inside a transaction: every read resolves through db.Conn so it
// sees earlier rows of the same batch, and every write is the real write.
// Rows are processed in borrowed_at order — the order the exclusion
// constraint itself reasons in — while results are stored by original
// index so the response never reorders the administrator's page.
func (s *Service) applyPaperBatch(ctx context.Context, batch checkoutapi.PaperBatch, actor string) (checkoutapi.PaperBatchResult, error) {
	now := s.clock.Now()
	results := make([]checkoutapi.PaperRowResult, len(batch.Rows))
	for i := range results {
		results[i] = checkoutapi.PaperRowResult{ClientRowID: batch.Rows[i].ClientRowID}
	}

	order := make([]int, len(batch.Rows))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return batch.Rows[a].BorrowedAt.Compare(batch.Rows[b].BorrowedAt)
	})

	// inlineUsers dedupes newUser rows within one batch (two rows about the
	// same new person create one user) and lets a re-submitted batch find a
	// user an earlier attempt created instead of duplicating them.
	inlineUsers := make(map[string]string)
	createdUsers := make(map[string]struct{})
	var touchedDevices []string

	for _, idx := range order {
		res := &results[idx]
		work, ok := s.resolvePaperRow(ctx, batch.Rows[idx], res, inlineUsers, actor, now)
		if !ok {
			continue // unresolved: reason already on res, nothing written
		}
		work.index, work.paperRef = idx, batch.PaperRef

		res.Device = &checkoutapi.DeviceView{ID: work.device.ID, AssetTag: work.device.AssetTag, Name: work.device.Name}
		res.User = &checkoutapi.PaperUserView{ID: work.userID, FullName: work.user.FullName}
		res.CreatesUser = work.creates
		if work.user.DepartmentID != "" {
			if dept, err := s.deps.Users.LookupDepartment(ctx, work.user.DepartmentID); err == nil {
				res.User.Department = dept.Name
			}
		}
		if !work.device.CreatedAt.IsZero() && work.row.BorrowedAt.Before(work.device.CreatedAt) {
			res.Warnings = append(res.Warnings,
				"the out-time precedes the device's registration date; recorded as typed")
		}
		if work.creates {
			createdUsers[work.userID] = struct{}{}
		}

		if err := s.actOnPaperRow(ctx, work, res, &touchedDevices, actor, now); err != nil {
			return checkoutapi.PaperBatchResult{}, err
		}
	}

	if err := s.reconcileDeviceStatuses(ctx, touchedDevices, actor); err != nil {
		return checkoutapi.PaperBatchResult{}, err
	}

	summary := checkoutapi.PaperSummary{NewUsers: len(createdUsers)}
	for _, res := range results {
		switch res.Status {
		case checkoutapi.PaperRowOK, checkoutapi.PaperRowDiscarded:
			summary.OK++ // a discarded row is settled, not a problem
		case checkoutapi.PaperRowConflict:
			summary.Conflicts++
		}
	}
	return checkoutapi.PaperBatchResult{Rows: results, Summary: summary}, nil
}

// resolvePaperRow resolves a row's device and person references and
// validates its times. Unresolvable rows return ok=false with Field/Reason
// set on res — a per-row status, never a batch-level failure (2.4b.2: the
// administrator must see every row's problem at once).
func (s *Service) resolvePaperRow(
	ctx context.Context, row checkoutapi.PaperRow, res *checkoutapi.PaperRowResult,
	inlineUsers map[string]string, actor string, now time.Time,
) (*paperRowWork, bool) {
	work := &paperRowWork{row: row}

	// Times are checked first, before resolving device/user, because
	// resolving a newUser reference can create a person (a real write): a
	// row with an invalid time range should fail without that side effect.
	switch {
	case row.BorrowedAt.IsZero():
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "borrowedAt", "the out-time is required"
	case row.BorrowedAt.After(now):
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "borrowedAt", "the out-time is in the future"
	case row.ReturnedAt != nil && !row.ReturnedAt.After(row.BorrowedAt):
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "returnedAt", "the in-time must be after the out-time"
	case row.ReturnedAt != nil && row.ReturnedAt.After(now):
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "returnedAt", "the in-time is in the future"
	}
	if res.Status == checkoutapi.PaperRowUnresolved {
		return nil, false
	}

	device, devErr := s.resolvePaperDevice(ctx, row.DeviceRef)
	if devErr != nil {
		res.Status = checkoutapi.PaperRowUnresolved
		res.Field, res.Reason = "deviceRef", devErr.Error()
		return nil, false
	}
	work.device = device

	user, userID, creates, userErr := s.resolvePaperUser(ctx, row.UserRef, inlineUsers, actor)
	if userErr != nil {
		res.Status = checkoutapi.PaperRowUnresolved
		res.Field, res.Reason = "userRef", userErr.Error()
		return nil, false
	}
	work.user, work.userID, work.creates = user, userID, creates

	return work, true
}

// resolvePaperDevice accepts an asset tag or a scanned credential token —
// one field, so a USB scanner on the admin PC fills it with one trigger
// pull (2.4b.2).
func (s *Service) resolvePaperDevice(ctx context.Context, deviceRef string) (catalogapi.DeviceSummary, error) {
	ref := strings.TrimSpace(deviceRef)
	if ref == "" {
		return catalogapi.DeviceSummary{}, errors.New("the device field is empty")
	}
	// A token-shaped ref (valid checksum and namespace) resolves through
	// credentials; anything else is treated as an asset tag. Asset tags
	// never carry a valid token checksum, so the two never collide.
	if _, err := tokens.Parse(ref); err == nil {
		sr, err := s.deps.Tokens.Resolve(ctx, ref)
		switch {
		case errors.Is(err, credentialsapi.ErrCredentialUnknown):
			return catalogapi.DeviceSummary{}, errors.New("that token does not match any credential")
		case err != nil:
			return catalogapi.DeviceSummary{}, fmt.Errorf("resolve device token: %w", err)
		case sr.Type != credentialsapi.RefDevice:
			return catalogapi.DeviceSummary{}, errors.New("that token is a person's card, not a device's")
		case sr.CredentialStatus != credentialsapi.StatusActive:
			return catalogapi.DeviceSummary{}, errors.New("that device's credential is revoked")
		}
		return s.deps.Devices.LookupDevice(ctx, sr.SubjectID)
	}
	return s.deps.Devices.LookupDeviceByAssetTag(ctx, ref)
}

// resolvePaperUser resolves the userRef discriminated union: userId,
// employeeNo, a scanned token, or an inline newUser (FR-73). Exactly one
// discriminator may be set — guessing between two is how a loan lands on
// the wrong Sharma.
func (s *Service) resolvePaperUser(
	ctx context.Context, ref checkoutapi.PaperUserRef, inlineUsers map[string]string, actor string,
) (identityapi.UserSummary, string, bool, error) {
	set := 0
	for _, v := range []string{ref.UserID, ref.EmployeeNo, ref.Token} {
		if strings.TrimSpace(v) != "" {
			set++
		}
	}
	if ref.NewUser != nil {
		set++
	}
	if set != 1 {
		return identityapi.UserSummary{}, "", false, errors.New("exactly one of userId, employeeNo, token or newUser is required")
	}

	switch {
	case strings.TrimSpace(ref.UserID) != "":
		u, err := s.deps.Users.LookupUser(ctx, strings.TrimSpace(ref.UserID))
		if err != nil {
			return identityapi.UserSummary{}, "", false, errors.New("no user with that id")
		}
		return u, u.ID, false, nil

	case strings.TrimSpace(ref.EmployeeNo) != "":
		u, err := s.deps.Users.LookupUserByEmployeeNo(ctx, strings.TrimSpace(ref.EmployeeNo))
		if err != nil {
			return identityapi.UserSummary{}, "", false, fmt.Errorf("no live user with employee number %s", strings.TrimSpace(ref.EmployeeNo))
		}
		return u, u.ID, false, nil

	case strings.TrimSpace(ref.Token) != "":
		u, err := s.resolvePaperUserToken(ctx, strings.TrimSpace(ref.Token))
		if err != nil {
			return identityapi.UserSummary{}, "", false, err
		}
		return u, u.ID, false, nil

	default:
		nu := ref.NewUser
		if strings.TrimSpace(nu.FullName) == "" || strings.TrimSpace(nu.EmployeeNo) == "" {
			return identityapi.UserSummary{}, "", false, errors.New("a new person needs at least a full name and an employee number")
		}
		key := strings.ToLower(strings.TrimSpace(nu.EmployeeNo))
		if existing, ok := inlineUsers[key]; ok {
			u, err := s.deps.Users.LookupUser(ctx, existing)
			if err == nil {
				return u, u.ID, false, nil
			}
		}
		u, err := s.deps.Users.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   strings.TrimSpace(nu.EmployeeNo),
			FullName:     strings.TrimSpace(nu.FullName),
			DepartmentID: nu.DepartmentID,
			// INV-11: the actor here is 'admin:<id>' by construction —
			// backfill is admin-only and the actor string is derived from
			// the authenticated admin, never taken from the request.
			RegisteredBy: actor,
		})
		if err != nil {
			// An employee number that appeared between preview and commit
			// (or in an earlier partially-failed attempt — the batch is
			// atomic, so "earlier attempt" means a concurrent admin) is a
			// person to reuse, not a duplicate to create.
			if errors.Is(err, identityapi.ErrEmployeeNoTaken) {
				if existing, lookupErr := s.deps.Users.LookupUserByEmployeeNo(ctx, strings.TrimSpace(nu.EmployeeNo)); lookupErr == nil {
					return existing, existing.ID, false, nil
				}
			}
			return identityapi.UserSummary{}, "", false, fmt.Errorf("could not create the new person: %w", err)
		}
		inlineUsers[key] = u.ID
		return u, u.ID, true, nil
	}
}

// resolvePaperUserToken resolves a scanned person card, refusing the
// non-active states a scanner can produce.
func (s *Service) resolvePaperUserToken(ctx context.Context, token string) (identityapi.UserSummary, error) {
	sr, err := s.deps.Tokens.Resolve(ctx, token)
	switch {
	case errors.Is(err, credentialsapi.ErrCredentialUnknown):
		return identityapi.UserSummary{}, errors.New("that token does not match any credential")
	case err != nil:
		return identityapi.UserSummary{}, fmt.Errorf("resolve person token: %w", err)
	case sr.Type != credentialsapi.RefUser:
		return identityapi.UserSummary{}, errors.New("that token belongs to a device, not a person")
	case sr.CredentialStatus != credentialsapi.StatusActive:
		return identityapi.UserSummary{}, errors.New("that card is revoked or otherwise not active")
	}
	return s.deps.Users.LookupUser(ctx, sr.SubjectID)
}

// actOnPaperRow detects the row's action (auto unless overridden, FR-74),
// applies any conflict resolution, and performs the write — all inside a
// per-row SAVEPOINT so one row's constraint violation is that row's
// structured conflict rather than the death of the transaction.
func (s *Service) actOnPaperRow(
	ctx context.Context, work *paperRowWork, res *checkoutapi.PaperRowResult,
	touchedDevices *[]string, actor string, now time.Time,
) error {
	row := work.row

	covering, err := s.deps.Loans.CustodyAt(ctx, work.device.ID, row.BorrowedAt)
	switch {
	case errors.Is(err, lendingapi.ErrLoanNotFound):
		work.covering = nil
	case err != nil:
		return fmt.Errorf("checkout: custody at out-time: %w", err)
	default:
		work.covering = &covering
	}

	action, actionErr := paperRowAction(row, work)
	if actionErr != nil {
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, actionErr.field, actionErr.err.Error()
		return nil
	}
	res.Action = string(action)

	// A borrow against a device somebody already holds at that instant (or
	// a forced return of somebody else's loan) is the conflict case —
	// structured, with the existing loan attached and the resolution menu
	// offered; the application never picks one.
	if action == checkoutapi.HistoricalBorrow && work.covering != nil ||
		action == checkoutapi.HistoricalReturn && work.covering != nil && work.covering.UserID != work.userID {
		return s.applyConflictResolution(ctx, work, res, touchedDevices, actor, now)
	}
	if action == checkoutapi.HistoricalReturn {
		return s.writePaperReturn(ctx, work, res, touchedDevices, actor, now)
	}
	return s.writePaperBorrow(ctx, work, res, touchedDevices, actor, now, false)
}

// fieldError names the offending field of an unresolved row.
type fieldError struct {
	field string
	err   error
}

func (e *fieldError) Error() string { return e.err.Error() }

// paperRowAction decides borrow/return for a row: the auto-detection
// (machine.ResolveAction over custody at the out-time — the same judgement
// the kiosk makes, FR-74) unless the administrator overrode it.
func paperRowAction(row checkoutapi.PaperRow, work *paperRowWork) (checkoutapi.HistoricalAction, *fieldError) {
	holder := ""
	if work.covering != nil {
		holder = work.covering.UserID
	}
	auto := checkoutapi.HistoricalBorrow
	switch machine.ResolveAction(holder, work.userID) {
	case machine.ActionReturn:
		auto = checkoutapi.HistoricalReturn
	case machine.ActionReject:
		auto = checkoutapi.HistoricalConflict
	}

	switch strings.TrimSpace(row.Action) {
	case "":
		if auto == checkoutapi.HistoricalConflict {
			// Auto-detected conflicts fall through to the resolution path
			// below as a borrow the system cannot accept — represented as
			// borrow-plus-covering so applyConflictResolution handles them.
			return checkoutapi.HistoricalBorrow, nil
		}
		return auto, nil
	case string(checkoutapi.HistoricalBorrow):
		return checkoutapi.HistoricalBorrow, nil
	case string(checkoutapi.HistoricalReturn):
		if auto == checkoutapi.HistoricalConflict {
			// Returning a device somebody else holds is the same conflict,
			// presented through the same path.
			return checkoutapi.HistoricalReturn, nil
		}
		if work.covering == nil {
			return "", &fieldError{"action", errors.New("return was chosen, but the device was not out to anyone at that time")}
		}
		return checkoutapi.HistoricalReturn, nil
	default:
		return "", &fieldError{"action", fmt.Errorf("unknown action %q (borrow or return)", row.Action)}
	}
}

// applyConflictResolution handles a row the exclusion constraint (or its
// pre-check) rejects: report the structured conflict, honour the chosen
// resolution, or leave the row flagged for the administrator.
func (s *Service) applyConflictResolution(
	ctx context.Context, work *paperRowWork, res *checkoutapi.PaperRowResult,
	touchedDevices *[]string, actor string, now time.Time,
) error {
	existing := *work.covering
	conflict := &checkoutapi.PaperConflict{
		Type:        "overlapping-custody",
		Resolutions: conflictResolutions,
	}
	res.Conflict = conflict

	switch strings.TrimSpace(work.row.Resolution) {
	case "":
		res.Status = checkoutapi.PaperRowConflict
		return s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing)

	case checkoutapi.ResolveChangeDevice:
		// A client-side resolution: by Save-time the admin has changed the
		// asset tag. One still carrying it on commit is a stale request.
		res.Status = checkoutapi.PaperRowUnresolved
		res.Field, res.Reason = "resolution", "change the asset tag and save again"
		return s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing)

	case checkoutapi.ResolveDiscardRow:
		res.Status = checkoutapi.PaperRowDiscarded
		return s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing)

	case checkoutapi.ResolveRecordDisputed:
		// Recorded as a claim, permanently badged, never a custody fact:
		// the insert bypasses the exclusion constraint and never touches
		// device status (2.4b's disputed-entry design).
		if err := s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing); err != nil {
			return err
		}
		if err := s.writePaperBorrow(ctx, work, res, touchedDevices, actor, now, true); err != nil {
			return err
		}
		res.Disputed = true
		return nil

	case checkoutapi.ResolveTruncateExisting:
		// "The existing record ended earlier" — correct it to this row's
		// out-time, then record the row normally.
		if err := s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing); err != nil {
			return err
		}
		closed, err := s.deps.Loans.CloseHistoricalAt(ctx, lendingapi.CloseHistoricalParams{
			LoanID: existing.ID, ReturnedAt: work.row.BorrowedAt,
			ReturnActor: actor, ReturnSource: "paper",
			PaperRef: work.paperRef, BackfillNote: work.row.Note,
			RecordedAt: &now, RecordedBy: actor,
		})
		if err != nil {
			switch {
			case errors.Is(err, lendingapi.ErrInvalidReturnTime):
				res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "resolution",
					"the existing record starts after this row's out-time; it cannot be truncated to fit"
				return nil
			case errors.Is(err, lendingapi.ErrAlreadyEndedBefore):
				// Raced: it already ends before the out-time, so nothing
				// was truncated and the borrow below proceeds normally.
			default:
				return fmt.Errorf("checkout: truncate existing loan: %w", err)
			}
		} else {
			if err := events.Publish(ctx, s.pool, events.TopicLoanClosed, loanEventPayload(closed)); err != nil {
				return fmt.Errorf("checkout: publish loan.closed (truncate): %w", err)
			}
			*touchedDevices = append(*touchedDevices, work.device.ID)
			res.Warnings = append(res.Warnings, "the existing record's return was corrected to this row's out-time")
		}
		return s.writePaperBorrow(ctx, work, res, touchedDevices, actor, now, false)

	default:
		res.Status = checkoutapi.PaperRowUnresolved
		res.Field, res.Reason = "resolution", fmt.Sprintf("unknown resolution %q", work.row.Resolution)
		return s.fillExistingLoanView(ctx, &conflict.ExistingLoan, existing)
	}
}

// conflictLoanFrom extracts the existing custody record from either of
// lending's typed custody rejections.
func conflictLoanFrom(err error) (lendingapi.Loan, bool) {
	var overlap *lendingapi.OverlappingCustodyError
	if errors.As(err, &overlap) {
		return overlap.Existing, true
	}
	var taken *lendingapi.DeviceAlreadyOnLoanError
	if errors.As(err, &taken) {
		return taken.Existing, true
	}
	return lendingapi.Loan{}, false
}

// writePaperBorrow inserts the row's loan with full paper provenance
// (INV-14). Each row runs inside its own SAVEPOINT: a custody-constraint
// violation — the race the in-transaction CustodyAt pre-check cannot see —
// is this row's structured conflict, not the transaction's death. disputed
// rows are recorded outside the constraint and never hold the device.
func (s *Service) writePaperBorrow(
	ctx context.Context, work *paperRowWork, res *checkoutapi.PaperRowResult,
	touchedDevices *[]string, actor string, now time.Time, disputed bool,
) error {
	conn := db.Conn(ctx, s.pool)
	savepoint := fmt.Sprintf("paper_row_%d", work.index)
	if _, err := conn.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("checkout: open savepoint: %w", err)
	}

	loan, err := s.deps.Loans.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: work.device.ID, UserID: work.userID,
		Origin:     lendingapi.OriginPaper,
		BorrowedAt: work.row.BorrowedAt, ReturnedAt: work.row.ReturnedAt,
		BorrowActor: actor, BorrowSource: "paper",
		ReturnActor: actor, ReturnSource: "paper",
		PaperRef: work.paperRef, RecordedAt: &now, RecordedBy: actor,
		BackfillNote: work.row.Note, Disputed: disputed,
	})
	if err != nil {
		if _, rbErr := conn.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rbErr != nil {
			return fmt.Errorf("checkout: rollback to savepoint: %w", rbErr)
		}
		// Both custody rejections become the same structured conflict:
		// which of the two constraints reports a raced insert is an index
		// OID accident (see lending.translateLoanErr), not a fact the
		// administrator cares about.
		existing, conflicted := conflictLoanFrom(err)
		if conflicted {
			if existing.ID == "" {
				// The re-read inside lending ran against the pool (the
				// transaction was aborted by the constraint violation), so
				// an intra-batch conflict can come back unidentified — now
				// that the savepoint has restored the transaction, look
				// again.
				if cov, covErr := s.deps.Loans.CustodyAt(ctx, work.device.ID, work.row.BorrowedAt); covErr == nil {
					existing = cov
				}
			}
			res.Status = checkoutapi.PaperRowConflict
			res.Conflict = &checkoutapi.PaperConflict{Type: "overlapping-custody", Resolutions: conflictResolutions}
			return s.fillExistingLoanView(ctx, &res.Conflict.ExistingLoan, existing)
		}
		if errors.Is(err, lendingapi.ErrInvalidReturnTime) {
			res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "returnedAt", "the in-time must be after the out-time"
			return nil
		}
		return fmt.Errorf("checkout: record paper loan: %w", err)
	}
	if _, err := conn.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("checkout: release savepoint: %w", err)
	}

	payload := loanEventPayload(loan)
	if disputed {
		payload["disputed"] = true
	}
	if err := events.Publish(ctx, s.pool, events.TopicLoanOpened, payload); err != nil {
		return fmt.Errorf("checkout: publish loan.opened: %w", err)
	}

	res.Status = checkoutapi.PaperRowOK
	res.LoanID = loan.ID
	res.Disputed = disputed
	if work.row.ReturnedAt == nil && !disputed {
		// An open row puts the device on loan, so the kiosk offers a
		// normal return next time it is scanned (FR-79).
		*touchedDevices = append(*touchedDevices, work.device.ID)
	}
	return nil
}

// writePaperReturn closes the covering loan at the row's in-time — the
// historical close, with provenance only where the loan carries none.
func (s *Service) writePaperReturn(
	ctx context.Context, work *paperRowWork, res *checkoutapi.PaperRowResult,
	touchedDevices *[]string, actor string, now time.Time,
) error {
	existing := *work.covering
	if work.row.ReturnedAt == nil {
		res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "returnedAt",
			"a return needs the in-time"
		return nil
	}

	closed, err := s.deps.Loans.CloseHistoricalAt(ctx, lendingapi.CloseHistoricalParams{
		LoanID: existing.ID, ReturnedAt: *work.row.ReturnedAt,
		ReturnActor: actor, ReturnSource: "paper",
		PaperRef: work.paperRef, BackfillNote: work.row.Note,
		RecordedAt: &now, RecordedBy: actor,
	})
	if err != nil {
		switch {
		case errors.Is(err, lendingapi.ErrAlreadyEndedBefore):
			// The system already ends this loan at or before the paper's
			// in-time: the row adds nothing, and that is worth saying
			// rather than failing a truthful page over.
			res.Status = checkoutapi.PaperRowOK
			res.ClosesLoanID = existing.ID
			res.Warnings = append(res.Warnings, "the system already records this device as returned at or before the in-time")
			return nil
		case errors.Is(err, lendingapi.ErrInvalidReturnTime):
			res.Status, res.Field, res.Reason = checkoutapi.PaperRowUnresolved, "returnedAt",
				"the in-time precedes the existing loan's out-time"
			return nil
		default:
			return fmt.Errorf("checkout: close paper loan: %w", err)
		}
	}

	if err := events.Publish(ctx, s.pool, events.TopicLoanClosed, loanEventPayload(closed)); err != nil {
		return fmt.Errorf("checkout: publish loan.closed: %w", err)
	}
	res.Status = checkoutapi.PaperRowOK
	res.ClosesLoanID = closed.ID
	*touchedDevices = append(*touchedDevices, work.device.ID)
	return nil
}

// fillExistingLoanView renders the conflicting loan's holder for the
// side-by-side comparison: display name and department, never just a UUID.
func (s *Service) fillExistingLoanView(ctx context.Context, view *checkoutapi.PaperExistingLoan, loan lendingapi.Loan) error {
	*view = checkoutapi.PaperExistingLoan{
		ID: loan.ID, BorrowedAt: loan.BorrowedAt, ReturnedAt: loan.ReturnedAt, Origin: string(loan.Origin),
	}
	holder, err := s.deps.Users.LookupUser(ctx, loan.UserID)
	if err != nil {
		return fmt.Errorf("checkout: lookup conflicting holder: %w", err)
	}
	view.UserDisplay = holder.FullName
	if holder.DepartmentID != "" {
		if dept, err := s.deps.Users.LookupDepartment(ctx, holder.DepartmentID); err == nil {
			view.Department = dept.Name
		}
	}
	return nil
}

// reconcileDeviceStatuses aligns each touched device's status with its
// actual custody after the batch: on_loan when an open (undisputed) loan
// holds it, available when nothing does. Only the two custody states are
// ever written here — a device in maintenance or retired keeps its status,
// with the drift left to INV-3 reconciliation rather than paper
// silently overriding an administrator's decision.
func (s *Service) reconcileDeviceStatuses(ctx context.Context, deviceIDs []string, actor string) error {
	seen := make(map[string]struct{})
	for _, id := range deviceIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}

		device, err := s.deps.Devices.LookupDevice(ctx, id)
		if err != nil {
			return fmt.Errorf("checkout: lookup device to reconcile: %w", err)
		}
		open, err := s.deps.Loans.CountOpenByDevice(ctx, id)
		if err != nil {
			return fmt.Errorf("checkout: count open loans to reconcile: %w", err)
		}

		var target catalogapi.DeviceStatus
		switch {
		case open > 0 && device.Status == catalogapi.StatusAvailable:
			target = catalogapi.StatusOnLoan
		case open == 0 && device.Status == catalogapi.StatusOnLoan:
			target = catalogapi.StatusAvailable
		default:
			continue
		}
		if _, err := s.deps.Devices.SetStatus(ctx, id, target, "paper backfill", actor); err != nil {
			return fmt.Errorf("checkout: reconcile device status: %w", err)
		}
		if err := events.Publish(ctx, s.pool, events.TopicDeviceStatusChanged, deviceEventPayload(id, string(target))); err != nil {
			return fmt.Errorf("checkout: publish device.status_changed: %w", err)
		}
	}
	return nil
}
