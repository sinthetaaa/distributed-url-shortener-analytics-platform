package analytics

import (
	"regexp"
	"testing"
)

func TestNewRedirectEvent(t *testing.T) {
	event, err := NewRedirectEvent("3ZB9CeC")
	if err != nil {
		t.Fatalf("NewRedirectEvent returned error: %v", err)
	}

	if event.EventType != RedirectEventType {
		t.Fatalf(
			"expected event type %q, got %q",
			RedirectEventType,
			event.EventType,
		)
	}
	if event.ShortCode != "3ZB9CeC" {
		t.Fatalf(
			"expected short code %q, got %q",
			"3ZB9CeC",
			event.ShortCode,
		)
	}

	if event.OccurredAt.IsZero() {
		t.Fatal("expected occurred_at to be set")
	}

	if event.OccurredAt.Location().String() != "UTC" {
		t.Fatalf(
			"expected occurred_at in UTC, got %s",
			event.OccurredAt.Location(),
		)
	}

	uuidV4Pattern := regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)

	if !uuidV4Pattern.MatchString(event.EventID) {
		t.Fatalf(
			"expected UUIDv4-shaped event id, got %q",
			event.EventID,
		)
	}
}

func TestNewRedirectEventRejectsEmptyShortCode(t *testing.T) {
	if _, err := NewRedirectEvent(""); err == nil {
		t.Fatal("expected empty short code to return an error")
	}
}

func TestNewRedirectEventGeneratesUniqueIDs(t *testing.T) {
	first, err := NewRedirectEvent("same-code")
	if err != nil {
		t.Fatalf("create first event: %v", err)
	}

	second, err := NewRedirectEvent("same-code")
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}
	if first.EventID == second.EventID {
		t.Fatalf(
			"expected distinct event ids, both were %q",
			first.EventID,
		)
	}
}
