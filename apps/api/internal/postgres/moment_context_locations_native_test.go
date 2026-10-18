package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The runner owns a marked loopback database migrated through 179, then 181.
// Do not use ownedMigrationDatabase: its full migration set includes the
// intentionally deferred Journey migration 180.
func TestMomentContextLocationsNative181(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an owned loopback schema179+181 database")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("explicit disposable database required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	loopback := func(host string) bool { return host == "127.0.0.1" || host == "localhost" || host == "::1" }
	if e != nil || !loopback(cfg.ConnConfig.Host) || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_") {
		t.Fatal("owned loopback database required")
	}
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("non-loopback fallback forbidden")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("connect owned database", e)
	}
	defer pool.Close()
	var name, marker string
	if e = pool.QueryRow(ctx, `SELECT current_database(),coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname=current_database()`).Scan(&name, &marker); e != nil || marker != "BIRDTIE_ISOLATED_FIXTURE_V1:"+name || !strings.HasPrefix(name, "birdtie_") {
		t.Fatal("exact owned database marker required")
	}
	var absent180, reply179 bool
	if e = pool.QueryRow(ctx, `SELECT
        to_regclass('public.journey_publication_snapshots') IS NULL
        AND to_regclass('public.journey_publication_current') IS NULL
        AND to_regprocedure('public.birdtie_journey_lifecycle(uuid,timestamp with time zone)') IS NULL,
        EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='moment_comments' AND column_name='parent_comment_id')`).Scan(&absent180, &reply179); e != nil || !absent180 || !reply179 {
		t.Fatal("test requires migration179+181 without180", e)
	}
	assertFK := func(t *testing.T, q interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}, independent bool) {
		t.Helper()
		var single, composite, activity, onlyTwo bool
		e := q.QueryRow(ctx, `SELECT
            EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='moment_activity_links'::regclass AND c.confrelid='moments'::regclass AND c.contype='f' AND c.convalidated AND c.confdeltype='c' AND c.conname='moment_activity_links_moment_id_fkey' AND cardinality(c.conkey)=1 AND (SELECT attname FROM pg_attribute WHERE attrelid=c.conrelid AND attnum=c.conkey[1])='moment_id' AND (SELECT attname FROM pg_attribute WHERE attrelid=c.confrelid AND attnum=c.confkey[1])='id'),
            EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='moment_activity_links'::regclass AND c.confrelid='moments'::regclass AND c.contype='f' AND c.convalidated AND c.confdeltype='c' AND c.conname='moment_activity_links_moment_id_city_id_fkey' AND cardinality(c.conkey)=2),
            EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='moment_activity_links'::regclass AND c.confrelid='activities'::regclass AND c.contype='f' AND c.convalidated AND c.confdeltype='a' AND cardinality(c.conkey)=2 AND (SELECT array_agg(a.attname::text ORDER BY k.n) FROM unnest(c.conkey) WITH ORDINALITY k(attnum,n) JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)=ARRAY['activity_id','city_id']::text[] AND (SELECT array_agg(a.attname::text ORDER BY k.n) FROM unnest(c.confkey) WITH ORDINALITY k(attnum,n) JOIN pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum)=ARRAY['id','city_id']::text[]),
            (SELECT count(*)=2 FROM pg_constraint WHERE conrelid='moment_activity_links'::regclass AND contype='f')`).Scan(&single, &composite, &activity, &onlyTwo)
		if e != nil || single != independent || composite == independent || !activity || !onlyTwo {
			t.Fatal("Moment FK independence/Activity City FK/cascade changed", independent, single, composite, activity, onlyTwo, e)
		}
	}
	assertFK(t, pool, true)
	migrationBody := func(file string) string {
		t.Helper()
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		if e != nil {
			t.Fatal(e)
		}
		sql := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(sql, "BEGIN;") || !strings.HasSuffix(sql, "COMMIT;") {
			t.Fatal("expected atomic181 migration boundaries")
		}
		// Use the real migration statements in a caller-owned transaction so
		// rollback tests never change the shared fixture's installed schema.
		return strings.TrimSuffix(strings.TrimPrefix(sql, "BEGIN;"), "COMMIT;")
	}
	up := migrationBody("181_moment_activity_location_independence.sql")
	down := migrationBody("181_moment_activity_location_independence.down.sql")
	f := &savedPageNativeFixture{ctx: ctx, pool: pool, store: New(pool, false)}
	owner := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, owner)
	_, f.digest, e = identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.actor = identity.Actor{ID: owner, AccountType: "person"}
	f.session = f.id(t)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.session, owner, f.digest[:])
	activity := entityCoverNativeActivity(t, f).ID

	// A legacy, genuinely City-located Moment remains representable both ways.
	cityInput := mc144Input(t, f)
	cityInput.Input.ActivityID = momentString(activity)
	cityMoment, _ := mc144Write(t, f, cityInput)
	var originalCity bool
	if e = pool.QueryRow(ctx, `SELECT m.city_id='aberdeen-gb' AND m.place_id IS NULL AND m.location_precision='city' AND l.city_id=m.city_id FROM moments m JOIN moment_activity_links l ON l.moment_id=m.id WHERE m.id=$1 AND l.activity_id=$2`, cityMoment.MomentID, activity).Scan(&originalCity); e != nil || !originalCity {
		t.Fatal("original explicit City semantics changed", e)
	}
	t.Run("compatibleDownUpPreservesRows", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		digest := func() string {
			t.Helper()
			var value string
			if e := tx.QueryRow(ctx, `SELECT md5(coalesce(jsonb_agg(to_jsonb(l) ORDER BY moment_id,activity_id),'[]'::jsonb)::text) FROM moment_activity_links l`).Scan(&value); e != nil {
				t.Fatal(e)
			}
			return value
		}
		// Hold the same lock before taking the baseline; concurrent tests
		// cannot insert a new association between the baseline and migration.
		if _, e = tx.Exec(ctx, `LOCK TABLE moment_activity_links IN ACCESS EXCLUSIVE MODE`); e != nil {
			t.Fatal(e)
		}
		before := digest()
		if _, e = tx.Exec(ctx, down); e != nil {
			t.Fatal("compatible181 downgrade", e)
		}
		assertFK(t, tx, false)
		if digest() != before {
			t.Fatal("downgrade rewrote original associations")
		}
		if _, e = tx.Exec(ctx, up); e != nil {
			t.Fatal("compatible181 upgrade", e)
		}
		assertFK(t, tx, true)
		if digest() != before {
			t.Fatal("upgrade rewrote original associations")
		}
	})
	assertFK(t, pool, true)

	_, place := f.bookmark(t)
	input := mc144Input(t, f)
	input.Input.PlaceID = place
	input.Input.LocationPrecision = "place"
	input.Input.ActivityID = momentString(activity)
	controlled, approved := mc144Write(t, f, input)
	assertCombination := func(t *testing.T, id string) {
		t.Helper()
		var canonical, links bool
		if e := pool.QueryRow(ctx, `SELECT m.author_account_id=$2 AND m.city_id IS NULL AND m.place_id=$3::uuid AND m.location_precision='place' AND m.visibility='private' AND m.status='draft',
            (SELECT count(*)=1 AND bool_and(l.kind='PLACE' AND l.place_id=$3::uuid AND l.city_id IS NULL AND l.position=0 AND l.precision='place') FROM moment_location_links l WHERE l.moment_id=m.id AND l.status='ACTIVE')
            AND (SELECT count(*)=1 AND bool_and(l.activity_id=$4::uuid AND l.city_id=a.city_id) FROM moment_activity_links l JOIN activities a ON a.id=l.activity_id WHERE l.moment_id=m.id)
            FROM moments m WHERE m.id=$1`, id, owner, place, activity).Scan(&canonical, &links); e != nil || !canonical || !links {
			t.Fatal("Place+Activity invented a Moment City or changed canonical links", e)
		}
		own, e := f.store.GetHumanMoment(ctx, f.digest, f.actor, id)
		if e != nil || own.ID != id || own.CityID != "" || own.PlaceID != place || own.LocationPrecision != "place" || len(own.ActivityIDs) != 1 || own.ActivityIDs[0] != activity {
			t.Fatal("own reader changed original Place+Activity selectors", e)
		}
	}
	assertCombination(t, controlled.MomentID)
	replay, e := f.store.CommitMomentControl(ctx, f.digest, f.actor, approved)
	if e != nil || content.MomentControlHash(replay) != content.MomentControlHash(controlled) {
		t.Fatal("new combination lost original operation recovery", e)
	}
	ordinary, e := f.store.CreateHumanMomentDraft(ctx, f.digest, f.actor, *input.Input)
	if e != nil {
		t.Fatal("ordinary authenticated Place+Activity writer", e)
	}
	assertCombination(t, ordinary.ID)

	t.Run("activityCityAndMomentIdentityRemainForeignKeys", func(t *testing.T) {
		wrongCity := "synthetic181-" + f.id(t)
		f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'仅隔离错误关联城市','合成','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','disposable://context181','合成')`, wrongCity)
		wrong := mc144Input(t, f)
		wrong.Input.CityID = wrongCity
		wrong.Input.ActivityID = momentString(activity)
		before := mc144Count(t, f)
		if _, e := f.store.PreviewMomentControl(ctx, f.digest, f.actor, wrong); !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("control preview accepted an Activity from a different selected City", e)
		}
		if _, e := f.store.CreateHumanMomentDraft(ctx, f.digest, f.actor, *wrong.Input); !errors.Is(e, content.ErrConflict) {
			t.Fatal("ordinary writer accepted an Activity from a different selected City", e)
		}
		mc144NoEffect(t, f, before)
		_, e := pool.Exec(ctx, `UPDATE moment_activity_links SET city_id=$2 WHERE moment_id=$1 AND activity_id=$3`, controlled.MomentID, wrongCity, activity)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23503" || pg.ConstraintName != "moment_activity_links_activity_id_city_id_fkey" {
			t.Fatal("Activity's original City constraint was removed", e)
		}
		_, e = pool.Exec(ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at)VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, f.id(t), activity)
		if !errors.As(e, &pg) || pg.Code != "23503" || pg.ConstraintName != "moment_activity_links_moment_id_fkey" {
			t.Fatal("missing canonical Moment accepted", e)
		}
		assertCombination(t, controlled.MomentID)
	})
	t.Run("momentDeletionStillCascades", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		id := f.id(t)
		if _, e = tx.Exec(ctx, `INSERT INTO moments(id,author_account_id,city_id,title,body,time_precision,location_precision)VALUES($1,$2,NULL,'','仅隔离FK级联夹具','unknown','none')`, id, owner); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at)VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, id, activity); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `DELETE FROM moments WHERE id=$1`, id); e != nil {
			t.Fatal("original Moment delete no longer cascades", e)
		}
		var remaining int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM moment_activity_links WHERE moment_id=$1`, id).Scan(&remaining); e != nil || remaining != 0 {
			t.Fatal("orphan Activity association", remaining, e)
		}
	})
	t.Run("incompatibleDownRejectsWithoutRewriting", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		_, e = tx.Exec(ctx, down)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23514" || !strings.Contains(pg.Message, "cannot roll back migration 181") {
			t.Fatal("downgrade must explicitly reject independent locations", e)
		}
		if e = tx.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
		assertFK(t, pool, true)
		assertCombination(t, controlled.MomentID)
		assertCombination(t, ordinary.ID)
	})
}
