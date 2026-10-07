package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestConsumerLagMetricsAreRegistered(t *testing.T) {
	metrics, err := NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	metrics.LagRecords.Set(17)
	metrics.LagCollectionFailuresTotal.Inc()

	if got := testutil.ToFloat64(metrics.LagRecords); got != 17 {
		t.Fatalf("expected lag gauge 17, got %v", got)
	}

	if got := testutil.ToFloat64(
		metrics.LagCollectionFailuresTotal,
	); got != 1 {
		t.Fatalf(
			"expected lag collection failures 1, got %v",
			got,
		)
	}
}
