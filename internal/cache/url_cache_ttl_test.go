package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

const ttlCharacterizationEntries = 50

type ttlRecordingStore struct {
	mu   sync.Mutex
	ttls []time.Duration
}

func (s *ttlRecordingStore) Get(
	_ context.Context,
	_ string,
) (string, error) {
	return "", ErrMiss
}

func (s *ttlRecordingStore) Set(
	_ context.Context,
	_ string,
	_ string,
	ttl time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ttls = append(s.ttls, ttl)

	return nil
}

func TestURLCacheJitterSpreadsExpiryWindow(t *testing.T) {
	store := &ttlRecordingStore{}

	cache := &URLCache{
		store:     store,
		ttl:       time.Hour,
		ttlForKey: jitteredTTL,
	}

	for i := 0; i < ttlCharacterizationEntries; i++ {
		err := cache.Set(
			context.Background(),
			fmt.Sprintf("hot-%02d", i),
			"https://example.com/hot",
		)
		if err != nil {
			t.Fatalf("Set returned error: %v", err)
		}
	}

	if len(store.ttls) != ttlCharacterizationEntries {
		t.Fatalf(
			"expected %d recorded TTLs, got %d",
			ttlCharacterizationEntries,
			len(store.ttls),
		)
	}

	minTTL := time.Hour - 6*time.Minute
	maxTTL := time.Hour + 6*time.Minute

	distinctTTLs := make(map[time.Duration]struct{})

	observedMin := store.ttls[0]
	observedMax := store.ttls[0]

	for _, ttl := range store.ttls {
		if ttl < minTTL || ttl > maxTTL {
			t.Fatalf(
				"expected TTL inside [%s, %s], got %s",
				minTTL,
				maxTTL,
				ttl,
			)
		}

		distinctTTLs[ttl] = struct{}{}

		if ttl < observedMin {
			observedMin = ttl
		}

		if ttl > observedMax {
			observedMax = ttl
		}
	}

	t.Logf(
		"%d cache writes produced %d distinct TTL values; observed range %s to %s",
		ttlCharacterizationEntries,
		len(distinctTTLs),
		observedMin,
		observedMax,
	)

	if len(distinctTTLs) <= 1 {
		t.Fatalf(
			"expected per-key TTL jitter to spread expirations, got %d distinct TTL value(s)",
			len(distinctTTLs),
		)
	}
}

func TestJitteredTTLIsDeterministicPerShortCode(t *testing.T) {
	const shortCode = "hot1234"

	first := jitteredTTL(time.Hour, shortCode)

	for i := 0; i < 20; i++ {
		got := jitteredTTL(time.Hour, shortCode)

		if got != first {
			t.Fatalf(
				"expected deterministic TTL %s for %q, got %s",
				first,
				shortCode,
				got,
			)
		}
	}
}

func TestJitteredTTLStaysWithinTenPercent(t *testing.T) {
	baseTTL := time.Hour
	minTTL := baseTTL - 6*time.Minute
	maxTTL := baseTTL + 6*time.Minute

	for i := 0; i < 1000; i++ {
		shortCode := fmt.Sprintf("url-%04d", i)

		ttl := jitteredTTL(baseTTL, shortCode)

		if ttl < minTTL || ttl > maxTTL {
			t.Fatalf(
				"TTL for %q outside expected range: got %s, expected [%s, %s]",
				shortCode,
				ttl,
				minTTL,
				maxTTL,
			)
		}
	}
}

func TestJitteredTTLPreservesNonPositiveTTL(t *testing.T) {
	tests := []time.Duration{
		0,
		-time.Second,
	}

	for _, ttl := range tests {
		got := jitteredTTL(ttl, "abc1234")

		if got != ttl {
			t.Fatalf(
				"expected TTL %s to remain unchanged, got %s",
				ttl,
				got,
			)
		}
	}
}
