package analytics

import (
	"context"
	"fmt"
	"strings"

	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/jackc/pgx/v5/pgtype"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	ctx, span := analyticsTracer().Start(
		ctx,
		"redirect.analytics.persist",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
		),
	)
	defer span.End()

	if strings.TrimSpace(event.EventID) == "" {
		err := fmt.Errorf("redirect event id must not be empty")
		markSpanError(span, err, "validate redirect event")
		return err
	}

	if event.EventType != RedirectEventType {
		err := fmt.Errorf(
			"redirect event type must be %q",
			RedirectEventType,
		)
		markSpanError(span, err, "validate redirect event")
		return err
	}

	if strings.TrimSpace(event.ShortCode) == "" {
		err := fmt.Errorf(
			"redirect event short code must not be empty",
		)
		markSpanError(span, err, "validate redirect event")
		return err
	}

	if event.OccurredAt.IsZero() {
		err := fmt.Errorf(
			"redirect event occurred_at must not be zero",
		)
		markSpanError(span, err, "validate redirect event")
		return err
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
		span.SetAttributes(
			attribute.String(
				"shortscale.analytics.persistence.result",
				persistenceResultError,
			),
		)

		err = fmt.Errorf(
			"persist redirect event %q: %w",
			event.EventID,
			err,
		)
		markSpanError(span, err, "persist redirect event")

		return err
	}

	switch rowsAffected {
	case 0:
		p.recordPersistence(persistenceResultDuplicate)
		span.SetAttributes(
			attribute.String(
				"shortscale.analytics.persistence.result",
				persistenceResultDuplicate,
			),
		)
		return nil

	case 1:
		p.recordPersistence(persistenceResultInserted)
		span.SetAttributes(
			attribute.String(
				"shortscale.analytics.persistence.result",
				persistenceResultInserted,
			),
		)
		return nil

	default:
		p.recordPersistence(persistenceResultError)
		span.SetAttributes(
			attribute.String(
				"shortscale.analytics.persistence.result",
				persistenceResultError,
			),
		)

		err := fmt.Errorf(
			"persist redirect event %q affected %d rows",
			event.EventID,
			rowsAffected,
		)
		markSpanError(span, err, "persist redirect event")

		return err
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
