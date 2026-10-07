// This is one-shot, trusted local control maintenance, not an enrichment
// consumer, human authorization endpoint or deployed scheduler.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type settings struct {
	subject actorref.PrincipalRef
	handler agentoutbox.HandlerVersion
	batch   int
	timeout time.Duration
}

func parseSettings(args []string) (settings, bool) {
	fs := flag.NewFlagSet("agent-outbox-control", flag.ContinueOnError)
	// flag errors contain supplied values; they must never reach logs.
	fs.SetOutput(io.Discard)
	local := fs.Bool("local-development-only", false, "")
	subject := fs.String("subject", "", "")
	handler := fs.String("handler", string(agentoutbox.HandlerV1), "")
	batch := fs.Int("batch", 25, "")
	timeout := fs.Duration("timeout", 15*time.Second, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || !*local {
		return settings{}, false
	}
	ref, e := actorref.ParsePrincipal("PERSON", *subject)
	if e != nil || ref.ID != *subject || ref.ID == "00000000-0000-0000-0000-000000000000" ||
		agentoutbox.ValidateHandlerVersion(agentoutbox.HandlerVersion(*handler)) != nil ||
		*batch < 1 || *batch > agentoutboxmaintenance.MaxBatch || *timeout < agentoutboxmaintenance.MinTimeout || *timeout > agentoutboxmaintenance.MaxTimeout {
		return settings{}, false
	}
	return settings{ref, agentoutbox.HandlerVersion(*handler), *batch, *timeout}, true
}

func localHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func localPoolConfig(raw string) (*pgxpool.Config, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return nil, false
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil || !localHost(cfg.ConnConfig.Host) {
		return nil, false
	}
	if cfg.ConnConfig.Host == "localhost" {
		cfg.ConnConfig.Host = "127.0.0.1"
	}
	for _, f := range cfg.ConnConfig.Fallbacks {
		if f == nil || !localHost(f.Host) {
			return nil, false
		}
		if f.Host == "localhost" {
			f.Host = "127.0.0.1"
		}
	}
	// No custom DNS name or non-loopback dial is allowed, including fallbacks.
	cfg.ConnConfig.LookupFunc = func(ctx context.Context, host string) ([]string, error) {
		if !localHost(host) {
			return nil, fmt.Errorf("local database address required")
		}
		if host == "localhost" {
			return []string{"127.0.0.1"}, nil
		}
		return []string{host}, nil
	}
	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, e := net.SplitHostPort(address)
		if e != nil || network != "tcp" || !localHost(host) {
			return nil, fmt.Errorf("local database address required")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	return cfg, true
}

func newWorkerID() (string, error) {
	var raw [16]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return "", e
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

type storeFactory func(context.Context, *pgxpool.Config) (agentoutboxmaintenance.Store, func(), error)

func nativeStore(ctx context.Context, cfg *pgxpool.Config) (agentoutboxmaintenance.Store, func(), error) {
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, nil, e
	}
	if e = pool.Ping(ctx); e != nil {
		pool.Close()
		return nil, nil, e
	}
	return postgres.New(pool, false), pool.Close, nil
}

func runCommand(parent context.Context, args []string, getenv func(string) string, w io.Writer, factory storeFactory) int {
	output := agentoutboxmaintenance.NewReport("STOPPED", "CONFIGURATION", "INVALID_CONFIGURATION")
	emit := func(code int) int {
		if json.NewEncoder(w).Encode(output) != nil {
			return 1
		}
		return code
	}
	opts, ok := parseSettings(args)
	if !ok || parent == nil || getenv == nil || factory == nil {
		return emit(2)
	}
	cfg, ok := localPoolConfig(getenv("BIRDTIE_DATABASE_URL"))
	if !ok {
		return emit(2)
	}
	worker, e := newWorkerID()
	if e != nil {
		output = agentoutboxmaintenance.NewReport("STOPPED", "CONFIGURATION", "WORKER_UNAVAILABLE")
		return emit(1)
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
	s, closeStore, e := factory(ctx, cfg)
	if closeStore != nil {
		defer closeStore()
	}
	if e != nil {
		output = agentoutboxmaintenance.NewReport("STOPPED", "DATABASE", "DATABASE_UNAVAILABLE")
		return emit(1)
	}
	output = agentoutboxmaintenance.Run(ctx, s, agentoutboxmaintenance.Options{Subject: opts.subject, WorkerID: worker, Handler: opts.handler, Batch: opts.batch, Timeout: opts.timeout})
	if output.Status != "FINISHED" {
		return emit(1)
	}
	return emit(0)
}

func main() {
	os.Exit(runCommand(context.Background(), os.Args[1:], os.Getenv, os.Stdout, nativeStore))
}
