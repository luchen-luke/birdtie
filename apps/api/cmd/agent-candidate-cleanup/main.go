// Local-development cleanup proof only. This command is not a deployed scheduler.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

func localConfig(raw, environment, disposable string) (*pgxpool.Config, error) {
	fail := errors.New("cleanup requires explicit disposable local-development PostgreSQL")
	if environment != "development" || disposable != "1" || raw == "" {
		return nil, fail
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "postgres" || u.Hostname() == "" || u.User == nil || u.Fragment != "" || u.Opaque != "" || u.Path == "" || u.Path == "/" {
		return nil, fail
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fail
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return nil, fail
	}
	for k, v := range q {
		if k != "sslmode" || len(v) != 1 || v[0] != "disable" {
			return nil, fail
		}
	}
	if q.Get("sslmode") != "disable" {
		return nil, fail
	}
	c, e := pgxpool.ParseConfig(raw)
	if e != nil {
		return nil, fail
	}
	if c.ConnConfig.TLSConfig != nil || len(c.ConnConfig.Fallbacks) != 0 || c.ConnConfig.Host != u.Hostname() {
		return nil, fail
	}
	if host := net.ParseIP(c.ConnConfig.Host); host == nil || !host.IsLoopback() {
		return nil, fail
	}
	// PGOPTIONS/runtime environment must not disable triggers, swap namespaces
	// or request a different transaction security context.
	for k := range c.ConnConfig.RuntimeParams {
		if k != "application_name" {
			return nil, fail
		}
	}
	c.ConnConfig.RuntimeParams["application_name"] = "birdtie-local-candidate-cleanup"
	c.MaxConns = 1
	c.MinConns = 0
	return c, nil
}
func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("agent-candidate-cleanup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	batch := flags.Int("batch", 100, "bounded pending candidates")
	deadline := flags.Duration("deadline", 10*time.Second, "finite execution deadline")
	if e := flags.Parse(args); e != nil || flags.NArg() != 0 || *batch < 1 || *batch > 100 || *deadline < time.Millisecond || *deadline > 30*time.Second {
		return errors.New("invalid cleanup bounds")
	}
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLKEY", "PGSSLCERT", "PGTARGETSESSIONATTRS"} {
		if strings.TrimSpace(getenv(key)) != "" {
			return errors.New("ambiguous PostgreSQL environment rejected")
		}
	}
	config, e := localConfig(getenv("BIRDTIE_DATABASE_URL"), getenv("BIRDTIE_ENV"), getenv("BIRDTIE_DISPOSABLE_DB"))
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, *deadline)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		return errors.New("local cleanup connection unavailable")
	}
	defer pool.Close()
	var ready bool
	e = pool.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_expire_candidate_pipeline(integer)') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='candidate_pipeline_revoke' AND tgenabled='O' AND tgrelid=to_regclass('public.consent_grants'))`).Scan(&ready)
	if e != nil || !ready {
		return errors.New("native cleanup migration unavailable")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline($1)`, *batch).Scan(&count); e != nil {
		return errors.New("native local cleanup failed")
	}
	return json.NewEncoder(out).Encode(struct {
		Scope               string `json:"scope"`
		Expired             int    `json:"expired"`
		ModelAccess         bool   `json:"modelAccess"`
		ProductionScheduler bool   `json:"productionScheduler"`
	}{Scope: "LOCAL_DISPOSABLE_ONLY", Expired: count})
}
func main() {
	if e := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
