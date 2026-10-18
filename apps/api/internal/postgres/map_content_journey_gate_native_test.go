package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The caller owns and migrates this marked loopback database through 179 only.
// Do not call ownedMigrationDatabase: that helper intentionally applies every
// checked-in migration, including the deferred Journey migration 180.
func TestMapContentJourneyDisabledNative179(t *testing.T) {
	if content.JourneysEnabled {
		t.Fatal("Journey release boundary must stay closed")
	}
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an owned loopback schema179 database")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("explicit disposable database required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1") {
		t.Fatal("owned loopback database required")
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
	var absent, replySchema bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.journey_publication_snapshots') IS NULL AND to_regclass('public.journey_publication_current') IS NULL AND to_regprocedure('public.birdtie_journey_lifecycle(uuid,timestamp with time zone)') IS NULL,EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='moment_comments' AND column_name='parent_comment_id')`).Scan(&absent, &replySchema); e != nil || !absent || !replySchema {
		t.Fatal("test requires migration179 and no180", e)
	}
	f := &savedPageNativeFixture{ctx: ctx, pool: pool, store: New(pool, false)}
	owner := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, owner)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.actor = identity.Actor{ID: owner, AccountType: "person"}
	f.digest = digest
	f.session = f.id(t)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.session, owner, digest[:])
	_, place := f.bookmark(t)
	in := threadNativeInput(t, f)
	located := in.Items[0]
	located.Input.PlaceID = place
	located.Input.LocationPrecision = "place"
	in.Items = []content.MomentThreadItem{located, in.Items[1], located, located}
	published, e := f.store.CommitMomentThread(ctx, digest, f.actor, threadNativeApprove(t, f, in))
	if e != nil {
		t.Fatal("publish original test thread", e)
	}
	page, e := f.store.ReadHumanMapContent(ctx, digest, f.actor, content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 14, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}})
	if e != nil || !page.Valid(time.Now()) {
		t.Fatal("Moment map must not query deferred Journey tables", e)
	}
	pins := map[string]content.MapContentPin{}
	for _, pin := range page.Pins {
		if len(pin.JourneyEdges) != 0 {
			t.Fatal("deferred Journey edge leaked")
		}
		if pin.ThreadID == published.ThreadID {
			pins[pin.MomentID] = pin
		}
	}
	if len(pins) != 3 || len(pins[published.Members[0].MomentID].NextMarkerIDs) != 0 || len(pins[published.Members[2].MomentID].NextMarkerIDs) != 1 {
		t.Fatal("original thread points/gaps changed")
	}
	if e = page.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("final Moment map proof queried180", e)
	}
	// An old private record is still readable/listable without lifecycle180.
	journey := f.id(t)
	f.exec(t, `INSERT INTO journeys(id,owner_account_id,city_id,title,summary)VALUES($1,$2,'aberdeen-gb','synthetic retained plan179','')`, journey, owner)
	if _, e = f.store.GetHumanCollection(ctx, digest, f.actor, "journey", journey); e != nil {
		t.Fatal("retained Journey read requires180", e)
	}
	if _, e = f.store.ListHumanCollections(ctx, digest, f.actor, "journey", ""); e != nil {
		t.Fatal("retained Journey list requires180", e)
	}
}
