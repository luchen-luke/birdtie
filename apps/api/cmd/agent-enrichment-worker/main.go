package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentworker"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"os"
	"time"
)

// A separately invoked bounded job, with no installation of a scheduler or
// flag/permission change. Production scheduling/operations are a separate gate.
func main() {
	log.SetFlags(0)
	uri := os.Getenv("BIRDTIE_DATABASE_URL")
	if uri == "" {
		log.Fatal("agent_run_batch status=failed reason=missing_database_configuration")
	}
	cfg, e := agentfeature.LoadConfig(os.LookupEnv)
	if e != nil {
		log.Fatal("agent_run_batch status=failed reason=invalid_feature_configuration")
	}
	flags, e := agentfeature.NewController(cfg)
	if e != nil {
		log.Fatal("agent_run_batch status=failed reason=invalid_feature_configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, uri)
	if e != nil {
		log.Fatal("agent_run_batch status=failed reason=database_unavailable")
	}
	defer pool.Close()
	if e = pool.Ping(ctx); e != nil {
		log.Fatal("agent_run_batch status=failed reason=database_unavailable")
	}
	// Dev phone remains disabled in this executable. Local native fixtures can
	// construct the same backend explicitly with synthetic dev Session support.
	backend := postgres.NewAgentRuns(postgres.New(pool, false), flags)
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		log.Fatal("agent_run_batch status=failed reason=worker_identity_unavailable")
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	worker := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	result, e := agentenrichmentworker.RunOnce(ctx, backend, worker, 25)
	if e != nil {
		log.Fatal("agent_run_batch status=failed reason=native_runtime_unavailable")
	}
	raw, e := json.Marshal(result)
	if e != nil {
		log.Fatal("agent_run_batch status=failed reason=invalid_summary")
	}
	log.Printf("agent_run_batch status=ok summary=%s", raw)
}
