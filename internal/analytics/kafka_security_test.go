package analytics

import "testing"

func TestKafkaSecurityConfigDefaultsToPlaintext(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")

	config, err := kafkaSecurityConfigFromEnv()
	if err != nil {
		t.Fatalf("kafkaSecurityConfigFromEnv() error = %v", err)
	}

	if config.TLSEnabled {
		t.Fatal("expected TLS to be disabled")
	}

	if config.SASLUsername != "" || config.SASLPassword != "" {
		t.Fatal("expected SASL credentials to be empty")
	}

	if options := kafkaSecurityOptions(config); len(options) != 0 {
		t.Fatalf("expected no security options, got %d", len(options))
	}
}

func TestKafkaSecurityConfigSupportsTLSOnly(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "true")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")

	config, err := kafkaSecurityConfigFromEnv()
	if err != nil {
		t.Fatalf("kafkaSecurityConfigFromEnv() error = %v", err)
	}

	if !config.TLSEnabled {
		t.Fatal("expected TLS to be enabled")
	}

	if options := kafkaSecurityOptions(config); len(options) != 1 {
		t.Fatalf("expected 1 security option, got %d", len(options))
	}
}

func TestKafkaSecurityConfigSupportsSCRAMOverTLS(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "true")
	t.Setenv("KAFKA_SASL_USERNAME", "shortscale")
	t.Setenv("KAFKA_SASL_PASSWORD", "secret")

	config, err := kafkaSecurityConfigFromEnv()
	if err != nil {
		t.Fatalf("kafkaSecurityConfigFromEnv() error = %v", err)
	}

	if config.SASLUsername != "shortscale" {
		t.Fatalf(
			"expected username %q, got %q",
			"shortscale",
			config.SASLUsername,
		)
	}

	if config.SASLPassword != "secret" {
		t.Fatal("expected SASL password to be loaded")
	}

	if options := kafkaSecurityOptions(config); len(options) != 2 {
		t.Fatalf("expected 2 security options, got %d", len(options))
	}
}

func TestKafkaSecurityConfigRejectsIncompleteCredentials(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "true")
	t.Setenv("KAFKA_SASL_USERNAME", "shortscale")
	t.Setenv("KAFKA_SASL_PASSWORD", "")

	if _, err := kafkaSecurityConfigFromEnv(); err == nil {
		t.Fatal("expected incomplete SASL credentials error")
	}
}

func TestKafkaSecurityConfigRejectsSASLWithoutTLS(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "false")
	t.Setenv("KAFKA_SASL_USERNAME", "shortscale")
	t.Setenv("KAFKA_SASL_PASSWORD", "secret")

	if _, err := kafkaSecurityConfigFromEnv(); err == nil {
		t.Fatal("expected SASL without TLS error")
	}
}

func TestKafkaSecurityConfigRejectsInvalidTLSValue(t *testing.T) {
	t.Setenv("KAFKA_TLS_ENABLED", "sometimes")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")

	if _, err := kafkaSecurityConfigFromEnv(); err == nil {
		t.Fatal("expected invalid TLS value error")
	}
}
