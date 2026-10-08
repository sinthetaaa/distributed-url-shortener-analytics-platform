package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/twmb/franz-go/pkg/kgo"
)

const defaultKafkaClientID = "shortscale-api"

type RedirectEventPublisher interface {
	Publish(context.Context, RedirectEvent) error
	Close()
}

type KafkaProducerConfig struct {
	Brokers  []string
	Topic    string
	ClientID string
}

func KafkaProducerConfigFromEnv() (KafkaProducerConfig, error) {
	config := KafkaProducerConfig{
		Brokers:  splitAndTrim(os.Getenv("KAFKA_BROKERS")),
		Topic:    strings.TrimSpace(os.Getenv("KAFKA_REDIRECT_TOPIC")),
		ClientID: strings.TrimSpace(os.Getenv("KAFKA_CLIENT_ID")),
	}

	if config.Topic == "" {
		config.Topic = RedirectEventsTopic
	}

	if config.ClientID == "" {
		config.ClientID = defaultKafkaClientID
	}

	return validateKafkaProducerConfig(config)
}

func validateKafkaProducerConfig(
	config KafkaProducerConfig,
) (KafkaProducerConfig, error) {
	brokers := make([]string, 0, len(config.Brokers))

	for _, broker := range config.Brokers {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			brokers = append(brokers, broker)
		}
	}

	if len(brokers) == 0 {
		return KafkaProducerConfig{}, fmt.Errorf("kafka brokers must not be empty")
	}

	topic := strings.TrimSpace(config.Topic)
	if topic == "" {
		return KafkaProducerConfig{}, fmt.Errorf("kafka topic must not be empty")
	}

	clientID := strings.TrimSpace(config.ClientID)
	if clientID == "" {
		return KafkaProducerConfig{}, fmt.Errorf("kafka client id must not be empty")
	}

	return KafkaProducerConfig{
		Brokers:  brokers,
		Topic:    topic,
		ClientID: clientID,
	}, nil
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

type KafkaRedirectEventProducer struct {
	client *kgo.Client
	topic  string
}

func NewKafkaRedirectEventProducer(
	config KafkaProducerConfig,
) (*KafkaRedirectEventProducer, error) {
	config, err := validateKafkaProducerConfig(config)
	if err != nil {
		return nil, err
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.DefaultProduceTopic(config.Topic),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka client: %w", err)
	}

	return &KafkaRedirectEventProducer{
		client: client,
		topic:  config.Topic,
	}, nil
}

func (p *KafkaRedirectEventProducer) Publish(
	ctx context.Context,
	event RedirectEvent,
) error {
	publishCtx, span := analyticsTracer().Start(
		ctx,
		"redirect.analytics.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String(
				"messaging.destination.name",
				p.topic,
			),
		),
	)
	defer span.End()

	record, err := kafkaRecordForRedirectEvent(p.topic, event)
	if err != nil {
		markSpanError(span, err, "build Kafka record")
		return err
	}

	injectKafkaTraceContext(publishCtx, record)

	if err := p.client.ProduceSync(publishCtx, record).FirstErr(); err != nil {
		err = fmt.Errorf("produce redirect event: %w", err)
		markSpanError(span, err, "publish Kafka record")
		return err
	}

	return nil
}

func (p *KafkaRedirectEventProducer) Close() {
	p.client.Close()
}

func kafkaRecordForRedirectEvent(
	topic string,
	event RedirectEvent,
) (*kgo.Record, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("kafka topic must not be empty")
	}

	if strings.TrimSpace(event.EventID) == "" {
		return nil, fmt.Errorf("redirect event id must not be empty")
	}

	if event.EventType != RedirectEventType {
		return nil, fmt.Errorf(
			"redirect event type must be %q",
			RedirectEventType,
		)
	}
	if strings.TrimSpace(event.ShortCode) == "" {
		return nil, fmt.Errorf("redirect event short code must not be empty")
	}

	if event.OccurredAt.IsZero() {
		return nil, fmt.Errorf("redirect event occurred_at must not be zero")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal redirect event: %w", err)
	}

	return &kgo.Record{
		Topic:     topic,
		Key:       []byte(event.ShortCode),
		Value:     payload,
		Timestamp: event.OccurredAt,
	}, nil
}
