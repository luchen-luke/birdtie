package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration roundtrips must not downgrade the runtime schema used concurrently
// by another package. The fixture helpers are synchronous (no t.Parallel);
// t.Setenv scopes their existing connection lookup to this owned database.
func ownedMigrationDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for actual PostgreSQL migration verification")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("migration roundtrip requires an explicitly disposable database")
	}
	uri, err := url.Parse(dsn)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") {
		t.Fatal("owned migration fixture requires a PostgreSQL URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	parent, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cancel()
		t.Fatal("cannot connect disposable parent database", err)
	}
	guard := func(ctx context.Context) (string, error) {
		var result string
		err := parent.QueryRow(ctx, `SELECT coalesce(pg_get_functiondef(to_regprocedure('public.birdtie_guard_memory_reinforcement()')),'')`).Scan(&result)
		return result, err
	}
	before, err := guard(ctx)
	if err != nil || before == "" {
		parent.Close()
		cancel()
		t.Fatal("parent reinforcement guard missing", err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		parent.Close()
		cancel()
		t.Fatal(err)
	}
	config.ConnConfig.Database = "postgres"
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		parent.Close()
		cancel()
		t.Fatal("cannot connect local database administration", err)
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		admin.Close()
		parent.Close()
		cancel()
		t.Fatal(err)
	}
	name := "birdtie_owned_migration_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close()
		parent.Close()
		cancel()
		t.Fatal("cannot create owned local migration database", err)
	}
	var owned *pgxpool.Pool
	t.Cleanup(func() {
		if owned != nil {
			owned.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		after, err := guard(cleanup)
		if err != nil || after != before {
			t.Error("owned migration changed parent runtime reinforcement guard", err)
		}
		// The only target is this call's successfully created random identifier.
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted); err != nil {
			t.Error("owned migration database cleanup failed", err)
		}
		admin.Close()
		parent.Close()
		cancel()
	})
	uri.Path = "/" + name
	uri.RawPath = ""
	ownedDSN := uri.String()
	owned, err = pgxpool.New(ctx, ownedDSN)
	if err != nil {
		t.Fatal("cannot connect owned migration database", err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	count := 0
	for _, path := range files {
		if strings.HasSuffix(path, ".down.sql") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = owned.Exec(ctx, string(raw)); err != nil {
			t.Fatal("owned actual migration baseline", filepath.Base(path), err)
		}
		count++
		var seeds []string
		if strings.HasPrefix(filepath.Base(path), "029_") {
			seeds = []string{"001_badminton.sql", "002_functional_mvp.sql"}
		}
		if strings.HasPrefix(filepath.Base(path), "033_") {
			seeds = []string{"003_community_social.sql"}
		}
		for _, seed := range seeds {
			raw, err := os.ReadFile(filepath.Join("..", "..", "dev-seeds", seed))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = owned.Exec(ctx, string(raw)); err != nil {
				t.Fatal("owned local synthetic seed", seed, err)
			}
		}
	}
	if count < 64 {
		t.Fatal("owned baseline requires actual current enrichment schema064 or later")
	}
	t.Logf("migration roundtrip uses independently owned database %s with %d actual migrations", name, count)
	t.Setenv("BIRDTIE_DATABASE_URL", ownedDSN)
}
