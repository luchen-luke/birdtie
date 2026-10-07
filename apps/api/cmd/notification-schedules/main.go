// A separately invoked deterministic worker. No inference is started here.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"time"
)

type scheduleCycleStore interface {
	ns.RunnerStore
	ns.ReminderStore
}
type connector func(context.Context, string) (scheduleCycleStore, func(), error)

var errConfiguration = errors.New("notification_schedule_run configuration_invalid")
var errRun = errors.New("notification_schedule_run unavailable")

func connect(ctx context.Context, raw string) (scheduleCycleStore, func(), error) {
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil {
		return nil, nil, errConfiguration
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, nil, errRun
	}
	if e = pool.Ping(ctx); e != nil {
		pool.Close()
		return nil, nil, errRun
	}
	return postgres.New(pool, false), pool.Close, nil
}
func runWithConnector(ctx context.Context, args []string, env func(string) string, out io.Writer, open connector) error {
	if ctx == nil || env == nil || out == nil || open == nil {
		return errConfiguration
	}
	flags := flag.NewFlagSet("notification-schedules", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	owners := flags.Int("owners", 100, "maximum explicitly configured owners per invocation")
	deadline := flags.Duration("deadline", 15*time.Second, "independent per-stage deadline")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *owners < 1 || *owners > ns.MaxOwnersPerRun || *deadline < time.Second || *deadline > 30*time.Second {
		return errConfiguration
	}
	raw := env("BIRDTIE_DATABASE_URL")
	if raw == "" {
		return errConfiguration
	}
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
		if env(key) != "" {
			return errConfiguration
		}
	}
	startCtx, cancel := context.WithTimeout(ctx, *deadline)
	store, close, e := open(startCtx, raw)
	cancel()
	if e != nil || store == nil || close == nil {
		return errRun
	}
	defer close()
	start := time.Now()
	result, e := ns.RunIndependentCycle(ctx, store, store, *owners, *deadline)
	reminderCount := "UNKNOWN"
	if result.RemindersOK {
		reminderCount = fmt.Sprint(result.Reminders)
	}
	digestCounts := "owners=UNKNOWN slots=UNKNOWN inbox_inserted=UNKNOWN skipped=UNKNOWN"
	if result.DigestOK {
		digestCounts = fmt.Sprintf("owners=%d slots=%d inbox_inserted=%d skipped=%d", result.Digest.Owners, result.Digest.Slots, result.Digest.Delivered, result.Digest.Discarded)
	}
	_, _ = fmt.Fprintf(out, "notification_schedule_run reminders_ok=%t reminder_inserted=%s digest_ok=%t digest_counts_known=%t %s duration_ms=%d\n", result.RemindersOK, reminderCount, result.DigestOK, result.DigestOK, digestCounts, time.Since(start).Milliseconds())
	if e != nil {
		return errRun
	}
	return nil
}
func run(ctx context.Context, args []string, env func(string) string, out io.Writer) error {
	return runWithConnector(ctx, args, env, out, connect)
}
func main() {
	if e := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
