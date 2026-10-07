package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestKafkaProducerConfigFromEnvUsesDefaults(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", " kafka-1:9092, kafka-2:9092 ")
	t.Setenv("KAFKA_REDIRECT_TOPIC", "")
	t.Setenv("KAFKA_CLIENT_ID", "")

	config, err := KafkaProducerConfigFromEnv()
	if err != nil {
		t.Fatalf("KafkaProducerConfigFromEnv returned error: %v", err)
	}

	wantBrokers := []string{"kafka-1:9092", "kafka-2:9092"}
	if !reflect.DeepEqual(config.Brokers, wantBrokers) {
		t.Fatalf("expected brokers %v, got %v", wantBrokers, config.Brokers)
	}

	if config.Topic != RedirectEventsTopic {
		t.Fatalf("expected topic %q, got %q", RedirectEventsTopic, config.Topic)
	}

	if config.ClientID != defaultKafkaClientID {
		t.Fatalf("expected client id %q, got %q", defaultKafkaClientID, config.ClientID)
	}
}

func TestKafkaProducerConfigFromEnvUsesOverrides(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "broker:19092")
	t.Setenv("KAFKA_REDIRECT_TOPIC", "redirect-events-test")
	t.Setenv("KAFKA_CLIENT_ID", "shortscale-test")

	config, err := KafkaProducerConfigFromEnv()
	if err != nil {
		t.Fatalf("KafkaProducerConfigFromEnv returned error: %v", err)
	}

	if config.Topic != "redirect-events-test" {
		t.Fatalf("expected topic override, got %q", config.Topic)
	}

	if config.ClientID != "shortscale-test" {
		t.Fatalf("expected client id override, got %q", config.ClientID)
	}
}

func TestKafkaProducerConfigFromEnvRequiresBrokers(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("KAFKA_REDIRECT_TOPIC", "")
	t.Setenv("KAFKA_CLIENT_ID", "")

	if _, err := KafkaProducerConfigFromEnv(); err == nil {
		t.Fatal("expected missing KAFKA_BROKERS to return an error")
	}
}

func TestKafkaRecordForRedirectEvent(t *testing.T) {
	occurredAt := time.Date(
		2026,
		time.October,
		7,
		12,
		30,
		45,
		123000000,
		time.UTC,
	)

	event := RedirectEvent{
		EventID:    "123e4567-e89b-42d3-a456-426614174000",
		EventType:  RedirectEventType,
		ShortCode:  "3ZB9CeC",
		OccurredAt: occurredAt,
	}

	record, err := kafkaRecordForRedirectEvent(RedirectEventsTopic, event)
	if err != nil {
		t.Fatalf("kafkaRecordForRedirectEvent returned error: %v", err)
	}

	if record.Topic != RedirectEventsTopic {
		t.Fatalf("expected topic %q, got %q", RedirectEventsTopic, record.Topic)
	}

	if string(record.Key) != event.ShortCode {
		t.Fatalf("expected record key %q, got %q", event.ShortCode, string(record.Key))
	}

	if !record.Timestamp.Equal(occurredAt) {
		t.Fatalf("expected timestamp %s, got %s", occurredAt, record.Timestamp)
	}

	var decoded RedirectEvent
	if err := json.Unmarshal(record.Value, &decoded); err != nil {
		t.Fatalf("decode record value: %v", err)
	}

	assertRedirectEventsEqual(t, event, decoded)
}

func TestKafkaRecordForRedirectEventRejectsInvalidEvent(t *testing.T) {
	event := validRedirectEventForTest()
	event.ShortCode = ""

	if _, err := kafkaRecordForRedirectEvent(RedirectEventsTopic, event); err == nil {
		t.Fatal("expected empty short code to return an error")
	}
}

