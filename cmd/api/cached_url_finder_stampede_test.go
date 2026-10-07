package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	urlcache "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/cache"
	database "github.com/sinthetaaa/distributed-url-shortener-analytics-platform/internal/database/generated"
)

const stampedeRequestCount = 50

type stampedeCache struct{}

func (stampedeCache) Get(
	_ context.Context,
	_ string,
) (string, error) {
	return "", urlcache.ErrMiss
}

func (stampedeCache) Set(
	_ context.Context,
	_ string,
	_ string,
) error {
	return nil
}

type countingURLSource struct {
	calls atomic.Int64
}

func (s *countingURLSource) GetURLByShortCode(
	_ context.Context,
	shortCode string,
) (database.Url, error) {
	s.calls.Add(1)

	// Keep the shared source lookup in flight long enough for concurrent
	// requests to join the same singleflight operation.
	time.Sleep(50 * time.Millisecond)

	return database.Url{
		ShortCode:   shortCode,
		OriginalUrl: "https://example.com/" + shortCode,
	}, nil
}

func newStampedeTestFinder(
	source urlFinder,
) *cachedURLFinder {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	return newCachedURLFinder(
		logger,
		stampedeCache{},
		source,
	)
}

func TestCachedURLFinderCoalescesConcurrentCacheMisses(t *testing.T) {
	source := &countingURLSource{}
	finder := newStampedeTestFinder(source)

	start := make(chan struct{})

	var ready sync.WaitGroup
	ready.Add(stampedeRequestCount)

	var requests sync.WaitGroup
	requests.Add(stampedeRequestCount)

	errs := make(chan error, stampedeRequestCount)

	for i := 0; i < stampedeRequestCount; i++ {
		go func() {
			defer requests.Done()

			ready.Done()
			<-start

			found, err := finder.GetURLByShortCode(
				context.Background(),
				"hot1234",
			)
			if err != nil {
				errs <- err
				return
			}

			if found.OriginalUrl != "https://example.com/hot1234" {
				errs <- errors.New("unexpected URL returned")
			}
		}()
	}

	ready.Wait()
	close(start)

	requests.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent redirect lookup failed: %v", err)
	}

	databaseCalls := source.calls.Load()

	t.Logf(
		"%d concurrent cache misses produced %d database lookup(s)",
		stampedeRequestCount,
		databaseCalls,
	)

	if databaseCalls != 1 {
		t.Fatalf(
			"expected exactly 1 shared database lookup, got %d",
			databaseCalls,
		)
	}
}

func TestCachedURLFinderCoalescesPerShortCode(t *testing.T) {
	source := &countingURLSource{}
	finder := newStampedeTestFinder(source)

	const requestsPerCode = 25

	shortCodes := []string{
		"hot1234",
		"hot5678",
	}

	start := make(chan struct{})

	var ready sync.WaitGroup
	ready.Add(requestsPerCode * len(shortCodes))

	var requests sync.WaitGroup
	requests.Add(requestsPerCode * len(shortCodes))

	errs := make(
		chan error,
		requestsPerCode*len(shortCodes),
	)

	for _, shortCode := range shortCodes {
		shortCode := shortCode

		for i := 0; i < requestsPerCode; i++ {
			go func() {
				defer requests.Done()

				ready.Done()
				<-start

				found, err := finder.GetURLByShortCode(
					context.Background(),
					shortCode,
				)
				if err != nil {
					errs <- err
					return
				}

				expectedURL := "https://example.com/" + shortCode

				if found.OriginalUrl != expectedURL {
					errs <- errors.New("unexpected URL returned")
				}
			}()
		}
	}

	ready.Wait()
	close(start)

	requests.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent redirect lookup failed: %v", err)
	}

	databaseCalls := source.calls.Load()

	t.Logf(
		"%d requests across %d short codes produced %d database lookup(s)",
		requestsPerCode*len(shortCodes),
		len(shortCodes),
		databaseCalls,
	)

	if databaseCalls != int64(len(shortCodes)) {
		t.Fatalf(
			"expected one database lookup per short code (%d total), got %d",
			len(shortCodes),
			databaseCalls,
		)
	}
}

type deadlineRecordingURLSource struct {
	hadDeadline atomic.Bool
}

func (s *deadlineRecordingURLSource) GetURLByShortCode(
	ctx context.Context,
	shortCode string,
) (database.Url, error) {
	_, hasDeadline := ctx.Deadline()
	s.hadDeadline.Store(hasDeadline)

	return database.Url{
		ShortCode:   shortCode,
		OriginalUrl: "https://example.com/" + shortCode,
	}, nil
}

func TestCachedURLFinderSharedDatabaseLookupHasDeadline(t *testing.T) {
	source := &deadlineRecordingURLSource{}
	finder := newStampedeTestFinder(source)

	_, err := finder.GetURLByShortCode(
		context.Background(),
		"hot1234",
	)
	if err != nil {
		t.Fatalf("GetURLByShortCode returned error: %v", err)
	}

	if !source.hadDeadline.Load() {
		t.Fatal("expected shared database lookup context to have a deadline")
	}
}

type cancellationIsolationSource struct {
	started              chan struct{}
	release              chan struct{}
	finished             chan struct{}
	observedCancellation atomic.Bool
}

func newCancellationIsolationSource() *cancellationIsolationSource {
	return &cancellationIsolationSource{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (s *cancellationIsolationSource) GetURLByShortCode(
	ctx context.Context,
	shortCode string,
) (database.Url, error) {
	close(s.started)

	select {
	case <-ctx.Done():
		s.observedCancellation.Store(true)

	case <-s.release:
	}

	close(s.finished)

	return database.Url{
		ShortCode:   shortCode,
		OriginalUrl: "https://example.com/" + shortCode,
	}, nil
}

func TestCachedURLFinderCallerCancellationDoesNotCancelSharedLookup(
	t *testing.T,
) {
	source := newCancellationIsolationSource()
	finder := newStampedeTestFinder(source)

	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan error, 1)

	go func() {
		_, err := finder.GetURLByShortCode(
			ctx,
			"hot1234",
		)
		result <- err
	}()

	<-source.started
	cancel()

	err := <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected caller to return context.Canceled, got %v",
			err,
		)
	}

	select {
	case <-source.finished:
		t.Fatal(
			"shared database lookup stopped when caller was canceled",
		)

	case <-time.After(20 * time.Millisecond):
	}

	close(source.release)

	select {
	case <-source.finished:

	case <-time.After(time.Second):
		t.Fatal("shared database lookup did not finish after release")
	}

	if source.observedCancellation.Load() {
		t.Fatal(
			"shared database lookup observed caller cancellation",
		)
	}
}
