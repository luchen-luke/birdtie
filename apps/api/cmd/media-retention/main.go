// Explicit bounded local-disposable maintenance; defaults to disabled.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func retentionConfig(raw, environment, disposable, database, marker string) (*pgxpool.Config, error) {
	fail := postgres.ErrMediaRetentionGuard
	if environment != "development" || disposable != "1" || !postgres.ValidMediaRetentionOwnership(database, marker) {
		return nil, fail
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "postgres" || u.User == nil || u.Fragment != "" || u.Opaque != "" || u.Path != "/"+database {
		return nil, fail
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fail
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(q) != 1 || len(q["sslmode"]) != 1 || q.Get("sslmode") != "disable" {
		return nil, fail
	}
	c, e := pgxpool.ParseConfig(raw)
	if e != nil || c.ConnConfig.TLSConfig != nil || len(c.ConnConfig.Fallbacks) != 0 || c.ConnConfig.Host != u.Hostname() || c.ConnConfig.Database != database {
		return nil, fail
	}
	for k := range c.ConnConfig.RuntimeParams {
		if k != "application_name" {
			return nil, fail
		}
	}
	c.ConnConfig.RuntimeParams["application_name"] = "birdtie-owned-human-media-retention"
	c.MaxConns = 1
	c.MinConns = 0
	return c, nil
}
func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	enabled := getenv("BIRDTIE_MEDIA_RETENTION_ENABLED")
	if enabled == "" || enabled == "0" {
		return json.NewEncoder(out).Encode(map[string]any{"status": "DISABLED", "productionScheduler": false, "modelAccess": false})
	}
	if enabled != "1" {
		return errors.New("invalid media retention activation")
	}
	flags := flag.NewFlagSet("media-retention", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	batch := flags.Int("batch", 100, "bounded retained derivatives")
	deadline := flags.Duration("deadline", 10*time.Second, "finite owned execution")
	if e := flags.Parse(args); e != nil || flags.NArg() != 0 || *batch < 1 || *batch > 100 || *deadline < time.Millisecond || *deadline > 30*time.Second {
		return errors.New("invalid media retention bounds")
	}
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLKEY", "PGSSLCERT", "PGTARGETSESSIONATTRS"} {
		if strings.TrimSpace(getenv(key)) != "" {
			return errors.New("ambiguous PostgreSQL environment rejected")
		}
	}
	database, marker := getenv("BIRDTIE_MEDIA_RETENTION_OWNED_DATABASE"), getenv("BIRDTIE_MEDIA_RETENTION_OWNED_MARKER")
	c, e := retentionConfig(getenv("BIRDTIE_DATABASE_URL"), getenv("BIRDTIE_ENV"), getenv("BIRDTIE_DISPOSABLE_DB"), database, marker)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, *deadline)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, c)
	if e != nil {
		return errors.New("owned media retention connection unavailable")
	}
	defer pool.Close()
	result, e := postgres.New(pool, false).PurgeHumanMediaDerivatives(ctx, database, marker, *batch)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(struct {
		Scope string `json:"scope"`
		postgres.MediaRetentionResult
		ProductionScheduler bool `json:"productionScheduler"`
		ModelAccess         bool `json:"modelAccess"`
	}{Scope: "LOCAL_EXACT_OWNED_DISPOSABLE_ONLY", MediaRetentionResult: result})
}
func main() {
	if e := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
