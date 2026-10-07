package observability

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	serviceNamespace = "shortscale"
	localEnvironment = "local"
)

type Tracing struct {
	provider *sdktrace.TracerProvider
}

func NewTracing(
	ctx context.Context,
	serviceName string,
) (*Tracing, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return &Tracing{}, fmt.Errorf(
			"OpenTelemetry service name must not be empty",
		)
	}

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	if strings.TrimSpace(
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	) == "" {
		return &Tracing{}, nil
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return &Tracing{}, fmt.Errorf(
			"create OTLP trace exporter: %w",
			err,
		)
	}

	resource := sdkresource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.namespace", serviceNamespace),
		attribute.String(
			"deployment.environment.name",
			localEnvironment,
		),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
		sdktrace.WithSampler(
			sdktrace.ParentBased(
				sdktrace.AlwaysSample(),
			),
		),
	)

	otel.SetTracerProvider(provider)

	return &Tracing{
		provider: provider,
	}, nil
}

func (t *Tracing) Enabled() bool {
	return t != nil && t.provider != nil
}

func (t *Tracing) Shutdown(ctx context.Context) error {
	if !t.Enabled() {
		return nil
	}

	if err := t.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf(
			"shutdown OpenTelemetry tracer provider: %w",
			err,
		)
	}

	return nil
}
