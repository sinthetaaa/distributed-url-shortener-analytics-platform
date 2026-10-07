package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

type contextualTestRedirectEventRecorder struct {
	ctx    context.Context
	event  analytics.RedirectEvent
	called bool
}

func (r *contextualTestRedirectEventRecorder) Record(
	analytics.RedirectEvent,
) {
}

func (r *contextualTestRedirectEventRecorder) RecordContext(
	ctx context.Context,
	event analytics.RedirectEvent,
) {
	r.ctx = ctx
	r.event = event
	r.called = true
}

func testRedirectTraceSpanContext() trace.SpanContext {
	return trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID: trace.TraceID{
				0x01, 0x02, 0x03, 0x04,
				0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c,
				0x0d, 0x0e, 0x0f, 0x10,
			},
			SpanID: trace.SpanID{
				0x11, 0x12, 0x13, 0x14,
				0x15, 0x16, 0x17, 0x18,
			},
			TraceFlags: trace.FlagsSampled,
		},
	)
}

func TestDetachRedirectAnalyticsTraceContextPreservesTraceWithoutCancellation(
	t *testing.T,
) {
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		testRedirectTraceSpanContext(),
	)

	member, err := baggage.NewMember("test-key", "test-value")
	if err != nil {
		t.Fatalf("create baggage member: %v", err)
	}

	currentBaggage, err := baggage.New(member)
	if err != nil {
		t.Fatalf("create baggage: %v", err)
	}

	ctx = baggage.ContextWithBaggage(ctx, currentBaggage)

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	detached := detachRedirectAnalyticsTraceContext(cancelledCtx)

	if detached.Err() != nil {
		t.Fatalf(
			"expected detached trace context without cancellation, got %v",
			detached.Err(),
		)
	}

	wantSpanContext := trace.SpanContextFromContext(ctx)
	gotSpanContext := trace.SpanContextFromContext(detached)

	if gotSpanContext.TraceID() != wantSpanContext.TraceID() {
		t.Fatalf(
			"expected trace id %s, got %s",
			wantSpanContext.TraceID(),
			gotSpanContext.TraceID(),
		)
	}

	if gotSpanContext.SpanID() != wantSpanContext.SpanID() {
		t.Fatalf(
			"expected span id %s, got %s",
			wantSpanContext.SpanID(),
			gotSpanContext.SpanID(),
		)
	}

	if got := baggage.FromContext(detached).Member("test-key").Value(); got != "test-value" {
		t.Fatalf(
			"expected baggage value %q, got %q",
			"test-value",
			got,
		)
	}
}

func TestAttachRedirectAnalyticsTraceContextPreservesWorkerCancellation(
	t *testing.T,
) {
	traceCtx := trace.ContextWithSpanContext(
		context.Background(),
		testRedirectTraceSpanContext(),
	)

	base, cancel := context.WithCancel(context.Background())
	cancel()

	attached := attachRedirectAnalyticsTraceContext(
		base,
		traceCtx,
	)

	if attached.Err() != context.Canceled {
		t.Fatalf(
			"expected worker cancellation to be preserved, got %v",
			attached.Err(),
		)
	}

	want := trace.SpanContextFromContext(traceCtx)
	got := trace.SpanContextFromContext(attached)

	if got.TraceID() != want.TraceID() {
		t.Fatalf(
			"expected trace id %s, got %s",
			want.TraceID(),
			got.TraceID(),
		)
	}
}

func TestRecordRedirectEventUsesContextualRecorderWhenAvailable(
	t *testing.T,
) {
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		testRedirectTraceSpanContext(),
	)

	recorder := &contextualTestRedirectEventRecorder{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	recordRedirectEvent(
		ctx,
		logger,
		recorder,
		"abc1234",
	)

	if !recorder.called {
		t.Fatal("expected contextual recorder to be called")
	}

	want := trace.SpanContextFromContext(ctx)
	got := trace.SpanContextFromContext(recorder.ctx)

	if got.TraceID() != want.TraceID() {
		t.Fatalf(
			"expected trace id %s, got %s",
			want.TraceID(),
			got.TraceID(),
		)
	}

	if recorder.event.ShortCode != "abc1234" {
		t.Fatalf(
			"expected short code %q, got %q",
			"abc1234",
			recorder.event.ShortCode,
		)
	}
}
