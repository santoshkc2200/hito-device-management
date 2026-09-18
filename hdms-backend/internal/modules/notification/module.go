package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/notification/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/modules/notification/templates"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Config holds runtime configuration for the notification module.
type Config struct {
	Transport             Transport
	QuietHours            QuietHoursConfig
	HumanContact          string
	DefaultReturnLocation string
	MaxAttempts           int
	Logger                *slog.Logger
}

// Service implements notificationapi.Service.
type Service struct {
	pool   *db.Pool
	clock  clock.Clock
	cfg    Config
	logger *slog.Logger
}

// New constructs the notification service.
func New(pool *db.Pool, clk clock.Clock, cfg Config) *Service {
	if clk == nil {
		clk = clock.System{}
	}
	if cfg.Transport == nil {
		cfg.Transport = NewSMTPTransport(SMTPConfig{})
	}
	if cfg.HumanContact == "" {
		cfg.HumanContact = "Equipment Counter, Ext. 4100 (counter@hospital.local)"
	}
	if cfg.DefaultReturnLocation == "" {
		cfg.DefaultReturnLocation = "Ward 4 Equipment Counter (Building B, 2F)"
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		pool:   pool,
		clock:  clk,
		cfg:    cfg,
		logger: cfg.Logger,
	}
}

var _ notificationapi.Service = (*Service)(nil)

// Subscribe wires the notification service as an outbox consumer.
// Consumes loan.overdue with module-level retry and quarantine — a relay
// outage never stalls the shared outbox for other modules.
func (s *Service) Subscribe(bus *events.Bus) {
	bus.Subscribe(events.TopicLoanOverdue, s.handleLoanOverdue)
}

