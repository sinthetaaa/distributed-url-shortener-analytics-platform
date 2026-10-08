package analytics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type scriptedKafkaPollResult struct {
	records []*kgo.Record
	err     error
}

type scriptedKafkaConsumerClient struct {
	mu sync.Mutex

	pollResults []scriptedKafkaPollResult
	pollCalls   int
	commits     int
	closed      bool
}

func (c *scriptedKafkaConsumerClient) Poll(
	ctx context.Context,
) ([]*kgo.Record, error) {
	c.mu.Lock()

	c.pollCalls++

	if len(c.pollResults) == 0 {
		c.mu.Unlock()

		<-ctx.Done()

		return nil, ctx.Err()
	}

	result := c.pollResults[0]
	c.pollResults = c.pollResults[1:]

	c.mu.Unlock()

	return result.records, result.err
}

func (c *scriptedKafkaConsumerClient) Commit(
	context.Context,
	*kgo.Record,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commits++

	return nil
}

func (c *scriptedKafkaConsumerClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
}

func (c *scriptedKafkaConsumerClient) snapshot() (
	pollCalls int,
	commits int,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.pollCalls, c.commits
}

type signalingRedirectEventProcessor struct {
	processed chan RedirectEvent
}

func (p *signalingRedirectEventProcessor) Process(
	_ context.Context,
	event RedirectEvent,
) error {
	p.processed <- event

	return nil
}

func TestKafkaRedirectEventConsumerRetriesPollFailure(
	t *testing.T,
) {
	event := RedirectEvent{
		EventID:   "123e4567-e89b-42d3-a456-426614174012",
		EventType: RedirectEventType,
		ShortCode: "retry12C",
		OccurredAt: time.Date(
			2026,
			time.October,
			8,
			6,
			0,
			0,
			0,
			time.UTC,
		),
	}

	record, err := kafkaRecordForRedirectEvent(
		RedirectEventsTopic,
		event,
	)
	if err != nil {
		t.Fatalf(
			"kafkaRecordForRedirectEvent returned error: %v",
			err,
		)
	}

	client := &scriptedKafkaConsumerClient{
		pollResults: []scriptedKafkaPollResult{
			{
				err: errors.New("broker unavailable"),
			},
			{
				records: []*kgo.Record{
					record,
				},
			},
		},
	}

	consumer := &KafkaRedirectEventConsumer{
		client: client,
	}

	processor := &signalingRedirectEventProcessor{
		processed: make(chan RedirectEvent, 1),
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	runDone := make(chan error, 1)

	go func() {
		runDone <- consumer.Run(
			ctx,
			processor,
		)
	}()

	select {
	case processed := <-processor.processed:
		if processed.EventID != event.EventID {
			t.Fatalf(
				"expected event id %q, got %q",
				event.EventID,
				processed.EventID,
			)
		}

		if processed.ShortCode != event.ShortCode {
			t.Fatalf(
				"expected short code %q, got %q",
				event.ShortCode,
				processed.ShortCode,
			)
		}

		if !processed.OccurredAt.Equal(
			event.OccurredAt,
		) {
			t.Fatalf(
				"expected occurred_at %s, got %s",
				event.OccurredAt,
				processed.OccurredAt,
			)
		}

	case <-time.After(2 * time.Second):
		t.Fatal(
			"consumer did not recover from transient poll failure",
		)
	}

	pollCalls, commits := client.snapshot()

	if pollCalls < 2 {
		t.Fatalf(
			"expected at least 2 poll calls, got %d",
			pollCalls,
		)
	}

	if commits != 1 {
		t.Fatalf(
			"expected 1 commit, got %d",
			commits,
		)
	}

	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf(
				"Run returned error after cancellation: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"consumer did not stop after cancellation",
		)
	}
}

func TestWaitForKafkaPollRetryStopsOnCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	if waitForKafkaPollRetry(
		ctx,
		kafkaPollRetryMaxDelay,
	) {
		t.Fatal(
			"expected cancelled context to stop retry wait",
		)
	}
}

func TestNextKafkaPollRetryDelayCapsAtMaximum(
	t *testing.T,
) {
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{
			name:    "zero uses initial delay",
			current: 0,
			want:    kafkaPollRetryInitialDelay,
		},
		{
			name:    "doubles below maximum",
			current: 400 * time.Millisecond,
			want:    800 * time.Millisecond,
		},
		{
			name:    "caps at maximum",
			current: 1500 * time.Millisecond,
			want:    kafkaPollRetryMaxDelay,
		},
		{
			name:    "stays capped",
			current: kafkaPollRetryMaxDelay,
			want:    kafkaPollRetryMaxDelay,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				got := nextKafkaPollRetryDelay(
					tt.current,
				)

				if got != tt.want {
					t.Fatalf(
						"nextKafkaPollRetryDelay(%s) = %s, want %s",
						tt.current,
						got,
						tt.want,
					)
				}
			},
		)
	}
}
