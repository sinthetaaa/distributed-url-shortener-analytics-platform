package analytics

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaConsumerLagReader struct {
	client  *kgo.Client
	admin   *kadm.Client
	groupID string
}

func NewKafkaConsumerLagReader(
	config KafkaConsumerConfig,
) (*KafkaConsumerLagReader, error) {
	config, err := validateKafkaConsumerConfig(config)
	if err != nil {
		return nil, err
	}

	securityOptions, err := kafkaSecurityOptionsFromEnv()
	if err != nil {
		return nil, fmt.Errorf(
			"load Kafka security configuration: %w",
			err,
		)
	}

	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID + "-lag-monitor"),
	}

	options = append(options, securityOptions...)

	client, err := kgo.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf(
			"create Kafka consumer lag client: %w",
			err,
		)
	}

	return &KafkaConsumerLagReader{
		client:  client,
		admin:   kadm.NewClient(client),
		groupID: config.GroupID,
	}, nil
}

func (r *KafkaConsumerLagReader) Lag(
	ctx context.Context,
) (int64, error) {
	lags, err := r.admin.Lag(ctx, r.groupID)
	if err != nil {
		return 0, fmt.Errorf(
			"load Kafka consumer lag: %w",
			err,
		)
	}

	groupLag, ok := lags[r.groupID]
	if !ok {
		return 0, fmt.Errorf(
			"kafka consumer lag response missing group %q",
			r.groupID,
		)
	}

	if err := groupLag.Error(); err != nil {
		return 0, fmt.Errorf(
			"load Kafka consumer lag for group %q: %w",
			r.groupID,
			err,
		)
	}

	total := groupLag.Lag.Total()
	if total < 0 {
		return 0, fmt.Errorf(
			"kafka consumer lag for group %q is negative: %d",
			r.groupID,
			total,
		)
	}

	return total, nil
}

func (r *KafkaConsumerLagReader) Close() {
	r.client.Close()
}
