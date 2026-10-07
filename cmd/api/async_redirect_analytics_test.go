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
