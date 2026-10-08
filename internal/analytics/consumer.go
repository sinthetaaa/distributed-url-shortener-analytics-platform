package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	defaultKafkaConsumerGroup    = "shortscale-analytics-v1"
	defaultKafkaConsumerClientID = "shortscale-analytics-consumer"

	kafkaConsumerResetEarliest = "earliest"
	kafkaConsumerResetLatest   = "latest"

	consumerFailureStagePoll    = "poll"
	consumerFailureStageDecode  = "decode"
	consumerFailureStageProcess = "process"
	consumerFailureStageCommit  = "commit"

	kafkaPollRetryInitialDelay = 100 * time.Millisecond
	kafkaPollRetryMaxDelay     = 2 * time.Second

	kafkaProcessRetryInitialDelay = 100 * time.Millisecond
	kafkaProcessRetryMaxDelay     = 2 * time.Second
)

type RedirectEventProcessor interface {
	Process(context.Context, RedirectEvent) error
}

type KafkaConsumerConfig struct {
	Brokers     []string
	Topic       string
	GroupID     string
	ClientID    string
	ResetOffset string
}

func KafkaConsumerConfigFromEnv() (KafkaConsumerConfig, error) {
	config := KafkaConsumerConfig{
		Brokers:     splitAndTrim(os.Getenv("KAFKA_BROKERS")),
		Topic:       strings.TrimSpace(os.Getenv("KAFKA_REDIRECT_TOPIC")),
		GroupID:     strings.TrimSpace(os.Getenv("KAFKA_CONSUMER_GROUP")),
		ClientID:    strings.TrimSpace(os.Getenv("KAFKA_CONSUMER_CLIENT_ID")),
		ResetOffset: strings.TrimSpace(os.Getenv("KAFKA_CONSUMER_RESET")),
	}

	if config.Topic == "" {
		config.Topic = RedirectEventsTopic
	}

	if config.GroupID == "" {
		config.GroupID = defaultKafkaConsumerGroup
	}

	if config.ClientID == "" {
		config.ClientID = defaultKafkaConsumerClientID
	}

	if config.ResetOffset == "" {
		config.ResetOffset = kafkaConsumerResetEarliest
	}

	return validateKafkaConsumerConfig(config)
}

func validateKafkaConsumerConfig(
	config KafkaConsumerConfig,
) (KafkaConsumerConfig, error) {
	brokers := make([]string, 0, len(config.Brokers))

	for _, broker := range config.Brokers {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			brokers = append(brokers, broker)
		}
	}

	if len(brokers) == 0 {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"kafka consumer brokers must not be empty",
		)
	}

	topic := strings.TrimSpace(config.Topic)
	if topic == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"kafka consumer topic must not be empty",
		)
	}

	groupID := strings.TrimSpace(config.GroupID)
	if groupID == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"kafka consumer group id must not be empty",
		)
	}

	clientID := strings.TrimSpace(config.ClientID)
	if clientID == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"kafka consumer client id must not be empty",
		)
	}

	resetOffset := strings.ToLower(strings.TrimSpace(config.ResetOffset))
	if resetOffset == "" {
		resetOffset = kafkaConsumerResetEarliest
	}

	switch resetOffset {
	case kafkaConsumerResetEarliest, kafkaConsumerResetLatest:
	default:
		return KafkaConsumerConfig{}, fmt.Errorf(
			"kafka consumer reset offset must be %q or %q",
			kafkaConsumerResetEarliest,
			kafkaConsumerResetLatest,
		)
	}

	return KafkaConsumerConfig{
		Brokers:     brokers,
		Topic:       topic,
		GroupID:     groupID,
		ClientID:    clientID,
		ResetOffset: resetOffset,
	}, nil
}

type kafkaConsumerClient interface {
	Poll(context.Context) ([]*kgo.Record, error)
	Commit(context.Context, *kgo.Record) error
	Close()
}

type franzKafkaConsumerClient struct {
	client *kgo.Client
}

func (c *franzKafkaConsumerClient) Poll(
	ctx context.Context,
) ([]*kgo.Record, error) {
	fetches := c.client.PollRecords(ctx, 1)

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if errs := fetches.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%v", errs)
	}

	return fetches.Records(), nil
}

func (c *franzKafkaConsumerClient) Commit(
	ctx context.Context,
	record *kgo.Record,
) error {
	return c.client.CommitRecords(ctx, record)
}

func (c *franzKafkaConsumerClient) Close() {
	c.client.Close()
}

type KafkaRedirectEventConsumer struct {
	client  kafkaConsumerClient
	metrics *observability.ConsumerMetrics
}

func NewKafkaRedirectEventConsumer(
	config KafkaConsumerConfig,
) (*KafkaRedirectEventConsumer, error) {
	return NewKafkaRedirectEventConsumerWithMetrics(
		config,
		nil,
	)
}

func NewKafkaRedirectEventConsumerWithMetrics(
	config KafkaConsumerConfig,
	metrics *observability.ConsumerMetrics,
) (*KafkaRedirectEventConsumer, error) {
	config, err := validateKafkaConsumerConfig(config)
	if err != nil {
		return nil, err
	}

	resetOffset := kgo.NewOffset().AtStart()
	if config.ResetOffset == kafkaConsumerResetLatest {
		resetOffset = kgo.NewOffset().AtEnd()
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.ConsumerGroup(config.GroupID),
		kgo.ConsumeTopics(config.Topic),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer: %w", err)
	}

	return &KafkaRedirectEventConsumer{
		client: &franzKafkaConsumerClient{
			client: client,
		},
		metrics: metrics,
	}, nil
}

