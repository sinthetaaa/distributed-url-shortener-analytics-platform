package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	analytics "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/analytics"
)

const (
	redirectAnalyticsQueueCapacity   = 4096
	redirectAnalyticsPublishTimeout  = 500 * time.Millisecond
	redirectAnalyticsDropLogInterval = time.Second
)

type asyncRedirectEventRecorder struct {
	logger         *slog.Logger
	publisher      analytics.RedirectEventPublisher
	queue          chan analytics.RedirectEvent
	publishTimeout time.Duration

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
		cancel:         cancel,
		done:           make(chan struct{}),
	}

	go recorder.run(workerCtx)

	return recorder, nil
}

func newProductionRedirectEventRecorder(
	logger *slog.Logger,
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

	recorder, err := newAsyncRedirectEventRecorder(
		logger,
		producer,
		redirectAnalyticsQueueCapacity,
		redirectAnalyticsPublishTimeout,
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

	select {
	case r.queue <- event:
	default:
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

			publishCtx, cancel := context.WithTimeout(
				ctx,
				r.publishTimeout,
			)

			err := r.publisher.Publish(publishCtx, event)
			cancel()

			if err != nil {
				r.logger.Warn(
					"failed to publish redirect analytics event",
					"event_id", event.EventID,
					"short_code", event.ShortCode,
					"error", err,
				)
			}
		}
	}
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
