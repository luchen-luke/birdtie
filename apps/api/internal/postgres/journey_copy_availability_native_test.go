package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Uses only the caller's exact marked loopback parent, migrated 001-179+181.
// It never invokes the all-migrations child fixture (which includes 180).
func TestJourneyCopyAvailabilityNative179KeepsOriginalRecovery(t *testing.T) {
	if content.JourneysEnabled {
		t.Fatal("Journey release boundary must stay closed")
	}
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an owned loopback database")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("explicit disposable database required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1") {
		t.Fatal("owned loopback database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
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
	var absent, replies bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.journey_publication_snapshots') IS NULL AND to_regclass('public.journey_publication_current') IS NULL,EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='moment_comments' AND column_name='parent_comment_id')`).Scan(&absent, &replies); e != nil || !absent || !replies {
		t.Fatal("requires179 and no180", e)
	}
	f := &collectionPublicationNativeFixture{t: t, ctx: ctx, pool: pool, s: New(pool, false)}
	account := func() (identity.Actor, [32]byte, string) {
		id, session := f.uuid(), f.uuid()
		f.exec(`INSERT INTO accounts(id,account_type)VALUES($1,'person')`, id)
		_, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, session, id, digest[:])
		return identity.Actor{ID: id, AccountType: "person"}, digest, session
	}
	f.owner, f.digest, f.session = account()
	f.peer, f.peerDigest, f.peerSession = account()
	for i := 0; i < 3; i++ {
		id := f.uuid()
		f.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'aberdeen-gb','隔离复制测试','park',57.15,-2.1,'wgs84','point','published','合成测试','disposable://journey-copy-gate','合成')`, id)
		f.places = append(f.places, id)
	}
	// A real retained publication and opt-in grant, not a disabled Create or
	// Publish call masquerading as a legacy record.
	ref := content.CollectionRef{Kind: "journey", ID: f.uuid()}
	f.exec(`INSERT INTO journeys(id,owner_account_id,city_id,title,visibility,status,revision,owner_confirmed_at,published_at)VALUES($1,$2,'aberdeen-gb','隔离旧旅程','public','published',2,clock_timestamp(),clock_timestamp())`, ref.ID, f.owner.ID)
	f.exec(`INSERT INTO journey_stops(journey_id,city_id,sequence_no,place_id,location_visibility)VALUES($1,'aberdeen-gb',1,$2,'place')`, ref.ID, f.places[0])
	f.exec(`INSERT INTO personal_collection_copy_grants(kind,target_id,owner_account_id,publication_revision,grant_version,allow_private_place_copy)VALUES('journey',$1,$2,2,1,true)`, ref.ID, f.owner.ID)
	ids := []string{f.places[0]}
	in := content.CollectionCopyInput{OperationID: f.uuid(), PublicationRevision: 2, GrantVersion: 1, OrderedPlaceIDs: ids, OrderedPlaceIDsSHA256: content.CollectionCopyPlacesDigest(ids), Title: "隔离原副本"}
	// Verify that the retained source really has independent current copy
	// authority, then issue the exact internal old approval for that source.
	bound, tx, fence, e := f.s.collectionCopyBegin(ctx, f.peerDigest, f.peer, in.OperationID)
	if e != nil {
		t.Fatal("current receiver session", e)
	}
	v, authority, e := f.s.collectionCopySource(bound, tx, f.peerDigest, f.peer, ref, in, true, nil)
	if e != nil || v.CopyGrant == nil || len(v.CopyGrant.EligiblePlaceIDs) != 1 {
		_ = tx.Rollback(context.Background())
		t.Fatal("retained source has no actual approved copy grant", e)
	}
	now := time.Now().UTC()
	end := now.Add(20 * time.Second)
	if v.SourceExpiresAt.Before(end) {
		end = v.SourceExpiresAt
	}
	in.Approval, e = collectionCopyToken(collectionCopyMaterial(f.peer, fence, ref, in, v, authority), now.UnixNano(), end.UnixNano())
	_ = tx.Rollback(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	preview := in
	preview.Approval = ""
	if _, e = f.s.PreviewHumanCollectionCopy(ctx, f.peerDigest, f.peer, ref, preview); !errors.Is(e, content.ErrUnavailable) {
		t.Fatal("new Journey preview was enabled", e)
	}
	if _, e = f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, ref, in); !errors.Is(e, content.ErrUnavailable) {
		t.Fatal("new Journey destination bypassed release boundary", e)
	}
	var destinations, receipts, audits int
	if e = pool.QueryRow(ctx, `SELECT(SELECT count(*) FROM journeys WHERE owner_account_id=$1),(SELECT count(*) FROM personal_collection_copy_operations WHERE receiver_account_id=$1),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='copy_public_places')`, f.peer.ID).Scan(&destinations, &receipts, &audits); e != nil || destinations != 0 || receipts != 0 || audits != 0 {
		t.Fatal("rejected copy had persistent effects", e)
	}
	// Preserve a concrete pre-existing success receipt and exact original
	// operation. Recovery must not depend on a new preview, source or approval.
	destination := f.uuid()
	f.exec(`INSERT INTO journeys(id,owner_account_id,city_id,title)VALUES($1,$2,'aberdeen-gb',$3)`, destination, f.peer.ID, in.Title)
	f.exec(`INSERT INTO journey_stops(journey_id,city_id,sequence_no,place_id,location_visibility)VALUES($1,'aberdeen-gb',1,$2,'hidden')`, destination, ids[0])
	f.exec(`INSERT INTO personal_collection_copy_origins(destination_kind,destination_id,receiver_account_id,source_kind,source_id,publication_revision,grant_version,origin_authority)VALUES('journey',$1,$2,'journey',$3,2,1,$4)`, destination, f.peer.ID, ref.ID, authority)
	f.exec(`INSERT INTO personal_collection_copy_borrowed_places(destination_kind,destination_id,place_id)VALUES('journey',$1,$2)`, destination, ids[0])
	digest, _ := hex.DecodeString(content.CollectionCopyDigest(ref, in))
	idsDigest, _ := hex.DecodeString(in.OrderedPlaceIDsSHA256)
	f.exec(`INSERT INTO personal_collection_copy_operations(receiver_account_id,operation_id,status,destination_kind,destination_id,source_kind,source_id,publication_revision,grant_version,ordered_place_ids_sha256,input_sha256,result_revision)VALUES($1,$2,'SUCCEEDED','journey',$3,'journey',$4,2,1,$5,$6,1)`, f.peer.ID, in.OperationID, destination, ref.ID, idsDigest, digest)
	in.Approval = "expired original approval is not replayed"
	for _, read := range []func() (content.CollectionCopyReceipt, error){
		func() (content.CollectionCopyReceipt, error) {
			return f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, ref, in)
		},
		func() (content.CollectionCopyReceipt, error) {
			return f.s.GetHumanCollectionCopyOperation(ctx, f.peerDigest, f.peer, in.OperationID)
		},
		func() (content.CollectionCopyReceipt, error) {
			return f.s.StopHumanCollectionCopyOperation(ctx, f.peerDigest, f.peer, in.OperationID)
		},
	} {
		out, err := read()
		if err != nil || out.Status != "SUCCEEDED" || out.DestinationRef == nil || *out.DestinationRef != (content.CollectionRef{Kind: "journey", ID: destination}) || out.SourceRef == nil || *out.SourceRef != ref {
			t.Fatal("original success recovery changed", err)
		}
	}
	changed := in
	changed.Title = "不能复用原操作"
	if _, e = f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, ref, changed); !errors.Is(e, content.ErrConflict) {
		t.Fatal("changed input adopted original receipt", e)
	}
	if _, e = f.s.GetHumanCollectionCopyOperation(ctx, f.digest, f.owner, in.OperationID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("cross-account receipt became readable", e)
	}
	stopped := in
	stopped.OperationID = f.uuid()
	if _, e = f.s.StopHumanCollectionCopyOperation(ctx, f.peerDigest, f.peer, stopped.OperationID); e != nil {
		t.Fatal("original bodyless stop disabled", e)
	}
	if out, err := f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, ref, stopped); err != nil || out.Status != "STOPPED" || out.DestinationRef != nil {
		t.Fatal("late commit defeated original stop", err)
	}
	// The unrelated ThemeMap path still performs the actual preview, write,
	// exact same-op replay, and independent private destination read.
	theme, publish := f.draft("theme_map")
	publish.AllowPrivatePlaceCopy = true
	publish.Entries[0].LocationVisibility = "place"
	theme, e = f.s.PublishHumanCollection(ctx, f.digest, f.owner, "theme_map", theme.ID, publish)
	if e != nil {
		t.Fatal("ThemeMap publication changed", e)
	}
	themeInput := content.CollectionCopyInput{OperationID: f.uuid(), PublicationRevision: theme.Revision, GrantVersion: 1, OrderedPlaceIDs: ids, OrderedPlaceIDsSHA256: content.CollectionCopyPlacesDigest(ids), Title: "隔离主题地图副本"}
	themeInput = collectionCopyNativeApprove(t, f, theme, themeInput)
	themeRef := content.CollectionRef{Kind: "theme_map", ID: theme.ID}
	themeOut, e := f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, themeRef, themeInput)
	if e != nil || themeOut.DestinationRef == nil {
		t.Fatal("ThemeMap copy disabled", e)
	}
	if own, err := f.s.GetHumanCollection(ctx, f.peerDigest, f.peer, "theme_map", themeOut.DestinationRef.ID); err != nil || own.Visibility != "private" || own.Status != "draft" || own.Revision != 1 || len(own.Entries) != 1 || own.Entries[0].PlaceID != ids[0] {
		t.Fatal("ThemeMap result lost exact private places", err)
	}
	if again, err := f.s.CopyHumanCollection(ctx, f.peerDigest, f.peer, themeRef, themeInput); err != nil || again.DestinationRef == nil || *again.DestinationRef != *themeOut.DestinationRef {
		t.Fatal("ThemeMap replay duplicated destination", err)
	}
	frame, e := f.s.CaptureCollectionSession(ctx, f.peerDigest, f.peer)
	if e != nil {
		t.Fatal(e)
	}
	oldContext := content.WithCollectionSession(ctx, f.peerDigest, f.peer, frame)
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.peerSession)
	f.exec(`UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.peerSession)
	if _, e = f.s.CopyHumanCollection(oldContext, f.peerDigest, f.peer, ref, in); !errors.Is(e, content.ErrConflict) {
		t.Fatal("receipt replay bypassed original session generation", e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM journeys WHERE owner_account_id=$1`, f.peer.ID).Scan(&destinations); e != nil || destinations != 1 {
		t.Fatal("recovery or stopped commit created another Journey", e)
	}
}
