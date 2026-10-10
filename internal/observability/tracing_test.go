package observability

import (
	"context"
	"testing"
)

func TestNewTracingDisabledWithoutExporterEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tracing, err := NewTracing(
		context.Background(),
		"shortscale-test",
	)
	if err != nil {
		t.Fatalf("create tracing: %v", err)
	}

	if tracing.Enabled() {
		t.Fatal("expected tracing to be disabled without OTLP endpoint")
	}

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown disabled tracing: %v", err)
	}
}

func TestNewTracingRejectsEmptyServiceName(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	if _, err := NewTracing(
		context.Background(),
		"   ",
	); err == nil {
		t.Fatal("expected empty service name to fail")
	}
}

func TestDeploymentEnvironmentNameDefaultsToLocal(t *testing.T) {
	t.Setenv(deploymentEnvironmentVariable, "")

	if got := deploymentEnvironmentName(); got != localEnvironment {
		t.Fatalf(
			"expected deployment environment %q, got %q",
			localEnvironment,
			got,
		)
	}
}

func TestDeploymentEnvironmentNameUsesConfiguredValue(t *testing.T) {
	t.Setenv(deploymentEnvironmentVariable, "  production  ")

	if got := deploymentEnvironmentName(); got != "production" {
		t.Fatalf(
			"expected deployment environment %q, got %q",
			"production",
			got,
		)
	}
}
