package audit

import (
	"context"
	"encoding/json"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/events"
)

// eventTopics is every topic audit subscribes to (2.2.4): audit is a
// write-only sink for the whole system, so it listens to all of them.
var eventTopics = []events.Topic{
	events.TopicLoanOpened,
	events.TopicLoanClosed,
	events.TopicLoanOverdue,
	events.TopicDeviceStatusChanged,
	events.TopicCredentialRevoked,
	events.TopicUserRegistered,
}

// Subscribe registers s on every topic audit cares about. Called once from
// the composition root, after both audit and the event bus are
// constructed.
func (s *Service) Subscribe(bus *events.Bus) {
	for _, topic := range eventTopics {
		bus.Subscribe(topic, s.recordEvent)
	}
}

// recordEvent turns one outbox event into an audit_events row. It runs with
// a plain, transaction-free context (events.Dispatcher's design — see that
// package's doc comment), so Record auto-commits immediately: audit's own
// write does not share a commit boundary with the dispatcher marking the
// event published, which is what makes audit's delivery at-least-once
// rather than exactly-once. That is fine here specifically because
// audit_events is append-only — a redelivered event just adds one more row,
// never a corruption.
func (s *Service) recordEvent(ctx context.Context, ev events.Event) error {
	return s.Record(ctx, auditapi.Event{
		Actor:   "system",
		Action:  string(ev.Topic),
		Subject: subjectForEvent(ev.Topic, ev.Payload),
		Payload: payloadMapForEvent(ev.Payload),
	})
}

// subjectForEvent extracts the primary subject id from a topic's payload
// (docs/phases/phase-2/2.2-events-and-outbox.md's payload table), formatted
// the same way every other audit Subject is: "<kind>:<uuid>".
func subjectForEvent(topic events.Topic, payload []byte) string {
	switch topic {
	case events.TopicLoanOpened, events.TopicLoanClosed, events.TopicLoanOverdue:
		var v struct {
			LoanID string `json:"loanId"`
		}
		_ = json.Unmarshal(payload, &v)
		return "loan:" + v.LoanID
	case events.TopicDeviceStatusChanged:
		var v struct {
			DeviceID string `json:"deviceId"`
		}
		_ = json.Unmarshal(payload, &v)
		return "device:" + v.DeviceID
	case events.TopicCredentialRevoked:
		var v struct {
			CredentialID string `json:"credentialId"`
		}
		_ = json.Unmarshal(payload, &v)
		return "credential:" + v.CredentialID
	case events.TopicUserRegistered:
		var v struct {
			UserID string `json:"userId"`
		}
		_ = json.Unmarshal(payload, &v)
		return "user:" + v.UserID
	default:
		return "event:" + string(topic)
	}
}

func payloadMapForEvent(raw []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}
