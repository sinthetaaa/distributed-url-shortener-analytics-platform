package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type testRedirectEventPublisher struct {
	mu sync.Mutex

	events []analytics.RedirectEvent
	err    error

	started     chan struct{}
	startedOnce sync.Once
	release     chan struct{}

	closed atomic.Bool
}

func (p *testRedirectEventPublisher) Publish(
	ctx context.Context,
	event analytics.RedirectEvent,
) error {
	if p.started != nil {
		p.startedOnce.Do(func() {
			close(p.started)
		})
	}

	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if p.err != nil {
		return p.err
	}

	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()

	return nil
}

func (p *testRedirectEventPublisher) Close() {
	p.closed.Store(true)
}

func (p *testRedirectEventPublisher) eventCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.events)
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testAnalyticsEvent(t *testing.T, shortCode string) analytics.RedirectEvent {
	t.Helper()

	event, err := analytics.NewRedirectEvent(shortCode)
	if err != nil {
		t.Fatalf("create redirect analytics event: %v", err)
	}

	return event
}

func TestAsyncRedirectEventRecorderDropsWhenQueueIsFullWithoutBlocking(
	t *testing.T,
) {
	started := make(chan struct{})
	release := make(chan struct{})

	publisher := &testRedirectEventPublisher{
		started: started,
		release: release,
	}

	recorder, err := newAsyncRedirectEventRecorder(
		newDiscardLogger(),
		publisher,
		1,
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("create async recorder: %v", err)
	}

	recorder.Record(testAnalyticsEvent(t, "event-one"))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("publisher did not start processing first event")
	}

	recorder.Record(testAnalyticsEvent(t, "event-two"))

	start := time.Now()
	recorder.Record(testAnalyticsEvent(t, "event-three"))
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("queue-full Record blocked for %s", elapsed)
	}

	if recorder.Dropped() != 1 {
		t.Fatalf("expected 1 dropped event, got %d", recorder.Dropped())
	}

	close(release)

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	recorder.Close(closeCtx)

	if publisher.eventCount() != 2 {
		t.Fatalf(
			"expected 2 published events, got %d",
			publisher.eventCount(),
		)
	}

	if !publisher.closed.Load() {
		t.Fatal("expected publisher to be closed")
	}
}

func TestAsyncRedirectEventRecorderDrainsQueueOnClose(t *testing.T) {
	publisher := &testRedirectEventPublisher{}

	recorder, err := newAsyncRedirectEventRecorder(
		newDiscardLogger(),
		publisher,
		4,
		time.Second,
	)
	if err != nil {
		t.Fatalf("create async recorder: %v", err)
	}

	recorder.Record(testAnalyticsEvent(t, "one"))
	recorder.Record(testAnalyticsEvent(t, "two"))
	recorder.Record(testAnalyticsEvent(t, "three"))

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	recorder.Close(closeCtx)

	if publisher.eventCount() != 3 {
		t.Fatalf(
			"expected 3 published events after drain, got %d",
			publisher.eventCount(),
		)
	}
}

func TestAsyncRedirectEventRecorderContainsPublisherFailures(t *testing.T) {
	publisher := &testRedirectEventPublisher{
		err: errors.New("Kafka unavailable"),
	}

	recorder, err := newAsyncRedirectEventRecorder(
		newDiscardLogger(),
		publisher,
		2,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("create async recorder: %v", err)
	}

	start := time.Now()
	recorder.Record(testAnalyticsEvent(t, "failure-contained"))
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("Record blocked on publisher failure for %s", elapsed)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	recorder.Close(closeCtx)

	if !publisher.closed.Load() {
		t.Fatal("expected failing publisher to be closed")
	}
}

func TestAsyncRedirectEventRecorderRecordsQueueAndPublishMetrics(
	t *testing.T,
) {
	metrics, err := observability.NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})

	publisher := &testRedirectEventPublisher{
		started: started,
		release: release,
	}

	recorder, err := newAsyncRedirectEventRecorderWithMetrics(
		newDiscardLogger(),
		publisher,
		1,
		5*time.Second,
		metrics,
	)
	if err != nil {
		t.Fatalf("create async recorder: %v", err)
	}

	recorder.Record(testAnalyticsEvent(t, "metric-one"))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("publisher did not start processing first event")
	}

	recorder.Record(testAnalyticsEvent(t, "metric-two"))
	recorder.Record(testAnalyticsEvent(t, "metric-three"))

	enqueued := testutil.ToFloat64(
		metrics.RedirectAnalyticsEnqueuesTotal.
			WithLabelValues(
				redirectAnalyticsEnqueueResultEnqueued,
			),
	)
	if enqueued != 2 {
		t.Fatalf("expected 2 enqueued events, got %v", enqueued)
	}

	dropped := testutil.ToFloat64(
		metrics.RedirectAnalyticsEnqueuesTotal.
			WithLabelValues(
				redirectAnalyticsEnqueueResultDropped,
			),
	)
	if dropped != 1 {
		t.Fatalf("expected 1 dropped event, got %v", dropped)
	}

	queueDepth := testutil.ToFloat64(
		metrics.RedirectAnalyticsQueueDepth,
	)
	if queueDepth != 1 {
		t.Fatalf(
			"expected queue depth 1 while publisher blocked, got %v",
			queueDepth,
		)
	}

	close(release)

	closeCtx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	recorder.Close(closeCtx)

	published := testutil.ToFloat64(
		metrics.RedirectAnalyticsPublishesTotal.
			WithLabelValues(
				redirectAnalyticsPublishResultSuccess,
			),
	)
	if published != 2 {
		t.Fatalf(
			"expected 2 successful publishes, got %v",
			published,
		)
	}

	queueDepth = testutil.ToFloat64(
		metrics.RedirectAnalyticsQueueDepth,
	)
	if queueDepth != 0 {
		t.Fatalf(
			"expected queue depth 0 after drain, got %v",
			queueDepth,
		)
	}
}

func TestAsyncRedirectEventRecorderRecordsPublishFailureMetric(
	t *testing.T,
) {
	metrics, err := observability.NewMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	publisher := &testRedirectEventPublisher{
		err: errors.New("Kafka unavailable"),
	}

	recorder, err := newAsyncRedirectEventRecorderWithMetrics(
		newDiscardLogger(),
		publisher,
		2,
		100*time.Millisecond,
		metrics,
	)
	if err != nil {
		t.Fatalf("create async recorder: %v", err)
	}

	recorder.Record(
		testAnalyticsEvent(t, "metric-publish-failure"),
	)

	closeCtx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	recorder.Close(closeCtx)

	failures := testutil.ToFloat64(
		metrics.RedirectAnalyticsPublishesTotal.
			WithLabelValues(
				redirectAnalyticsPublishResultFailure,
			),
	)
	if failures != 1 {
		t.Fatalf(
			"expected 1 publish failure, got %v",
			failures,
		)
	}

	queueDepth := testutil.ToFloat64(
		metrics.RedirectAnalyticsQueueDepth,
	)
	if queueDepth != 0 {
		t.Fatalf(
			"expected queue depth 0 after failed publish, got %v",
			queueDepth,
		)
	}
}
