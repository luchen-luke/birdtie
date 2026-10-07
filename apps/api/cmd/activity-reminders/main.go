package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One-shot runner for deployments that prefer a scheduled job, and for
// checking the same idempotent reminder query used by the API scheduler.
func main() {
	databaseURL := os.Getenv("BIRDTIE_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("activity_reminder_run status=failed stage=configuration error=missing_database_url")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("activity_reminder_run status=failed stage=pool_init duration_ms=%d error=%q", time.Since(started).Milliseconds(), err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("activity_reminder_run status=failed stage=database_ping duration_ms=%d error=%q", time.Since(started).Milliseconds(), err)
	}
	count, err := postgres.New(pool, false).EnqueueStartsSoonReminders(ctx)
	if err != nil {
		log.Fatalf("activity_reminder_run status=failed stage=enqueue duration_ms=%d error=%q", time.Since(started).Milliseconds(), err)
	}
	log.Printf("activity_reminder_run status=ok inserted=%d duration_ms=%d", count, time.Since(started).Milliseconds())
}
