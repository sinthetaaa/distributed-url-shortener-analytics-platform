package analytics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type retryingRedirectEventProcessor struct {
	mu sync.Mutex

	failuresRemaining int
	calls             int
	processed         chan RedirectEvent
}

func (p *retryingRedirectEventProcessor) Process(
	_ context.Context,
	event RedirectEvent,
) error {
	p.mu.Lock()

	p.calls++

	if p.failuresRemaining > 0 {
		p.failuresRemaining--
		p.mu.Unlock()

		return errors.New("database unavailable")
	}

	p.mu.Unlock()

	p.processed <- event

	return nil
}

func (p *retryingRedirectEventProcessor) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

func TestKafkaRedirectEventConsumerRetriesProcessFailure(
	t *testing.T,
) {
	event := validRedirectEventForTest()

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
				records: []*kgo.Record{
					record,
				},
			},
		},
	}

	consumer := &KafkaRedirectEventConsumer{
		client: client,
	}

	processor := &retryingRedirectEventProcessor{
		failuresRemaining: 2,
		processed:         make(chan RedirectEvent, 1),
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
		assertRedirectEventsEqual(
			t,
			event,
			processed,
		)

	case <-time.After(2 * time.Second):
		t.Fatal(
			"consumer did not recover from transient process failure",
		)
	}

	if got := processor.callCount(); got != 3 {
		t.Fatalf(
			"expected 3 process attempts, got %d",
			got,
		)
	}

	pollCalls, commits := client.snapshot()

	if pollCalls < 1 || pollCalls > 2 {
		t.Fatalf(
			"expected 1 completed poll plus at most 1 blocking next poll, got %d",
			pollCalls,
		)
	}

	if commits != 1 {
		t.Fatalf(
			"expected exactly 1 commit after successful processing, got %d",
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

func TestWaitForKafkaProcessRetryStopsOnCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	if waitForKafkaProcessRetry(
		ctx,
		kafkaProcessRetryMaxDelay,
	) {
		t.Fatal(
			"expected cancelled context to stop process retry wait",
		)
	}
}

func TestNextKafkaProcessRetryDelayCapsAtMaximum(
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
			want:    kafkaProcessRetryInitialDelay,
		},
		{
			name:    "doubles below maximum",
			current: 400 * time.Millisecond,
			want:    800 * time.Millisecond,
		},
		{
			name:    "caps at maximum",
			current: 1500 * time.Millisecond,
			want:    kafkaProcessRetryMaxDelay,
		},
		{
			name:    "stays capped",
			current: kafkaProcessRetryMaxDelay,
			want:    kafkaProcessRetryMaxDelay,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				got := nextKafkaProcessRetryDelay(
					tt.current,
				)

				if got != tt.want {
					t.Fatalf(
						"nextKafkaProcessRetryDelay(%s) = %s, want %s",
						tt.current,
						got,
						tt.want,
					)
				}
			},
		)
	}
}
