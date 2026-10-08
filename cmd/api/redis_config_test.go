package main

import "testing"

func TestRedisOptionsFromEnvUsesRedisURL(t *testing.T) {
	t.Setenv(
		"REDIS_URL",
		"rediss"+"://default:secret@cache.example.test:6379/0",
	)
	t.Setenv("REDIS_ADDR", "")

	options, err := redisOptionsFromEnv()
	if err != nil {
		t.Fatalf("redisOptionsFromEnv() error = %v", err)
	}

	if options.Addr != "cache.example.test:6379" {
		t.Fatalf("expected Redis address %q, got %q", "cache.example.test:6379", options.Addr)
	}

	if options.Username != "default" {
		t.Fatalf("expected Redis username %q, got %q", "default", options.Username)
	}

	if options.Password != "secret" {
		t.Fatal("expected Redis password to be parsed")
	}

	if options.TLSConfig == nil {
		t.Fatal("expected TLS configuration for rediss URL")
	}
}

func TestRedisOptionsFromEnvFallsBackToRedisAddr(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_ADDR", "redis:6379")

	options, err := redisOptionsFromEnv()
	if err != nil {
		t.Fatalf("redisOptionsFromEnv() error = %v", err)
	}

	if options.Addr != "redis:6379" {
		t.Fatalf("expected Redis address %q, got %q", "redis:6379", options.Addr)
	}

	if options.TLSConfig != nil {
		t.Fatal("expected local Redis connection to remain plaintext")
	}
}

func TestRedisOptionsFromEnvRequiresConfiguration(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("REDIS_ADDR", "")

	if _, err := redisOptionsFromEnv(); err == nil {
		t.Fatal("expected missing Redis configuration error")
	}
}

func TestRedisOptionsFromEnvRejectsInvalidURL(t *testing.T) {
	t.Setenv("REDIS_URL", "not-a-redis-url")
	t.Setenv("REDIS_ADDR", "")

	if _, err := redisOptionsFromEnv(); err == nil {
		t.Fatal("expected invalid REDIS_URL error")
	}
}