func (c *KafkaRedirectEventConsumer) Run(
	ctx context.Context,
	processor RedirectEventProcessor,
) error {
	if processor == nil {
		return fmt.Errorf("redirect event processor must not be nil")
	}

	pollRetryDelay := kafkaPollRetryInitialDelay

	for {
		records, err := c.client.Poll(ctx)

		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			c.recordFailure(consumerFailureStagePoll)

			if !waitForKafkaPollRetry(
				ctx,
				pollRetryDelay,
			) {
				return nil
			}

			pollRetryDelay = nextKafkaPollRetryDelay(
				pollRetryDelay,
			)

			continue
		}

		pollRetryDelay = kafkaPollRetryInitialDelay

		if len(records) == 0 {
			continue
		}

		processRetryDelay := kafkaProcessRetryInitialDelay

		for {
			stage, err := c.processRecord(
				ctx,
				processor,
				records[0],
			)
			if err == nil {
				c.recordProcessed()
				break
			}

			if ctx.Err() != nil {
				return nil
			}

			c.recordFailure(stage)

			if stage != consumerFailureStageProcess {
				return err
			}

			if !waitForKafkaProcessRetry(
				ctx,
				processRetryDelay,
			) {
				return nil
			}

			processRetryDelay = nextKafkaProcessRetryDelay(
				processRetryDelay,
			)
		}
	}
}

func waitForKafkaPollRetry(
	ctx context.Context,
	delay time.Duration,
) bool {
	if delay <= 0 {
		delay = kafkaPollRetryInitialDelay
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false

	case <-timer.C:
		return true
	}
}

func nextKafkaPollRetryDelay(
	current time.Duration,
) time.Duration {
	if current <= 0 {
		return kafkaPollRetryInitialDelay
	}

	next := current * 2

	if next > kafkaPollRetryMaxDelay {
		return kafkaPollRetryMaxDelay
	}

	return next
}

func waitForKafkaProcessRetry(
	ctx context.Context,
	delay time.Duration,
) bool {
	if delay <= 0 {
		delay = kafkaProcessRetryInitialDelay
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false

	case <-timer.C:
		return true
	}
}

func nextKafkaProcessRetryDelay(
	current time.Duration,
) time.Duration {
	if current <= 0 {
		return kafkaProcessRetryInitialDelay
	}

	next := current * 2

	if next > kafkaProcessRetryMaxDelay {
		return kafkaProcessRetryMaxDelay
	}

	return next
}

func (c *KafkaRedirectEventConsumer) processRecord(
	ctx context.Context,
	processor RedirectEventProcessor,
	record *kgo.Record,
) (string, error) {
	processCtx := extractKafkaTraceContext(ctx, record)

	spanOptions := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
		),
	}

	if record != nil && strings.TrimSpace(record.Topic) != "" {
		spanOptions = append(
			spanOptions,
			trace.WithAttributes(
				attribute.String(
					"messaging.destination.name",
					record.Topic,
				),
			),
		)
	}

	processCtx, span := analyticsTracer().Start(
		processCtx,
		"redirect.analytics.process",
		spanOptions...,
	)
	defer span.End()

	event, err := redirectEventFromKafkaRecord(record)
	if err != nil {
		err = fmt.Errorf(
			"decode Kafka redirect event: %w",
			err,
		)
		markSpanError(span, err, "decode Kafka record")

		return consumerFailureStageDecode, err
	}

	if err := processor.Process(processCtx, event); err != nil {
		err = fmt.Errorf(
			"process redirect event %q: %w",
			event.EventID,
			err,
		)
		markSpanError(span, err, "process redirect event")

		return consumerFailureStageProcess, err
	}

	if err := c.client.Commit(processCtx, record); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		err = fmt.Errorf(
			"commit redirect event %q: %w",
			event.EventID,
			err,
		)
		markSpanError(span, err, "commit Kafka record")

		return consumerFailureStageCommit, err
	}

	return "", nil
}

func (c *KafkaRedirectEventConsumer) Close() {
	c.client.Close()
}

func (c *KafkaRedirectEventConsumer) recordFailure(stage string) {
	if c.metrics == nil {
		return
	}

	c.metrics.FailuresTotal.
		WithLabelValues(stage).
		Inc()
}

func (c *KafkaRedirectEventConsumer) recordProcessed() {
	if c.metrics == nil {
		return
	}

	c.metrics.ProcessedTotal.Inc()
}

func redirectEventFromKafkaRecord(
	record *kgo.Record,
) (RedirectEvent, error) {
	if record == nil {
		return RedirectEvent{}, fmt.Errorf(
			"kafka redirect event record must not be nil",
		)
	}

	var event RedirectEvent

	if err := json.Unmarshal(record.Value, &event); err != nil {
		return RedirectEvent{}, fmt.Errorf(
			"unmarshal redirect event: %w",
			err,
		)
	}

	if strings.TrimSpace(event.EventID) == "" {
		return RedirectEvent{}, fmt.Errorf(
			"redirect event id must not be empty",
		)
	}

	if event.EventType != RedirectEventType {
		return RedirectEvent{}, fmt.Errorf(
			"redirect event type must be %q",
			RedirectEventType,
		)
	}

	if strings.TrimSpace(event.ShortCode) == "" {
		return RedirectEvent{}, fmt.Errorf(
			"redirect event short code must not be empty",
		)
	}

	if event.OccurredAt.IsZero() {
		return RedirectEvent{}, fmt.Errorf(
			"redirect event occurred_at must not be zero",
		)
	}

	if string(record.Key) != event.ShortCode {
		return RedirectEvent{}, fmt.Errorf(
			"kafka record key %q does not match short code %q",
			string(record.Key),
			event.ShortCode,
		)
	}

	return event, nil
}
