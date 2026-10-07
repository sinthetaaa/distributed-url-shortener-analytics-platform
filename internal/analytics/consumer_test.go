package analytics

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestKafkaConsumerConfigFromEnvUsesDefaults(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", " kafka-1:9092, kafka-2:9092 ")
	t.Setenv("KAFKA_REDIRECT_TOPIC", "")
	t.Setenv("KAFKA_CONSUMER_GROUP", "")
	t.Setenv("KAFKA_CONSUMER_CLIENT_ID", "")
	t.Setenv("KAFKA_CONSUMER_RESET", "")

	config, err := KafkaConsumerConfigFromEnv()
	if err != nil {
		t.Fatalf(
			"KafkaConsumerConfigFromEnv returned error: %v",
			err,
		)
	}

	wantBrokers := []string{
		"kafka-1:9092",
		"kafka-2:9092",
	}

	if !reflect.DeepEqual(config.Brokers, wantBrokers) {
		t.Fatalf(
			"expected brokers %v, got %v",
			wantBrokers,
			config.Brokers,
		)
	}

	if config.Topic != RedirectEventsTopic {
		t.Fatalf(
			"expected topic %q, got %q",
			RedirectEventsTopic,
			config.Topic,
		)
	}

	if config.GroupID != defaultKafkaConsumerGroup {
		t.Fatalf(
			"expected group %q, got %q",
			defaultKafkaConsumerGroup,
			config.GroupID,
		)
	}

	if config.ClientID != defaultKafkaConsumerClientID {
		t.Fatalf(
			"expected client id %q, got %q",
			defaultKafkaConsumerClientID,
			config.ClientID,
		)
	}

	if config.ResetOffset != kafkaConsumerResetEarliest {
		t.Fatalf(
			"expected reset offset %q, got %q",
			kafkaConsumerResetEarliest,
			config.ResetOffset,
		)
	}
}

func TestKafkaConsumerConfigFromEnvUsesOverrides(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "broker:19092")
	t.Setenv("KAFKA_REDIRECT_TOPIC", "redirect-events-test")
	t.Setenv("KAFKA_CONSUMER_GROUP", "analytics-test")
	t.Setenv("KAFKA_CONSUMER_CLIENT_ID", "consumer-test")
	t.Setenv("KAFKA_CONSUMER_RESET", "latest")

	config, err := KafkaConsumerConfigFromEnv()
	if err != nil {
		t.Fatalf(
			"KafkaConsumerConfigFromEnv returned error: %v",
			err,
		)
	}

	if config.Topic != "redirect-events-test" {
		t.Fatalf("expected topic override, got %q", config.Topic)
	}

	if config.GroupID != "analytics-test" {
		t.Fatalf("expected group override, got %q", config.GroupID)
	}

	if config.ClientID != "consumer-test" {
		t.Fatalf(
			"expected client id override, got %q",
			config.ClientID,
		)
	}

	if config.ResetOffset != kafkaConsumerResetLatest {
		t.Fatalf(
			"expected reset offset %q, got %q",
			kafkaConsumerResetLatest,
			config.ResetOffset,
		)
	}
}

func TestKafkaConsumerConfigRejectsInvalidResetOffset(t *testing.T) {
	_, err := validateKafkaConsumerConfig(
		KafkaConsumerConfig{
			Brokers:     []string{"broker:9092"},
			Topic:       RedirectEventsTopic,
			GroupID:     "analytics",
			ClientID:    "consumer",
			ResetOffset: "middle",
		},
	)

	if err == nil {
		t.Fatal("expected invalid reset offset to return an error")
	}
}

func TestRedirectEventFromKafkaRecord(t *testing.T) {
	event := RedirectEvent{
		EventID:   "123e4567-e89b-42d3-a456-426614174000",
		EventType: RedirectEventType,
		ShortCode: "3ZB9CeC",
		OccurredAt: time.Date(
			2026,
			time.October,
			7,
			13,
			30,
			0,
			0,
			time.UTC,
		),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	record := &kgo.Record{
		Key:   []byte(event.ShortCode),
		Value: payload,
	}

	decoded, err := redirectEventFromKafkaRecord(record)
	if err != nil {
		t.Fatalf(
			"redirectEventFromKafkaRecord returned error: %v",
			err,
		)
	}

	assertRedirectEventsEqual(t, event, decoded)
}

func TestRedirectEventFromKafkaRecordRejectsKeyMismatch(
	t *testing.T,
) {
	event := validRedirectEventForTest()

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	record := &kgo.Record{
		Key:   []byte("different"),
		Value: payload,
	}

	if _, err := redirectEventFromKafkaRecord(record); err == nil {
		t.Fatal("expected record key mismatch to return an error")
	}
}

func TestRedirectEventFromKafkaRecordRejectsMalformedJSON(
	t *testing.T,
) {
	record := &kgo.Record{
		Key:   []byte("3ZB9CeC"),
		Value: []byte("{"),
	}

	if _, err := redirectEventFromKafkaRecord(record); err == nil {
		t.Fatal("expected malformed JSON to return an error")
	}
}
