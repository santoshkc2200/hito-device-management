// Package notificationapi is the public surface of the notification module. Only this
// package may be imported by other modules; everything under
// internal/modules/notification/internal is unreachable outside the module by
// Go's own visibility rules.
//
// notification provides multi-channel notifications (overdue reminders, administrative
// digests, and return confirmations). Delivery is an outbox consumer guaranteeing
// at-least-once delivery, with deduplication enforced on (loan, escalation step)
// before send attempts to ensure a staff member never receives duplicate reminders
// or messages about already returned equipment.
package notificationapi

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDeliveryNotFound    = errors.New("notification: delivery not found")
	ErrDuplicateDelivery   = errors.New("notification: delivery with dedupe key already exists")
	ErrRecipientOptedOut   = errors.New("notification: recipient opted out")
	ErrLoanDisputed        = errors.New("notification: loan is disputed, notification suppressed")
	ErrLoanAlreadyReturned = errors.New("notification: loan already returned, notification suppressed")
)

// DeliveryStatus tracks each delivery's state across the pipeline.
// Quarantined deliveries (exhausted retries) remain visible to operators
// rather than being silently dropped (Phase 5 visibility convention).
type DeliveryStatus string

const (
	StatusPending          DeliveryStatus = "pending"
	StatusQueuedQuietHours DeliveryStatus = "queued_quiet_hours"
	StatusSent             DeliveryStatus = "sent"
	StatusQuarantined      DeliveryStatus = "quarantined"
	StatusSuppressed       DeliveryStatus = "suppressed"
	StatusFailed           DeliveryStatus = "failed"
)

// Channel represents the communication mechanism.
type Channel string

const (
	ChannelEmail Channel = "email"
)

// Template identifies which message layout to render.
type Template string

const (
	TemplateOverdueReminder    Template = "overdue_reminder"
	TemplateWeeklyDigest       Template = "weekly_digest"
	TemplateReturnConfirmation Template = "return_confirmation"
)

// Delivery represents one delivery record from the delivery_log table.
type Delivery struct {
	ID             string
	Recipient      string
	Channel        Channel
	Template       Template
	DedupeKey      string
	AttemptCount   int
	Status         DeliveryStatus
	LastError      string
	NextAttemptAt  time.Time
	LoanID         string
	UserID         string
	EscalationStep int
	Payload        map[string]any
	CreatedAt      time.Time
	SentAt         *time.Time
	UpdatedAt      time.Time
}

// Preferences holds user-specific communication settings.
// Managed exclusively by administrators (staff have no self-service login in Phase 6).
type Preferences struct {
	UserID    string
	Channel   Channel
	OptedOut  bool
	UpdatedAt time.Time
	UpdatedBy string
}

// SetPreferencesParams specifies settings to update.
type SetPreferencesParams struct {
	UserID    string
	Channel   Channel
	OptedOut  bool
	UpdatedBy string
}

// EnqueueParams carries the data needed to schedule a notification.
type EnqueueParams struct {
	Recipient      string
	Channel        Channel
	Template       Template
	DedupeKey      string
	LoanID         string
	UserID         string
	EscalationStep int
	Payload        map[string]any
}

// ListDeliveriesParams allows filtering delivery log records.
type ListDeliveriesParams struct {
	Status DeliveryStatus
	Limit  int
	Offset int
}

// Service defines the public API of the notification module.
type Service interface {
	// Enqueue creates a delivery_log record. If quiet hours are active, the message
	// is scheduled for the window opening rather than dropped. If a record with
	// the same dedupe key exists, ErrDuplicateDelivery is returned.
	Enqueue(ctx context.Context, params EnqueueParams) (Delivery, error)

	// GetDelivery retrieves a delivery log record by its UUID.
	GetDelivery(ctx context.Context, id string) (Delivery, error)

	// ListDeliveries returns a slice of delivery log records matching the filter.
	ListDeliveries(ctx context.Context, params ListDeliveriesParams) ([]Delivery, error)

	// GetQuarantinedDeliveries returns all deliveries that have exhausted retries,
	// ensuring failed sends remain visible in the admin console.
	GetQuarantinedDeliveries(ctx context.Context) ([]Delivery, error)

	// GetPreferences looks up per-recipient preferences and opt-out status.
	GetPreferences(ctx context.Context, userID string) (Preferences, error)

	// SetPreferences updates per-recipient preferences and opt-out status.
	SetPreferences(ctx context.Context, params SetPreferencesParams) (Preferences, error)

	// ProcessPendingDeliveries attempts delivery of all pending and quiet-hours-released
	// items whose next_attempt_at has passed. It enforces send-time state verification:
	// loans returned or disputed while queued are suppressed before any email is sent.
	ProcessPendingDeliveries(ctx context.Context, limit int) (int, error)

	// RemindLoan manually triggers an overdue reminder for an open loan through the same
	// delivery pipeline, subject to quiet hours, opt-out, deduplication, and suppression.
	RemindLoan(ctx context.Context, loanID string) (RemindOutcome, error)
}

// RemindOutcomeStatus categorizes the result of a manual reminder request (6.2e).
type RemindOutcomeStatus string

const (
	RemindOutcomeSent             RemindOutcomeStatus = "sent"
	RemindOutcomeQueuedQuietHours RemindOutcomeStatus = "queued_quiet_hours"
	RemindOutcomeRefused          RemindOutcomeStatus = "refused"
)

// RemindOutcome carries the result reported to the administrator.
type RemindOutcome struct {
	Outcome    RemindOutcomeStatus
	Reason     string
	DeliveryID string
}
