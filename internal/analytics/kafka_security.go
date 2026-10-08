package analytics

import (
	"crypto/tls"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type kafkaSecurityConfig struct {
	TLSEnabled   bool
	SASLUsername string
	SASLPassword string
}

func kafkaSecurityConfigFromEnv() (kafkaSecurityConfig, error) {
	tlsEnabled := false

	if raw := strings.TrimSpace(os.Getenv("KAFKA_TLS_ENABLED")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return kafkaSecurityConfig{}, fmt.Errorf(
				"KAFKA_TLS_ENABLED must be true or false: %w",
				err,
			)
		}

		tlsEnabled = parsed
	}

	username := strings.TrimSpace(
		os.Getenv("KAFKA_SASL_USERNAME"),
	)
	password := os.Getenv("KAFKA_SASL_PASSWORD")

	hasUsername := username != ""
	hasPassword := password != ""

	if hasUsername != hasPassword {
		return kafkaSecurityConfig{}, fmt.Errorf(
			"KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD must be set together",
		)
	}

	if hasUsername && !tlsEnabled {
		return kafkaSecurityConfig{}, fmt.Errorf(
			"Kafka SASL credentials require KAFKA_TLS_ENABLED=true",
		)
	}

	return kafkaSecurityConfig{
		TLSEnabled:   tlsEnabled,
		SASLUsername: username,
		SASLPassword: password,
	}, nil
}

func kafkaSecurityOptionsFromEnv() ([]kgo.Opt, error) {
	config, err := kafkaSecurityConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return kafkaSecurityOptions(config), nil
}

func kafkaSecurityOptions(
	config kafkaSecurityConfig,
) []kgo.Opt {
	options := make([]kgo.Opt, 0, 2)

	if config.TLSEnabled {
		options = append(
			options,
			kgo.DialTLSConfig(
				&tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			),
		)
	}

	if config.SASLUsername != "" {
		mechanism := scram.Auth{
			User: config.SASLUsername,
			Pass: config.SASLPassword,
		}.AsSha256Mechanism()

		options = append(
			options,
			kgo.SASL(mechanism),
		)
	}

	return options
}
