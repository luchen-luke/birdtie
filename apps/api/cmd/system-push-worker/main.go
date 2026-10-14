// Explicit single bounded dispatch. This command never installs a scheduler or
// enables a provider, and prints aggregate outcomes only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	log.SetFlags(0)
	action := flag.String("action", "dispatch", "dispatch or rotate-keys")
	limit := flag.Int("limit", 25, "bounded batch 1..100")
	flag.Parse()
	cfg, e := sp.LoadConfig(os.LookupEnv)
	if e != nil {
		log.Fatal("system_push status=failed reason=invalid_configuration")
	}
	if !cfg.Enabled {
		log.Print("system_push status=disabled")
		return
	}
	if (*action != "dispatch" && *action != "rotate-keys") || *limit < 1 || *limit > 100 || flag.NArg() != 0 {
		log.Fatal("system_push status=failed reason=invalid_arguments")
	}
	ring, e := sp.LoadKeyringFile(cfg.KeyringFile)
	if e != nil {
		log.Fatal("system_push status=failed reason=keyring_unavailable")
	}
	ctx, stop := context.WithTimeout(context.Background(), 70*time.Second)
	defer stop()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		log.Fatal("system_push status=failed reason=database_unavailable")
	}
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		log.Fatal("system_push status=failed reason=database_unavailable")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		log.Fatal("system_push status=failed reason=database_unavailable")
	}
	devSetting := strings.TrimSpace(os.Getenv("BIRDTIE_DEV_PHONE_AUTH"))
	dev := os.Getenv("BIRDTIE_ENV") == "development" && (devSetting == "1" || strings.EqualFold(devSetting, "true"))
	if dev {
		host := pool.Config().ConnConfig.Host
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			log.Fatal("system_push status=failed reason=invalid_development_gate")
		}
	}
	gateway, e := postgres.NewSystemPush(postgres.New(pool, dev), ring, cfg)
	if e != nil {
		log.Fatal("system_push status=failed reason=invalid_configuration")
	}
	if *action == "rotate-keys" {
		n, e := gateway.RotateAddressKeys(ctx, *limit)
		if e != nil {
			log.Fatal("system_push status=failed reason=rotation_unavailable")
		}
		log.Printf("system_push rotated=%d", n)
		return
	}
	providers, e := sp.NewProviders(cfg)
	if e != nil {
		log.Fatal("system_push status=failed reason=provider_configuration_unavailable")
	}
	out, e := sp.RunOnce(ctx, gateway, providers, sp.RunOptions{Limit: *limit})
	b, _ := json.Marshal(out)
	log.Printf("system_push outcomes=%s", b)
	if e != nil {
		log.Fatal("system_push status=failed reason=receipt_unavailable")
	}
}
