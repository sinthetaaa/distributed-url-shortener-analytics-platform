package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"
)

const (
	redirectAnalyticsQueueCapacity   = 4096
	redirectAnalyticsPublishTimeout  = 500 * time.Millisecond
	redirectAnalyticsDropLogInterval = time.Second

	redirectAnalyticsEnqueueResultEnqueued = "enqueued"
	redirectAnalyticsEnqueueResultDropped  = "dropped"

	redirectAnalyticsPublishResultSuccess = "success"
	redirectAnalyticsPublishResultFailure = "failure"
)

type asyncRedirectEventRecorder struct {
	logger         *slog.Logger
	publisher      analytics.RedirectEventPublisher
	queue          chan analytics.RedirectEvent
	publishTimeout time.Duration
	metrics        *observability.Metrics

	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once

	cancel context.CancelFunc
	done   chan struct{}

	droppedTotal    atomic.Uint64
	droppedSinceLog atomic.Uint64
}

func newAsyncRedirectEventRecorder(
	logger *slog.Logger,
	publisher analytics.RedirectEventPublisher,
	queueCapacity int,
	publishTimeout time.Duration,
) (*asyncRedirectEventRecorder, error) {
	return newAsyncRedirectEventRecorderWithMetrics(
		logger,
		publisher,
		queueCapacity,
		publishTimeout,
		nil,
	)
}

func newAsyncRedirectEventRecorderWithMetrics(
	logger *slog.Logger,
	publisher analytics.RedirectEventPublisher,
	queueCapacity int,
	publishTimeout time.Duration,
	metrics *observability.Metrics,
) (*asyncRedirectEventRecorder, error) {
	if publisher == nil {
		return nil, fmt.Errorf("redirect analytics publisher must not be nil")
	}

	if queueCapacity <= 0 {
		return nil, fmt.Errorf("redirect analytics queue capacity must be positive")
	}

	if publishTimeout <= 0 {
		return nil, fmt.Errorf("redirect analytics publish timeout must be positive")
	}

	workerCtx, cancel := context.WithCancel(context.Background())

	recorder := &asyncRedirectEventRecorder{
		logger:         logger,
		publisher:      publisher,
		queue:          make(chan analytics.RedirectEvent, queueCapacity),
		publishTimeout: publishTimeout,
		metrics:        metrics,
		cancel:         cancel,
		done:           make(chan struct{}),
	}

	go recorder.run(workerCtx)

	return recorder, nil
}

func newProductionRedirectEventRecorder(
	logger *slog.Logger,
) (redirectEventRecorder, func(context.Context)) {
	return newProductionRedirectEventRecorderWithMetrics(
		logger,
		nil,
	)
}

func newProductionRedirectEventRecorderWithMetrics(
	logger *slog.Logger,
	metrics *observability.Metrics,
) (redirectEventRecorder, func(context.Context)) {
	config, err := analytics.KafkaProducerConfigFromEnv()
	if err != nil {
		logger.Warn(
			"redirect analytics disabled",
			"error", err,
		)

		return noopRedirectEventRecorder{}, func(context.Context) {}
	}

	producer, err := analytics.NewKafkaRedirectEventProducer(config)
	if err != nil {
		logger.Warn(
			"failed to create redirect analytics producer; analytics disabled",
			"error", err,
		)

		return noopRedirectEventRecorder{}, func(context.Context) {}
	}

	recorder, err := newAsyncRedirectEventRecorderWithMetrics(
		logger,
		producer,
		redirectAnalyticsQueueCapacity,
		redirectAnalyticsPublishTimeout,
		metrics,
	)
	if err != nil {
		producer.Close()

		logger.Warn(
			"failed to create redirect analytics recorder; analytics disabled",
			"error", err,
		)

		return noopRedirectEventRecorder{}, func(context.Context) {}
	}

	logger.Info(
		"redirect analytics enabled",
		"brokers", config.Brokers,
		"topic", config.Topic,
		"queue_capacity", redirectAnalyticsQueueCapacity,
	)

	return recorder, recorder.Close
}

func (r *asyncRedirectEventRecorder) Record(event analytics.RedirectEvent) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return
	}

	r.incrementQueueDepth()

	select {
	case r.queue <- event:
		r.recordEnqueue(redirectAnalyticsEnqueueResultEnqueued)

	default:
		r.decrementQueueDepth()
		r.recordEnqueue(redirectAnalyticsEnqueueResultDropped)
		r.droppedTotal.Add(1)
		r.droppedSinceLog.Add(1)
	}
}

func (r *asyncRedirectEventRecorder) Dropped() uint64 {
	return r.droppedTotal.Load()
}

func (r *asyncRedirectEventRecorder) Close(ctx context.Context) {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		close(r.queue)
		r.mu.Unlock()

		select {
		case <-r.done:
		case <-ctx.Done():
			r.cancel()
			<-r.done
		}

		r.publisher.Close()
	})
}

func (r *asyncRedirectEventRecorder) run(ctx context.Context) {
	ticker := time.NewTicker(redirectAnalyticsDropLogInterval)
	defer ticker.Stop()
	defer close(r.done)

	for {
		select {
		case <-ctx.Done():
			r.logDroppedEvents()
			return

		case <-ticker.C:
			r.logDroppedEvents()

		case event, ok := <-r.queue:
			if !ok {
				r.logDroppedEvents()
				return
			}

			r.decrementQueueDepth()

			publishCtx, cancel := context.WithTimeout(
				ctx,
				r.publishTimeout,
			)

			err := r.publisher.Publish(publishCtx, event)
			cancel()

			if err != nil {
				r.recordPublish(
					redirectAnalyticsPublishResultFailure,
				)

				r.logger.Warn(
					"failed to publish redirect analytics event",
					"event_id", event.EventID,
					"short_code", event.ShortCode,
					"error", err,
				)

				continue
			}

			r.recordPublish(
				redirectAnalyticsPublishResultSuccess,
			)
		}
	}
}

func (r *asyncRedirectEventRecorder) incrementQueueDepth() {
	if r.metrics == nil {
		return
	}

	r.metrics.RedirectAnalyticsQueueDepth.Inc()
}

func (r *asyncRedirectEventRecorder) decrementQueueDepth() {
	if r.metrics == nil {
		return
	}

	r.metrics.RedirectAnalyticsQueueDepth.Dec()
}

func (r *asyncRedirectEventRecorder) recordEnqueue(result string) {
	if r.metrics == nil {
		return
	}

	r.metrics.RedirectAnalyticsEnqueuesTotal.
		WithLabelValues(result).
		Inc()
}

func (r *asyncRedirectEventRecorder) recordPublish(result string) {
	if r.metrics == nil {
		return
	}

	r.metrics.RedirectAnalyticsPublishesTotal.
		WithLabelValues(result).
		Inc()
}

func (r *asyncRedirectEventRecorder) logDroppedEvents() {
	dropped := r.droppedSinceLog.Swap(0)
	if dropped == 0 {
		return
	}

	r.logger.Warn(
		"redirect analytics queue full; events dropped",
		"count", dropped,
		"dropped_total", r.droppedTotal.Load(),
	)
}
