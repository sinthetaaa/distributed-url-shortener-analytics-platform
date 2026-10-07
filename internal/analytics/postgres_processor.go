package analytics

import (
	"context"
	"fmt"
	"strings"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	persistenceResultInserted  = "inserted"
	persistenceResultDuplicate = "duplicate"
	persistenceResultError     = "error"
)

type redirectEventStore interface {
	InsertRedirectEvent(
		context.Context,
		database.InsertRedirectEventParams,
	) (int64, error)
}

type PostgresRedirectEventProcessor struct {
	store   redirectEventStore
	metrics *observability.ConsumerMetrics
}

func NewPostgresRedirectEventProcessor(
	store redirectEventStore,
) (*PostgresRedirectEventProcessor, error) {
	return NewPostgresRedirectEventProcessorWithMetrics(
		store,
		nil,
	)
}

func NewPostgresRedirectEventProcessorWithMetrics(
	store redirectEventStore,
	metrics *observability.ConsumerMetrics,
) (*PostgresRedirectEventProcessor, error) {
	if store == nil {
		return nil, fmt.Errorf(
			"redirect event store must not be nil",
		)
	}

	return &PostgresRedirectEventProcessor{
		store:   store,
		metrics: metrics,
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
		p.recordPersistence(persistenceResultError)

		return fmt.Errorf(
			"persist redirect event %q: %w",
			event.EventID,
			err,
		)
	}

	switch rowsAffected {
	case 0:
		p.recordPersistence(persistenceResultDuplicate)
		return nil

	case 1:
		p.recordPersistence(persistenceResultInserted)
		return nil

	default:
		p.recordPersistence(persistenceResultError)

		return fmt.Errorf(
			"persist redirect event %q affected %d rows",
			event.EventID,
			rowsAffected,
		)
	}
}

func (p *PostgresRedirectEventProcessor) recordPersistence(
	result string,
) {
	if p.metrics == nil {
		return
	}

	p.metrics.PersistenceTotal.
		WithLabelValues(result).
		Inc()
}
