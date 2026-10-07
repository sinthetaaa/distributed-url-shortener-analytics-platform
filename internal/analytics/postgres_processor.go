package analytics

import (
	"context"
	"fmt"
	"strings"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

type redirectEventStore interface {
	InsertRedirectEvent(
		context.Context,
		database.InsertRedirectEventParams,
	) (int64, error)
}

type PostgresRedirectEventProcessor struct {
	store redirectEventStore
}

func NewPostgresRedirectEventProcessor(
	store redirectEventStore,
) (*PostgresRedirectEventProcessor, error) {
	if store == nil {
		return nil, fmt.Errorf(
			"redirect event store must not be nil",
		)
	}

	return &PostgresRedirectEventProcessor{
		store: store,
	}, nil
}

func (p *PostgresRedirectEventProcessor) Process(
	ctx context.Context,
	event RedirectEvent,
) error {
	if strings.TrimSpace(event.EventID) == "" {
		return fmt.Errorf("redirect event id must not be empty")
	}

	if event.EventType != RedirectEventType {
		return fmt.Errorf(
			"redirect event type must be %q",
			RedirectEventType,
		)
	}

	if strings.TrimSpace(event.ShortCode) == "" {
		return fmt.Errorf(
			"redirect event short code must not be empty",
		)
	}

	if event.OccurredAt.IsZero() {
		return fmt.Errorf(
			"redirect event occurred_at must not be zero",
		)
	}

	rowsAffected, err := p.store.InsertRedirectEvent(
		ctx,
		database.InsertRedirectEventParams{
			EventID:   event.EventID,
			EventType: event.EventType,
			ShortCode: event.ShortCode,
			OccurredAt: pgtype.Timestamptz{
				Time:  event.OccurredAt.UTC(),
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"persist redirect event %q: %w",
			event.EventID,
			err,
		)
	}

	switch rowsAffected {
	case 0, 1:
		return nil

	default:
		return fmt.Errorf(
			"persist redirect event %q affected %d rows",
			event.EventID,
			rowsAffected,
		)
	}
}