func (s *Service) handleLoanOverdue(ctx context.Context, ev events.Event) error {
	var payload struct {
		LoanID         string    `json:"loanId"`
		DeviceID       string    `json:"deviceId"`
		UserID         string    `json:"userId"`
		DueAt          time.Time `json:"dueAt"`
		BorrowerEmail  string    `json:"borrowerEmail"`
		BorrowerName   string    `json:"borrowerName"`
		DeviceName     string    `json:"deviceName"`
		DeviceAssetTag string    `json:"deviceAssetTag"`
		EscalationStep int       `json:"escalationStep"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		s.logger.Error("notification: unmarshal loan.overdue payload", "error", err)
		return nil // Drop malformed event; outbox shouldn't retry bad json forever
	}

	if payload.EscalationStep <= 0 {
		payload.EscalationStep = 1
	}

	step := payload.EscalationStep
	dedupeKey := fmt.Sprintf("loan:%s:step:%d", payload.LoanID, step)

	// Check per-user preference and opt-out
	pref, err := s.GetPreferences(ctx, payload.UserID)
	if err == nil && pref.OptedOut {
		s.logger.Info("notification: recipient opted out, skipping", "userId", payload.UserID, "loanId", payload.LoanID)
		// Record as suppressed so dedupe is preserved and silence is auditable
		_, _ = s.recordDelivery(ctx, notificationapi.EnqueueParams{
			Recipient:      payload.BorrowerEmail,
			Channel:        notificationapi.ChannelEmail,
			Template:       notificationapi.TemplateOverdueReminder,
			DedupeKey:      dedupeKey,
			LoanID:         payload.LoanID,
			UserID:         payload.UserID,
			EscalationStep: step,
			Payload:        map[string]any{"optedOut": true},
		}, notificationapi.StatusSuppressed, "recipient opted out", s.clock.Now())
		return nil
	}

	now := s.clock.Now()
	var initialStatus notificationapi.DeliveryStatus
	var nextAttempt time.Time

	if s.cfg.QuietHours.IsQuiet(now) {
		initialStatus = notificationapi.StatusQueuedQuietHours
		nextAttempt = s.cfg.QuietHours.NextWindowOpen(now)
	} else {
		initialStatus = notificationapi.StatusPending
		nextAttempt = now
	}

	rawPayload := map[string]any{
		"borrowerName":   payload.BorrowerName,
		"borrowerEmail":  payload.BorrowerEmail,
		"deviceName":     payload.DeviceName,
		"deviceAssetTag": payload.DeviceAssetTag,
		"dueAt":          payload.DueAt,
		"escalationStep": step,
	}

	delivery, err := s.recordDelivery(ctx, notificationapi.EnqueueParams{
		Recipient:      payload.BorrowerEmail,
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      dedupeKey,
		LoanID:         payload.LoanID,
		UserID:         payload.UserID,
		EscalationStep: step,
		Payload:        rawPayload,
	}, initialStatus, "", nextAttempt)

	if err != nil {
		if errors.Is(err, notificationapi.ErrDuplicateDelivery) {
			// Already enqueued / sent for this (loan, step)
			return nil
		}
		s.logger.Error("notification: failed to record delivery", "error", err, "dedupeKey", dedupeKey)
		return nil
	}

	// If pending and ready now, attempt immediate delivery
	if initialStatus == notificationapi.StatusPending {
		_ = s.deliverOne(ctx, delivery)
	}

	return nil
}

func (s *Service) recordDelivery(
	ctx context.Context,
	params notificationapi.EnqueueParams,
	status notificationapi.DeliveryStatus,
	lastErr string,
	nextAttemptAt time.Time,
) (notificationstore.DeliveryLog, error) {
	q := notificationstore.New(db.Conn(ctx, s.pool))

	loanUUID := pgtype.UUID{}
	if params.LoanID != "" {
		if id, err := pgtypeconv.UUID(params.LoanID); err == nil {
			loanUUID = id
		}
	}
	userUUID := pgtype.UUID{}
	if params.UserID != "" {
		if id, err := pgtypeconv.UUID(params.UserID); err == nil {
			userUUID = id
		}
	}

	sanitizedPayload := SanitizePayload(params.Payload)
	rawPayload, err := json.Marshal(sanitizedPayload)
	if err != nil {
		rawPayload = []byte("{}")
	}

	now := s.clock.Now()
	deliveryID := pgtypeconv.NewUUID()

	var step pgtype.Int4
	if params.EscalationStep > 0 {
		step = pgtype.Int4{Int32: int32(params.EscalationStep), Valid: true}
	}

	channel := string(params.Channel)
	if channel == "" {
		channel = string(notificationapi.ChannelEmail)
	}

	row, err := q.InsertDelivery(ctx, notificationstore.InsertDeliveryParams{
		ID:             deliveryID,
		Recipient:      SanitizeText(params.Recipient),
		Channel:        channel,
		Template:       string(params.Template),
		DedupeKey:      params.DedupeKey,
		AttemptCount:   0,
		Status:         string(status),
		LastError:      pgtypeconv.Text(SanitizeText(lastErr)),
		NextAttemptAt:  pgtypeconv.Timestamptz(nextAttemptAt),
		LoanID:         loanUUID,
		UserID:         userUUID,
		EscalationStep: step,
		Payload:        rawPayload,
		CreatedAt:      pgtypeconv.Timestamptz(now),
		SentAt:         pgtype.Timestamptz{},
		UpdatedAt:      pgtypeconv.Timestamptz(now),
	})

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return notificationstore.DeliveryLog{}, notificationapi.ErrDuplicateDelivery
		}
		return notificationstore.DeliveryLog{}, fmt.Errorf("notification: insert delivery: %w", err)
	}

	return row, nil
}

// deliverOne performs send-time state verification and dispatches the notification.
func (s *Service) deliverOne(ctx context.Context, d notificationstore.DeliveryLog) error {
	q := notificationstore.New(db.Conn(ctx, s.pool))
	now := s.clock.Now()

	// ═══ SEND-TIME RE-CHECK (6.2d suppression rule) ═══
	// A loan returned or disputed while queued is suppressed before sending.
	var borrowerName, deviceName, assetTag string
	var borrowedAt, dueAt time.Time
	recipient := d.Recipient

	if d.LoanID.Valid {
		loanRow, err := q.GetLoanForNotification(ctx, d.LoanID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.logger.Warn("notification: loan row disappeared, suppressing", "loanId", pgtypeconv.UUIDString(d.LoanID))
				_, _ = q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
					ID:            d.ID,
					Status:        string(notificationapi.StatusSuppressed),
					AttemptCount:  d.AttemptCount,
					LastError:     pgtypeconv.Text("loan not found"),
					NextAttemptAt: d.NextAttemptAt,
					SentAt:        pgtype.Timestamptz{},
					UpdatedAt:     pgtypeconv.Timestamptz(now),
				})
				return nil
			}
			return fmt.Errorf("notification: check loan status: %w", err)
		}

		// Disputed loans are never notified
		if loanRow.Disputed {
			s.logger.Info("notification: loan is disputed, suppressing send", "loanId", pgtypeconv.UUIDString(d.LoanID))
			_, _ = q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
				ID:            d.ID,
				Status:        string(notificationapi.StatusSuppressed),
				AttemptCount:  d.AttemptCount,
				LastError:     pgtypeconv.Text("loan is disputed"),
				NextAttemptAt: d.NextAttemptAt,
				SentAt:        pgtype.Timestamptz{},
				UpdatedAt:     pgtypeconv.Timestamptz(now),
			})
			return nil
		}

		// Returned loans must never be notified
		if loanRow.Status != "open" {
			s.logger.Info("notification: loan is no longer open, suppressing send", "loanId", pgtypeconv.UUIDString(d.LoanID), "status", loanRow.Status)
			_, _ = q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
				ID:            d.ID,
				Status:        string(notificationapi.StatusSuppressed),
				AttemptCount:  d.AttemptCount,
				LastError:     pgtypeconv.Text(fmt.Sprintf("loan is %s", loanRow.Status)),
				NextAttemptAt: d.NextAttemptAt,
				SentAt:        pgtype.Timestamptz{},
				UpdatedAt:     pgtypeconv.Timestamptz(now),
			})
			return nil
		}

		borrowerName = loanRow.BorrowerName
		if loanRow.BorrowerEmail.Valid && loanRow.BorrowerEmail.String != "" {
			recipient = loanRow.BorrowerEmail.String
		}
		deviceName = loanRow.DeviceName
		assetTag = loanRow.DeviceAssetTag
		borrowedAt = pgtypeconv.Time(loanRow.BorrowedAt)
		if loanRow.DueAt.Valid {
			dueAt = pgtypeconv.Time(loanRow.DueAt)
		}
	}

	// Render templates based on d.Template
	var subject, textBody, htmlBody string
	var renderErr error

	switch notificationapi.Template(d.Template) {
	case notificationapi.TemplateOverdueReminder:
		overdueDays := int(math.Ceil(now.Sub(dueAt).Hours() / 24.0))
		if overdueDays < 1 {
			overdueDays = 1
		}
		subject, textBody, htmlBody, renderErr = templates.RenderOverdueReminder(templates.OverdueReminderData{
			BorrowerName:   borrowerName,
			DeviceName:     deviceName,
			AssetTag:       assetTag,
			BorrowedAt:     borrowedAt.Format("2006-01-02 15:04"),
			DueAt:          dueAt.Format("2006-01-02 15:04"),
			OverdueDays:    overdueDays,
			ReturnLocation: s.cfg.DefaultReturnLocation,
			HumanContact:   s.cfg.HumanContact,
		})
	case notificationapi.TemplateReturnConfirmation:
		subject, textBody, htmlBody, renderErr = templates.RenderReturnConfirmation(templates.ReturnConfirmationData{
			BorrowerName: borrowerName,
			DeviceName:   deviceName,
			AssetTag:     assetTag,
			ReturnedAt:   now.Format("2006-01-02 15:04"),
			HumanContact: s.cfg.HumanContact,
		})
	case notificationapi.TemplateWeeklyDigest:
		var payload map[string]any
		_ = json.Unmarshal(d.Payload, &payload)
		var items []templates.OverdueDigestItem
		if rawItems, ok := payload["items"].([]any); ok {
			for _, it := range rawItems {
				if m, ok := it.(map[string]any); ok {
					item := templates.OverdueDigestItem{}
					if tag, ok := m["assetTag"].(string); ok {
						item.AssetTag = tag
					}
					if name, ok := m["deviceName"].(string); ok {
						item.DeviceName = name
					}
					if bName, ok := m["borrowerName"].(string); ok {
						item.BorrowerName = bName
					}
					if due, ok := m["dueAt"].(string); ok {
						item.DueAt = due
					}
					if days, ok := m["overdueDays"].(float64); ok {
						item.OverdueDays = int(days)
					}
					items = append(items, item)
				}
			}
		}
		totalOverdue := len(items)
		if tVal, ok := payload["totalOverdue"].(float64); ok {
			totalOverdue = int(tVal)
		}
		genAt := now.Format("2006-01-02 15:04")
		if gVal, ok := payload["generatedAt"].(string); ok && gVal != "" {
			genAt = gVal
		}
		subject, textBody, htmlBody, renderErr = templates.RenderWeeklyDigest(templates.WeeklyDigestData{
			GeneratedAt:  genAt,
			TotalOverdue: totalOverdue,
			Items:        items,
			HumanContact: s.cfg.HumanContact,
		})
	case notificationapi.TemplateReservationExpired:
		var payload map[string]any
		_ = json.Unmarshal(d.Payload, &payload)
		userName, _ := payload["userName"].(string)
		deviceName, _ := payload["deviceName"].(string)
		assetTag, _ := payload["deviceAssetTag"].(string)
		startAt, _ := payload["startAt"].(string)
		if userName == "" {
			userName = "Staff Member"
		}
		subject = fmt.Sprintf("[HITO HOSPITAL] Reservation Expired: %s (%s)", deviceName, assetTag)
		textBody = fmt.Sprintf("Hello %s,\n\nYour reservation for %s (%s) starting at %s has expired and been cancelled because the device was not collected within the grace period.\n\nIf you still need this equipment, please make a new reservation or visit the counter.\n\nContact: %s\n",
			userName, deviceName, assetTag, startAt, s.cfg.HumanContact)
		htmlBody = fmt.Sprintf("<p>Hello %s,</p><p>Your reservation for <strong>%s</strong> (%s) starting at %s has expired and been cancelled because the device was not collected within the grace period.</p><p>If you still need this equipment, please make a new reservation or visit the counter.</p><p>Contact: %s</p>",
			userName, deviceName, assetTag, startAt, s.cfg.HumanContact)
	default:
		subject = fmt.Sprintf("[HITO HOSPITAL] Notification: %s", d.Template)
		textBody = fmt.Sprintf("Notification for recipient %s (template: %s)", recipient, d.Template)
		htmlBody = fmt.Sprintf("<p>%s</p>", textBody)
	}

	if renderErr != nil {
		s.logger.Error("notification: render error", "error", renderErr, "deliveryId", pgtypeconv.UUIDString(d.ID))
		return renderErr
	}

	// Attempt delivery through transport
	sendErr := s.cfg.Transport.Send(ctx, Message{
		To:       recipient,
		Subject:  SanitizeText(subject),
		TextBody: SanitizeText(textBody),
		HTMLBody: SanitizeText(htmlBody),
		ReplyTo:  s.cfg.HumanContact,
	})

	if sendErr == nil {
		// Delivery succeeded
		_, err := q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
			ID:            d.ID,
			Status:        string(notificationapi.StatusSent),
			AttemptCount:  d.AttemptCount + 1,
			LastError:     pgtypeconv.Text(""),
			NextAttemptAt: d.NextAttemptAt,
			SentAt:        pgtypeconv.Timestamptz(now),
			UpdatedAt:     pgtypeconv.Timestamptz(now),
		})
		observability.IncNotificationDelivery(d.Channel, d.Template, "sent")
		return err
	}

	// Delivery failed — execute retry & quarantine logic
	attempts := d.AttemptCount + 1
	sanitizedErr := SanitizeText(sendErr.Error())

	if int(attempts) >= s.cfg.MaxAttempts {
		// Quarantined: exhausted retries, remains visible in console
		s.logger.Error("notification: delivery quarantined after max retries",
			"deliveryId", pgtypeconv.UUIDString(d.ID),
			"attempts", attempts,
			"error", sanitizedErr)

		_, err := q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
			ID:            d.ID,
			Status:        string(notificationapi.StatusQuarantined),
			AttemptCount:  attempts,
			LastError:     pgtypeconv.Text(sanitizedErr),
			NextAttemptAt: d.NextAttemptAt,
			SentAt:        pgtype.Timestamptz{},
			UpdatedAt:     pgtypeconv.Timestamptz(now),
		})
		observability.IncNotificationDelivery(d.Channel, d.Template, "quarantined")
		return err
	}

	// Schedule next attempt with exponential backoff (e.g. 1m, 2m, 4m...)
	backoff := time.Duration(1<<attempts) * time.Minute
	nextAttempt := now.Add(backoff)

	_, err := q.UpdateDeliveryStatus(ctx, notificationstore.UpdateDeliveryStatusParams{
		ID:            d.ID,
		Status:        string(notificationapi.StatusPending),
		AttemptCount:  attempts,
		LastError:     pgtypeconv.Text(sanitizedErr),
		NextAttemptAt: pgtypeconv.Timestamptz(nextAttempt),
		SentAt:        pgtype.Timestamptz{},
		UpdatedAt:     pgtypeconv.Timestamptz(now),
	})
	observability.IncNotificationDelivery(d.Channel, d.Template, "retry")
	return err
}

// ProcessPendingDeliveries scans for ready pending or released quiet-hours deliveries and sends them.
func (s *Service) ProcessPendingDeliveries(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	q := notificationstore.New(db.Conn(ctx, s.pool))
	now := s.clock.Now()

	rows, err := q.ListPendingDeliveries(ctx, notificationstore.ListPendingDeliveriesParams{
		NextAttemptAt: pgtypeconv.Timestamptz(now),
		Limit:         int32(limit),
	})
	if err != nil {
		return 0, fmt.Errorf("notification: list pending deliveries: %w", err)
	}

	count := 0
	for _, row := range rows {
		// If item was queued for quiet hours, verify quiet hours are now over
		if row.Status == string(notificationapi.StatusQueuedQuietHours) && s.cfg.QuietHours.IsQuiet(now) {
			continue
		}
		if err := s.deliverOne(ctx, row); err == nil {
			count++
		}
	}
	return count, nil
}

// Enqueue inserts a new delivery into the log.
func (s *Service) Enqueue(ctx context.Context, params notificationapi.EnqueueParams) (notificationapi.Delivery, error) {
	now := s.clock.Now()
	var initialStatus notificationapi.DeliveryStatus
	var nextAttempt time.Time

	if s.cfg.QuietHours.IsQuiet(now) {
		initialStatus = notificationapi.StatusQueuedQuietHours
		nextAttempt = s.cfg.QuietHours.NextWindowOpen(now)
	} else {
		initialStatus = notificationapi.StatusPending
		nextAttempt = now
	}

	row, err := s.recordDelivery(ctx, params, initialStatus, "", nextAttempt)
	if err != nil {
		return notificationapi.Delivery{}, err
	}

	return toAPIDelivery(row), nil
}

// GetDelivery retrieves a delivery record by ID.
func (s *Service) GetDelivery(ctx context.Context, id string) (notificationapi.Delivery, error) {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return notificationapi.Delivery{}, fmt.Errorf("notification: invalid id: %w", err)
	}
	q := notificationstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetDelivery(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notificationapi.Delivery{}, notificationapi.ErrDeliveryNotFound
		}
		return notificationapi.Delivery{}, err
	}
	return toAPIDelivery(row), nil
}

// ListDeliveries retrieves delivery log records matching the filter.
func (s *Service) ListDeliveries(ctx context.Context, params notificationapi.ListDeliveriesParams) ([]notificationapi.Delivery, error) {
	q := notificationstore.New(db.Conn(ctx, s.pool))
	limit := int32(50)
	if params.Limit > 0 {
		limit = int32(params.Limit)
	}
	offset := int32(params.Offset)

	var statusFilter pgtype.Text
	if params.Status != "" {
		statusFilter = pgtypeconv.Text(string(params.Status))
	}

	rows, err := q.ListDeliveries(ctx, notificationstore.ListDeliveriesParams{
		Status: statusFilter,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]notificationapi.Delivery, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAPIDelivery(r))
	}
	return items, nil
}

// GetQuarantinedDeliveries returns all deliveries in quarantined status.
func (s *Service) GetQuarantinedDeliveries(ctx context.Context) ([]notificationapi.Delivery, error) {
	q := notificationstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListQuarantinedDeliveries(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]notificationapi.Delivery, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAPIDelivery(r))
	}
	return items, nil
}

// GetPreferences fetches recipient communication preferences.
func (s *Service) GetPreferences(ctx context.Context, userID string) (notificationapi.Preferences, error) {
	uid, err := pgtypeconv.UUID(userID)
	if err != nil {
		return notificationapi.Preferences{}, fmt.Errorf("notification: invalid user id: %w", err)
	}
	q := notificationstore.New(db.Conn(ctx, s.pool))
	pref, err := q.GetPreferences(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Default preferences: email channel, not opted out
			return notificationapi.Preferences{
				UserID:    userID,
				Channel:   notificationapi.ChannelEmail,
				OptedOut:  false,
				UpdatedAt: s.clock.Now(),
				UpdatedBy: "system",
			}, nil
		}
		return notificationapi.Preferences{}, err
	}
	return notificationapi.Preferences{
		UserID:    pgtypeconv.UUIDString(pref.UserID),
		Channel:   notificationapi.Channel(pref.Channel),
		OptedOut:  pref.OptedOut,
		UpdatedAt: pgtypeconv.Time(pref.UpdatedAt),
		UpdatedBy: pref.UpdatedBy,
	}, nil
}

// SetPreferences creates or updates recipient communication preferences.
func (s *Service) SetPreferences(ctx context.Context, params notificationapi.SetPreferencesParams) (notificationapi.Preferences, error) {
	uid, err := pgtypeconv.UUID(params.UserID)
	if err != nil {
		return notificationapi.Preferences{}, fmt.Errorf("notification: invalid user id: %w", err)
	}
	q := notificationstore.New(db.Conn(ctx, s.pool))
	now := s.clock.Now()
	channel := string(params.Channel)
	if channel == "" {
		channel = string(notificationapi.ChannelEmail)
	}
	updatedBy := params.UpdatedBy
	if updatedBy == "" {
		updatedBy = "admin"
	}

	pref, err := q.UpsertPreferences(ctx, notificationstore.UpsertPreferencesParams{
		UserID:    uid,
		Channel:   channel,
		OptedOut:  params.OptedOut,
		UpdatedAt: pgtypeconv.Timestamptz(now),
		UpdatedBy: updatedBy,
	})
	if err != nil {
		return notificationapi.Preferences{}, err
	}

	return notificationapi.Preferences{
		UserID:    pgtypeconv.UUIDString(pref.UserID),
		Channel:   notificationapi.Channel(pref.Channel),
		OptedOut:  pref.OptedOut,
		UpdatedAt: pgtypeconv.Time(pref.UpdatedAt),
		UpdatedBy: pref.UpdatedBy,
	}, nil
}

func toAPIDelivery(r notificationstore.DeliveryLog) notificationapi.Delivery {
	var payload map[string]any
	_ = json.Unmarshal(r.Payload, &payload)

	var sentAt *time.Time
	if r.SentAt.Valid {
		t := pgtypeconv.Time(r.SentAt)
		sentAt = &t
	}

	step := 0
	if r.EscalationStep.Valid {
		step = int(r.EscalationStep.Int32)
	}

	return notificationapi.Delivery{
		ID:             pgtypeconv.UUIDString(r.ID),
		Recipient:      r.Recipient,
		Channel:        notificationapi.Channel(r.Channel),
		Template:       notificationapi.Template(r.Template),
		DedupeKey:      r.DedupeKey,
		AttemptCount:   int(r.AttemptCount),
		Status:         notificationapi.DeliveryStatus(r.Status),
		LastError:      r.LastError.String,
		NextAttemptAt:  pgtypeconv.Time(r.NextAttemptAt),
		LoanID:         pgtypeconv.UUIDString(r.LoanID),
		UserID:         pgtypeconv.UUIDString(r.UserID),
		EscalationStep: step,
		Payload:        payload,
		CreatedAt:      pgtypeconv.Time(r.CreatedAt),
		SentAt:         sentAt,
		UpdatedAt:      pgtypeconv.Time(r.UpdatedAt),
	}
}

// RemindLoan manually triggers an overdue reminder for an open loan through the same
// delivery pipeline, subject to quiet hours, opt-out, deduplication, and suppression (6.2e).
func (s *Service) RemindLoan(ctx context.Context, loanID string) (notificationapi.RemindOutcome, error) {
	uid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return notificationapi.RemindOutcome{}, fmt.Errorf("notification: invalid loan id: %w", err)
	}

	q := notificationstore.New(db.Conn(ctx, s.pool))
	loanRow, err := q.GetLoanForNotification(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notificationapi.RemindOutcome{}, notificationapi.ErrDeliveryNotFound
		}
		return notificationapi.RemindOutcome{}, fmt.Errorf("notification: get loan: %w", err)
	}

	if loanRow.Status != "open" {
		return notificationapi.RemindOutcome{
			Outcome: notificationapi.RemindOutcomeRefused,
			Reason:  fmt.Sprintf("loan is %s, not open", loanRow.Status),
		}, nil
	}

	if loanRow.Disputed {
		return notificationapi.RemindOutcome{
			Outcome: notificationapi.RemindOutcomeRefused,
			Reason:  "loan is disputed, notifications suppressed",
		}, nil
	}

	now := s.clock.Now()
	if !loanRow.DueAt.Valid || !now.After(pgtypeconv.Time(loanRow.DueAt)) {
		return notificationapi.RemindOutcome{
			Outcome: notificationapi.RemindOutcomeRefused,
			Reason:  "loan is not overdue",
		}, nil
	}

	recipientEmail := ""
	if loanRow.BorrowerEmail.Valid {
		recipientEmail = loanRow.BorrowerEmail.String
	}
	if recipientEmail == "" {
		return notificationapi.RemindOutcome{
			Outcome: notificationapi.RemindOutcomeRefused,
			Reason:  "borrower has no email address",
		}, nil
	}

	overdueDuration := now.Sub(pgtypeconv.Time(loanRow.DueAt))
	step := escalationStepFor(overdueDuration)
	overdueDays := int(math.Ceil(overdueDuration.Hours() / 24.0))
	if overdueDays < 1 {
		overdueDays = 1
	}

	payload := map[string]any{
		"loanId":          loanID,
		"deviceId":        pgtypeconv.UUIDString(loanRow.DeviceID),
		"userId":          pgtypeconv.UUIDString(loanRow.UserID),
		"borrowerName":    loanRow.BorrowerName,
		"borrowerEmail":   recipientEmail,
		"deviceName":      loanRow.DeviceName,
		"deviceAssetTag":  loanRow.DeviceAssetTag,
		"dueAt":           pgtypeconv.Time(loanRow.DueAt),
		"overdueDuration": overdueDuration.String(),
		"overdueDays":     overdueDays,
		"escalationStep":  step,
	}

	dedupeKey := fmt.Sprintf("loan:%s:step:%d", loanID, step)

	// Check user preferences / opt-out
	pref, err := s.GetPreferences(ctx, pgtypeconv.UUIDString(loanRow.UserID))
	if err == nil && pref.OptedOut {
		params := notificationapi.EnqueueParams{
			Recipient:      recipientEmail,
			Channel:        notificationapi.ChannelEmail,
			Template:       notificationapi.TemplateOverdueReminder,
			DedupeKey:      dedupeKey,
			LoanID:         loanID,
			UserID:         pgtypeconv.UUIDString(loanRow.UserID),
			EscalationStep: step,
			Payload:        payload,
		}
		suppressedRow, _ := s.recordDelivery(ctx, params, notificationapi.StatusSuppressed, "recipient has opted out of notifications", now)
		deliveryID := ""
		if suppressedRow.ID.Valid {
			deliveryID = pgtypeconv.UUIDString(suppressedRow.ID)
		}
		return notificationapi.RemindOutcome{
			Outcome:    notificationapi.RemindOutcomeRefused,
			Reason:     "recipient has opted out of notifications",
			DeliveryID: deliveryID,
		}, nil
	}

	params := notificationapi.EnqueueParams{
		Recipient:      recipientEmail,
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      dedupeKey,
		LoanID:         loanID,
		UserID:         pgtypeconv.UUIDString(loanRow.UserID),
		EscalationStep: step,
		Payload:        payload,
	}

	isQuiet := s.cfg.QuietHours.IsQuiet(now)
	var initialStatus notificationapi.DeliveryStatus
	var nextAttempt time.Time
	if isQuiet {
		initialStatus = notificationapi.StatusQueuedQuietHours
		nextAttempt = s.cfg.QuietHours.NextWindowOpen(now)
	} else {
		initialStatus = notificationapi.StatusPending
		nextAttempt = now
	}

	deliveryRow, err := s.recordDelivery(ctx, params, initialStatus, "", nextAttempt)
	if err != nil {
		if errors.Is(err, notificationapi.ErrDuplicateDelivery) {
			return notificationapi.RemindOutcome{
				Outcome: notificationapi.RemindOutcomeRefused,
				Reason:  fmt.Sprintf("reminder for escalation step %d already exists", step),
			}, nil
		}
		return notificationapi.RemindOutcome{}, fmt.Errorf("notification: enqueue reminder: %w", err)
	}

	// Record escalation step to keep overdue_escalations in sync with manual reminders
	_, _ = q.RecordOverdueEscalation(ctx, notificationstore.RecordOverdueEscalationParams{
		LoanID:         loanRow.ID,
		EscalationStep: int32(step),
		PublishedAt:    pgtypeconv.Timestamptz(now),
	})

	deliveryID := pgtypeconv.UUIDString(deliveryRow.ID)

	if isQuiet {
		return notificationapi.RemindOutcome{
			Outcome:    notificationapi.RemindOutcomeQueuedQuietHours,
			Reason:     "reminder queued for delivery after quiet hours",
			DeliveryID: deliveryID,
		}, nil
	}

	// Not in quiet hours: dispatch immediately
	if err := s.deliverOne(ctx, deliveryRow); err != nil {
		s.logger.Error("notification: manual reminder delivery attempt failed", "error", err, "deliveryId", deliveryID)
		// Even if transport fails initially, the delivery record exists in pending/quarantined status
	}

	// Re-check status from DB to return accurate outcome (sent vs suppressed vs pending)
	updatedRow, err := q.GetDelivery(ctx, deliveryRow.ID)
	if err == nil {
		if updatedRow.Status == string(notificationapi.StatusSuppressed) {
			return notificationapi.RemindOutcome{
				Outcome:    notificationapi.RemindOutcomeRefused,
				Reason:     updatedRow.LastError.String,
				DeliveryID: deliveryID,
			}, nil
		}
	}

	return notificationapi.RemindOutcome{
		Outcome:    notificationapi.RemindOutcomeSent,
		DeliveryID: deliveryID,
	}, nil
}

func escalationStepFor(d time.Duration) int {
	switch {
	case d >= 7*24*time.Hour:
		return 3
	case d >= 3*24*time.Hour:
		return 2
	default:
		return 1
	}
}
