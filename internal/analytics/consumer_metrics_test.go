package analytics

import (
	"testing"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestKafkaRedirectEventConsumerRecordsBoundedMetrics(
	t *testing.T,
) {
	metrics, err := observability.NewConsumerMetrics()
	if err != nil {
		t.Fatalf("create consumer metrics: %v", err)
	}

	consumer := &KafkaRedirectEventConsumer{
		metrics: metrics,
	}

	for _, stage := range []string{
		consumerFailureStagePoll,
		consumerFailureStageDecode,
		consumerFailureStageProcess,
		consumerFailureStageCommit,
	} {
		consumer.recordFailure(stage)

		got := testutil.ToFloat64(
			metrics.FailuresTotal.WithLabelValues(stage),
		)
		if got != 1 {
			t.Fatalf(
				"expected stage %q failure counter 1, got %v",
				stage,
				got,
			)
		}
	}

	consumer.recordProcessed()

	if got := testutil.ToFloat64(
		metrics.ProcessedTotal,
	); got != 1 {
		t.Fatalf(
			"expected processed counter 1, got %v",
			got,
		)
	}
}
