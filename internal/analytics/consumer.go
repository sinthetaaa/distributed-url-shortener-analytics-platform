package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/observability"

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
			"Kafka consumer brokers must not be empty",
		)
	}

	topic := strings.TrimSpace(config.Topic)
	if topic == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"Kafka consumer topic must not be empty",
		)
	}

	groupID := strings.TrimSpace(config.GroupID)
	if groupID == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"Kafka consumer group id must not be empty",
		)
	}

	clientID := strings.TrimSpace(config.ClientID)
	if clientID == "" {
		return KafkaConsumerConfig{}, fmt.Errorf(
			"Kafka consumer client id must not be empty",
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
			"Kafka consumer reset offset must be %q or %q",
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

type KafkaRedirectEventConsumer struct {
	client  *kgo.Client
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
		client:  client,
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

	for {
		fetches := c.client.PollRecords(ctx, 1)

		if ctx.Err() != nil {
			return nil
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			c.recordFailure(consumerFailureStagePoll)

			return fmt.Errorf("poll Kafka redirect events: %v", errs)
		}

		records := fetches.Records()
		if len(records) == 0 {
			continue
		}

		record := records[0]

		event, err := redirectEventFromKafkaRecord(record)
		if err != nil {
			c.recordFailure(consumerFailureStageDecode)

			return fmt.Errorf(
				"decode Kafka redirect event: %w",
				err,
			)
		}

		if err := processor.Process(ctx, event); err != nil {
			c.recordFailure(consumerFailureStageProcess)

			return fmt.Errorf(
				"process redirect event %q: %w",
				event.EventID,
				err,
			)
		}

		if err := c.client.CommitRecords(ctx, record); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			c.recordFailure(consumerFailureStageCommit)

			return fmt.Errorf(
				"commit redirect event %q: %w",
				event.EventID,
				err,
			)
		}

		c.recordProcessed()
	}
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
			"Kafka redirect event record must not be nil",
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
			"Kafka record key %q does not match short code %q",
			string(record.Key),
			event.ShortCode,
		)
	}

	return event, nil
}
