package analytics

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestKafkaTraceContextRoundTrip(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
	})

	traceID := trace.TraceID{
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c,
		0x0d, 0x0e, 0x0f, 0x10,
	}
	spanID := trace.SpanID{
		0x11, 0x12, 0x13, 0x14,
		0x15, 0x16, 0x17, 0x18,
	}

	spanContext := trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
		},
	)

	ctx := trace.ContextWithSpanContext(
		context.Background(),
		spanContext,
	)

	record := &kgo.Record{
		Topic: "shortscale.redirect-events.v1",
	}

	injectKafkaTraceContext(ctx, record)

	if len(record.Headers) == 0 {
		t.Fatal("expected Kafka trace headers")
	}

	extracted := extractKafkaTraceContext(
		context.Background(),
		record,
	)

	got := trace.SpanContextFromContext(extracted)

	if got.TraceID() != traceID {
		t.Fatalf(
			"expected trace id %s, got %s",
			traceID,
			got.TraceID(),
		)
	}

	if got.SpanID() != spanID {
		t.Fatalf(
			"expected span id %s, got %s",
			spanID,
			got.SpanID(),
		)
	}

	if !got.IsRemote() {
		t.Fatal("expected extracted Kafka span context to be remote")
	}
}

func TestSetKafkaHeaderReplacesExistingPropagationHeader(t *testing.T) {
	headers := []kgo.RecordHeader{
		{
			Key:   "traceparent",
			Value: []byte("old"),
		},
	}

	headers = setKafkaHeader(
		headers,
		"traceparent",
		"new",
	)

	if len(headers) != 1 {
		t.Fatalf(
			"expected 1 Kafka header, got %d",
			len(headers),
		)
	}

	if got := string(headers[0].Value); got != "new" {
		t.Fatalf(
			"expected replacement value %q, got %q",
			"new",
			got,
		)
	}
}