func TestKafkaRedirectEventProducerIntegration(t *testing.T) {
	broker := os.Getenv("SHORTSCALE_KAFKA_INTEGRATION_BROKER")
	if broker == "" {
		t.Skip("SHORTSCALE_KAFKA_INTEGRATION_BROKER is not set")
	}

	event, err := NewRedirectEvent(
		fmt.Sprintf("phase9c-%d", time.Now().UnixNano()),
	)
	if err != nil {
		t.Fatalf("create redirect event: %v", err)
	}

	producer, err := NewKafkaRedirectEventProducer(
		KafkaProducerConfig{
			Brokers:  []string{broker},
			Topic:    RedirectEventsTopic,
			ClientID: "shortscale-phase9c-test",
		},
	)
	if err != nil {
		t.Fatalf("create Kafka producer: %v", err)
	}
	t.Cleanup(producer.Close)

	produceCtx, produceCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer produceCancel()

	if err := producer.Publish(produceCtx, event); err != nil {
		t.Fatalf("publish redirect event: %v", err)
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(broker),
		kgo.ConsumeTopics(RedirectEventsTopic),
		kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("create Kafka integration consumer: %v", err)
	}
	t.Cleanup(consumer.Close)

	consumeCtx, consumeCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer consumeCancel()

	for consumeCtx.Err() == nil {
		fetches := consumer.PollRecords(consumeCtx, 100)

		if consumeCtx.Err() != nil {
			break
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			t.Fatalf("consume Kafka records: %v", errs)
		}

		found := false

		fetches.EachRecord(func(record *kgo.Record) {
			if found {
				return
			}

			var decoded RedirectEvent
			if err := json.Unmarshal(record.Value, &decoded); err != nil {
				return
			}

			if decoded.EventID != event.EventID {
				return
			}

			if string(record.Key) != event.ShortCode {
				t.Fatalf(
					"expected consumed key %q, got %q",
					event.ShortCode,
					string(record.Key),
				)
			}

			assertRedirectEventsEqual(t, event, decoded)
			found = true
		})

		if found {
			return
		}
	}

	t.Fatalf(
		"did not consume produced event %q before timeout: %v",
		event.EventID,
		consumeCtx.Err(),
	)
}

func validRedirectEventForTest() RedirectEvent {
	return RedirectEvent{
		EventID:    "123e4567-e89b-42d3-a456-426614174000",
		EventType:  RedirectEventType,
		ShortCode:  "3ZB9CeC",
		OccurredAt: time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC),
	}
}

func assertRedirectEventsEqual(
	t *testing.T,
	want RedirectEvent,
	got RedirectEvent,
) {
	t.Helper()

	if got.EventID != want.EventID {
		t.Fatalf("expected event id %q, got %q", want.EventID, got.EventID)
	}

	if got.EventType != want.EventType {
		t.Fatalf("expected event type %q, got %q", want.EventType, got.EventType)
	}

	if got.ShortCode != want.ShortCode {
		t.Fatalf("expected short code %q, got %q", want.ShortCode, got.ShortCode)
	}

	if !got.OccurredAt.Equal(want.OccurredAt) {
		t.Fatalf("expected occurred_at %s, got %s", want.OccurredAt, got.OccurredAt)
	}
}

func TestKafkaRedirectEventProducerAllowsBoundedIdempotentCancellation(
	t *testing.T,
) {
	producer, err := NewKafkaRedirectEventProducer(
		KafkaProducerConfig{
			Brokers: []string{
				"127.0.0.1:1",
			},
			Topic:    RedirectEventsTopic,
			ClientID: "shortscale-cancellation-test",
		},
	)
	if err != nil {
		t.Fatalf(
			"create Kafka redirect event producer: %v",
			err,
		)
	}
	t.Cleanup(producer.Close)

	allowCancellation, ok := producer.client.
		OptValue(kgo.AllowIdempotentProduceCancellation).(bool)
	if !ok {
		t.Fatal(
			"expected AllowIdempotentProduceCancellation to expose a bool",
		)
	}

	if !allowCancellation {
		t.Fatal(
			"expected idempotent produce cancellation to be enabled",
		)
	}

	idempotencyDisabled, ok := producer.client.
		OptValue(kgo.DisableIdempotentWrite).(bool)
	if !ok {
		t.Fatal(
			"expected DisableIdempotentWrite to expose a bool",
		)
	}

	if idempotencyDisabled {
		t.Fatal(
			"expected Kafka idempotent writes to remain enabled",
		)
	}
}
