package analytics

import (
	"context"
	"errors"
	"testing"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPostgresRedirectEventProcessorRecordsPersistenceMetrics(
	t *testing.T,
) {
	metrics, err := observability.NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	tests := []struct {
		name         string
		rowsAffected int64
		storeErr     error
		result       string
		wantErr      bool
	}{
		{
			name:         "inserted",
			rowsAffected: 1,
			result:       persistenceResultInserted,
		},
		{
			name:         "duplicate",
			rowsAffected: 0,
			result:       persistenceResultDuplicate,
		},
		{
			name:     "store error",
			storeErr: errors.New("database unavailable"),
			result:   persistenceResultError,
			wantErr:  true,
		},
		{
			name:         "unexpected rows affected",
			rowsAffected: 2,
			result:       persistenceResultError,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &testRedirectEventStore{
				rowsAffected: tc.rowsAffected,
				err:          tc.storeErr,
			}

			processor, err := NewPostgresRedirectEventProcessorWithMetrics(
				store,
				metrics,
			)
			if err != nil {
				t.Fatalf("create processor: %v", err)
			}

			err = processor.Process(
				context.Background(),
				validRedirectEventForTest(),
			)

			if tc.wantErr && err == nil {
				t.Fatal("expected process error")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected process error: %v", err)
			}
		})
	}

	if got := testutil.ToFloat64(
		metrics.PersistenceTotal.WithLabelValues(
			persistenceResultInserted,
		),
	); got != 1 {
		t.Fatalf(
			"expected inserted persistence counter 1, got %v",
			got,
		)
	}

	if got := testutil.ToFloat64(
		metrics.PersistenceTotal.WithLabelValues(
			persistenceResultDuplicate,
		),
	); got != 1 {
		t.Fatalf(
			"expected duplicate persistence counter 1, got %v",
			got,
		)
	}

	if got := testutil.ToFloat64(
		metrics.PersistenceTotal.WithLabelValues(
			persistenceResultError,
		),
	); got != 2 {
		t.Fatalf(
			"expected persistence error counter 2, got %v",
			got,
		)
	}
}
