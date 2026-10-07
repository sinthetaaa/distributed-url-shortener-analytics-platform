package analytics

import (
	"crypto/rand"
	"fmt"
	"time"
)

const (
	RedirectEventType   = "redirect"
	RedirectEventsTopic = "shortscale.redirect-events.v1"
)

type RedirectEvent struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	ShortCode  string    `json:"short_code"`
	OccurredAt time.Time `json:"occurred_at"`
}

func NewRedirectEvent(shortCode string) (RedirectEvent, error) {
	if shortCode == "" {
		return RedirectEvent{}, fmt.Errorf("short code must not be empty")
	}

	eventID, err := newEventID()
	if err != nil {
		return RedirectEvent{}, err
	}

	return RedirectEvent{
		EventID:    eventID,
		EventType:  RedirectEventType,
		ShortCode:  shortCode,
		OccurredAt: time.Now().UTC(),
	}, nil
}

func newEventID() (string, error) {
	var id [16]byte

	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		id[0:4],
		id[4:6],
		id[6:8],
		id[8:10],
		id[10:16],
	), nil
}
