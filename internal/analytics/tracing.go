package analytics

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/twmb/franz-go/pkg/kgo"
)

const analyticsTracerName = "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"

func analyticsTracer() trace.Tracer {
	return otel.Tracer(analyticsTracerName)
}

func injectKafkaTraceContext(
	ctx context.Context,
	record *kgo.Record,
) {
	if record == nil {
		return
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	for _, key := range carrier.Keys() {
		record.Headers = setKafkaHeader(
			record.Headers,
			key,
			carrier.Get(key),
		)
	}
}

func extractKafkaTraceContext(
	ctx context.Context,
	record *kgo.Record,
) context.Context {
	if record == nil {
		return ctx
	}

	carrier := propagation.MapCarrier{}

	for _, header := range record.Headers {
		key := strings.TrimSpace(header.Key)
		if key == "" {
			continue
		}

		carrier.Set(key, string(header.Value))
	}

	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func setKafkaHeader(
	headers []kgo.RecordHeader,
	key string,
	value string,
) []kgo.RecordHeader {
	for index := range headers {
		if strings.EqualFold(headers[index].Key, key) {
			headers[index].Key = key
			headers[index].Value = []byte(value)
			return headers
		}
	}

	return append(
		headers,
		kgo.RecordHeader{
			Key:   key,
			Value: []byte(value),
		},
	)
}

func markSpanError(
	span trace.Span,
	err error,
	message string,
) {
	if err == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, message)
}
