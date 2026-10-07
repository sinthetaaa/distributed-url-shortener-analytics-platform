package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateAndGetURL(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping database integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	queries := New(pool)

	shortCode := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	originalURL := "https://example.com/integration-test"

	created, err := queries.CreateURL(ctx, CreateURLParams{
		ShortCode:   shortCode,
		OriginalUrl: originalURL,
		ExpiresAt:   pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		t.Fatalf("create URL: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM urls WHERE id = $1", created.ID); err != nil {
			t.Errorf("clean up URL: %v", err)
		}
	})

	if created.ID == 0 {
		t.Error("expected generated ID to be non-zero")
	}

	if created.ShortCode != shortCode {
		t.Errorf("expected short code %q, got %q", shortCode, created.ShortCode)
	}

	if created.OriginalUrl != originalURL {
		t.Errorf("expected original URL %q, got %q", originalURL, created.OriginalUrl)
	}

	if !created.CreatedAt.Valid {
		t.Error("expected created_at to be valid")
	}

	if created.ExpiresAt.Valid {
		t.Error("expected expires_at to be NULL")
	}

	found, err := queries.GetURLByShortCode(ctx, shortCode)
	if err != nil {
		t.Fatalf("get URL by short code: %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("expected ID %d, got %d", created.ID, found.ID)
	}

	if found.ShortCode != shortCode {
		t.Errorf("expected short code %q, got %q", shortCode, found.ShortCode)
	}

	if found.OriginalUrl != originalURL {
		t.Errorf("expected original URL %q, got %q", originalURL, found.OriginalUrl)
	}
}
