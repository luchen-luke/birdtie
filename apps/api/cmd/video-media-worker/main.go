package main

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Explicitly invoked, bounded worker; it never installs a scheduler or enables
// providers. Logs contain no owner IDs, object keys, credentials or filenames.
func main() {
	log.SetFlags(0)
	runtime, err := media.VideoRuntimeFromEnvironment(os.Getenv)
	if err != nil {
		log.Fatal("video_worker status=failed reason=invalid_provider_configuration")
	}
	if !runtime.Available() {
		log.Print("video_worker status=disabled")
		return
	}
	limit := 1
	if raw := os.Getenv("BIRDTIE_VIDEO_WORKER_LIMIT"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 5 {
			log.Fatal("video_worker status=failed reason=invalid_batch_limit")
		}
	}
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		log.Fatal("video_worker status=failed reason=missing_database_configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(limit)*155*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		log.Fatal("video_worker status=failed reason=database_unavailable")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		log.Fatal("video_worker status=failed reason=database_unavailable")
	}
	// Dev sessions remain off unless the actual server-side development gate is
	// explicitly configured identically; client build environment is unrelated.
	devSetting := strings.TrimSpace(os.Getenv("BIRDTIE_DEV_PHONE_AUTH"))
	dev := os.Getenv("BIRDTIE_ENV") == "development" && (devSetting == "1" || strings.EqualFold(devSetting, "true"))
	if dev {
		host := pool.Config().ConnConfig.Host
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			log.Fatal("video_worker status=failed reason=development_requires_loopback_database")
		}
	}
	service := media.NewVideoMaterialService(postgres.New(pool, dev), runtime)
	processed, failures := 0, 0
	for processed < limit {
		worked, e := service.ProcessNext(ctx)
		if !worked && e == nil {
			break
		}
		if worked {
			processed++
		}
		if e != nil {
			failures++
			if !worked || errors.Is(e, context.Canceled) || ctx.Err() != nil {
				break
			}
		}
	}
	log.Printf("video_worker processed=%d failures=%d", processed, failures)
	if failures > 0 {
		os.Exit(1)
	}
}
