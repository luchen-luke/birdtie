package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"strings"
	"testing"
	"time"
)

func collectionCopyNativeSource(t *testing.T, kind string, allow bool) (*collectionPublicationNativeFixture, content.PersonalCollection, content.CollectionCopyInput) {
	t.Helper()
	f := collectionPublicationFixture(t)
	f.exec(`UPDATE places SET coordinate_system='wgs84' WHERE id=ANY($1::uuid[])`, f.places)
	v, p := f.draft(kind)
	p.AllowPrivatePlaceCopy = allow
	p.Entries[0].LocationVisibility = "place"
	p.Entries[1].LocationVisibility = "city"
	p.Entries[2].LocationVisibility = "place"
	v, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, v.ID, p)
	if e != nil {
		t.Fatal("author opt-in publication", e)
	}
	ids := []string{f.places[0], f.places[2]}
	in := content.CollectionCopyInput{OperationID: f.uuid(), PublicationRevision: v.Revision, GrantVersion: 1, OrderedPlaceIDs: ids, OrderedPlaceIDsSHA256: content.CollectionCopyPlacesDigest(ids), Title: "本人独立私藏"}
	return f, v, in
}
func collectionCopyNativeApprove(t *testing.T, f *collectionPublicationNativeFixture, source content.PersonalCollection, in content.CollectionCopyInput) content.CollectionCopyInput {
	t.Helper()
	p, e := f.s.PreviewHumanCollectionCopy(f.ctx, f.peerDigest, f.peer, content.CollectionRef{Kind: source.Kind, ID: source.ID}, in)
	if e != nil || p.EntryCount != len(in.OrderedPlaceIDs) || p.InputSHA256 != content.CollectionCopyDigest(p.SourceRef, in) || p.ExpiresAt.After(time.Now().Add(31*time.Second)) {
		t.Fatal("original concrete preview", e)
	}
	if e = p.ResponseProof.RevalidateCollection(f.ctx); e != nil {
		t.Fatal("actual preview proof", e)
	}
	in.Approval = p.Approval
	return in
}
func TestCollectionCopyNativeOptInCompatibilityAndReceiverPrivateCopy(t *testing.T) {
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			f, source, in := collectionCopyNativeSource(t, kind, false)
			ref := content.CollectionRef{Kind: kind, ID: source.ID}
			public, e := f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{Actor: f.peer, SessionDigest: f.peerDigest}, kind, source.ID)
			if e != nil || public.CopyGrant != nil {
				t.Fatal("legacy false publication became copy permission", e)
			}
			if _, e = f.s.PreviewHumanCollectionCopy(f.ctx, f.peerDigest, f.peer, ref, in); !errors.Is(e, content.ErrConflict) {
				t.Fatal("default false allowed copy", e)
			}
			source, e = f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, source.ID, content.CollectionUnpublishInput{OperationID: f.uuid(), Revision: source.Revision})
			if e != nil {
				t.Fatal(e)
			}
			p := content.CollectionPublicationInput{OperationID: f.uuid(), Revision: source.Revision, AllowPrivatePlaceCopy: true, Entries: []content.CollectionPublicationEntry{}}
			for i, id := range f.places {
				precision := "place"
				if i == 1 {
					precision = "city"
				}
				p.Entries = append(p.Entries, content.CollectionPublicationEntry{PlaceID: id, LocationVisibility: precision})
			}
			source, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, source.ID, p)
			if e != nil {
				t.Fatal(e)
			}
			in.PublicationRevision = source.Revision
			public, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{Actor: f.peer, SessionDigest: f.peerDigest}, kind, source.ID)
			if e != nil || public.CopyGrant == nil || len(public.CopyGrant.EligiblePlaceIDs) != 2 || public.CopyGrant.EligiblePlaceIDs[0] != f.places[0] || public.CopyGrant.EligiblePlaceIDs[1] != f.places[2] {
				t.Fatal("grant exposed hidden/city ID or lost original order", e)
			}
			if e = f.s.RevalidatePublicCollection(f.ctx, content.PublicMomentAccess{Actor: f.peer, SessionDigest: f.peerDigest}, public); e != nil {
				t.Fatal("fixed public grant lease", e)
			}
			bad := in
			bad.OrderedPlaceIDs = []string{f.places[2], f.places[0]}
			bad.OrderedPlaceIDsSHA256 = content.CollectionCopyPlacesDigest(bad.OrderedPlaceIDs)
			if _, e = f.s.PreviewHumanCollectionCopy(f.ctx, f.peerDigest, f.peer, ref, bad); !errors.Is(e, content.ErrConflict) {
				t.Fatal("relative order bypassed", e)
			}
			in = collectionCopyNativeApprove(t, f, source, in)
			out, e := f.s.CopyHumanCollection(f.ctx, f.peerDigest, f.peer, ref, in)
			if e != nil || out.Status != "SUCCEEDED" || out.ResultRevision != 1 || out.DestinationRef == nil || out.DestinationRef.ID == source.ID || out.DestinationRef.ID == in.OperationID {
				t.Fatal("independent private destination", e)
			}
			own, e := f.s.GetHumanCollection(f.ctx, f.peerDigest, f.peer, kind, out.DestinationRef.ID)
			if e != nil || own.OwnerID != f.peer.ID || own.Visibility != "private" || own.Status != "draft" || own.Revision != 1 || !own.PrivatePlaceCopyRestricted || own.Title != in.Title || own.Summary != "" || len(own.Entries) != 2 {
				t.Fatal("copied author data or destination ownership", e)
			}
			for i, entry := range own.Entries {
				if entry.PlaceID != in.OrderedPlaceIDs[i] || entry.Note != "" || entry.LocationVisibility != "hidden" || !entry.Available || entry.PlaceName == "" || entry.Latitude == nil {
					t.Fatal("private ID/order or current approved point missing")
				}
			}
			if e = own.ResponseProof.RevalidateCollection(f.ctx); e != nil {
				t.Fatal("actual private fixed point lease", e)
			}
			replay, e := f.s.CopyHumanCollection(f.ctx, f.peerDigest, f.peer, ref, in)
			if e != nil || replay.DestinationRef == nil || *replay.DestinationRef != *out.DestinationRef {
				t.Fatal("operation duplicated", e)
			}
			var audits int
			if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='copy_public_places'`, out.DestinationRef.ID).Scan(&audits); e != nil || audits != 1 {
				t.Fatal("copy audit repeated", e)
			}
			original, e := f.s.GetHumanCollection(f.ctx, f.digest, f.owner, kind, source.ID)
			if e != nil || original.Title == in.Title || original.Entries[0].Note == "" {
				t.Fatal("receiver mutated original author", e)
			}
			publish := content.CollectionPublicationInput{OperationID: f.uuid(), Revision: own.Revision, Entries: []content.CollectionPublicationEntry{}}
			for _, entry := range own.Entries {
				publish.Entries = append(publish.Entries, content.CollectionPublicationEntry{PlaceID: entry.PlaceID, LocationVisibility: "place"})
			}
			if _, e = f.s.PublishHumanCollection(f.ctx, f.peerDigest, f.peer, kind, own.ID, publish); !errors.Is(e, content.ErrConflict) {
				t.Fatal("private copy grant became publication permission", e)
			}
		})
	}
}
func TestCollectionCopyNativeOriginalSessionAndAuthorityWait(t *testing.T) {
	f, source, in := collectionCopyNativeSource(t, "journey", true)
	in = collectionCopyNativeApprove(t, f, source, in)
	ref := content.CollectionRef{Kind: source.Kind, ID: source.ID}
	frame, e := f.s.CaptureCollectionSession(f.ctx, f.peerDigest, f.peer)
	if e != nil {
		t.Fatal(e)
	}
	bound := content.WithCollectionSession(f.ctx, f.peerDigest, f.peer, frame)
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	var blocker int
	if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&blocker); e != nil {
		t.Fatal(e)
	}
	if _, e = lock.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,141))`, f.peer.ID+":collection-copy:"+in.OperationID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.s.CopyHumanCollection(bound, f.peerDigest, f.peer, ref, in); done <- e }()
	wait := false
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		var n int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity waiter WHERE datname=current_database() AND wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(pid))`, blocker).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			wait = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !wait {
		t.Fatal("actual copy operation lock wait absent")
	}
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.peerSession)
	f.exec(`UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.peerSession)
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, content.ErrConflict) {
			t.Fatal("Session ABA borrowed restored generation", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copy wait did not return")
	}
	var copies, receipts, audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM personal_collection_copy_origins WHERE receiver_account_id=$1),(SELECT count(*) FROM personal_collection_copy_operations WHERE receiver_account_id=$1),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='copy_public_places')`, f.peer.ID).Scan(&copies, &receipts, &audits); e != nil || copies != 0 || receipts != 0 || audits != 0 {
		t.Fatal("late Session produced effect/history/audit", e)
	}
	in.OperationID = f.uuid()
	in.Approval = ""
	in = collectionCopyNativeApprove(t, f, source, in)
	f.exec(`UPDATE places SET name=name||'changed' WHERE id=$1`, f.places[0])
	f.exec(`UPDATE places SET name=replace(name,'changed','') WHERE id=$1`, f.places[0])
	if _, e = f.s.CopyHumanCollection(f.ctx, f.peerDigest, f.peer, ref, in); !errors.Is(e, content.ErrConflict) {
		t.Fatal("same values/source xmin ABA retained approval", e)
	}
	if _, e = f.s.GetHumanCollectionCopyOperation(f.ctx, f.peerDigest, f.peer, in.OperationID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("rejected source minted receipt", e)
	}
}
func TestCollectionCopyNativeOriginalReceiptStopAndBorrowedSourceRetirement(t *testing.T) {
	f, source, in := collectionCopyNativeSource(t, "theme_map", true)
	f.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, f.places[1])
	in = collectionCopyNativeApprove(t, f, source, in)
	ref := content.CollectionRef{Kind: source.Kind, ID: source.ID}
	out, e := f.s.CopyHumanCollection(f.ctx, f.peerDigest, f.peer, ref, in)
	if e != nil {
		t.Fatal(e)
	}
	unknown := f.uuid()
	if _, e = f.s.GetHumanCollectionCopyOperation(f.ctx, f.peerDigest, f.peer, unknown); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("404 inferred success", e)
	}
	stop, e := f.s.StopHumanCollectionCopyOperation(f.ctx, f.peerDigest, f.peer, unknown)
	if e != nil || stop.Status != "STOPPED" || stop.DestinationRef != nil {
		t.Fatal("unknown stop not zero-effect", e)
	}
	late := in
	late.OperationID = unknown
	if r, e := f.s.CopyHumanCollection(f.ctx, f.peerDigest, f.peer, ref, late); e != nil || r.Status != "STOPPED" {
		t.Fatal("late PUT defeated bodyless stop", e)
	}
	if _, e = f.s.GetHumanCollectionCopyOperation(f.ctx, f.digest, f.owner, in.OperationID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("cross actor tiny receipt", e)
	}
	old, e := f.s.GetHumanCollection(f.ctx, f.peerDigest, f.peer, source.Kind, out.DestinationRef.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range old.Entries {
		if entry.SourceExpiresAt == nil || entry.SourceExpiresAt.After(time.Now().Add(21*time.Second)) {
			t.Fatal("borrowed point ignored hidden original source deadline")
		}
	}
	if _, e = f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, source.Kind, source.ID, content.CollectionUnpublishInput{OperationID: f.uuid(), Revision: source.Revision}); e != nil {
		t.Fatal(e)
	}
	if e = old.ResponseProof.RevalidateCollection(f.ctx); e == nil {
		t.Fatal("original already-encoded borrowed point survived withdrawal")
	}
	current, e := f.s.GetHumanCollection(f.ctx, f.peerDigest, f.peer, source.Kind, old.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range current.Entries {
		if entry.Available || entry.PlaceName != "" || entry.Latitude != nil || entry.Longitude != nil || entry.SourceExpiresAt != nil {
			t.Fatal("withdrawn source borrowed label/point/lease survived")
		}
	}
	historical, e := f.s.GetHumanCollectionCopyOperation(f.ctx, f.peerDigest, f.peer, in.OperationID)
	if e != nil || historical.DestinationRef == nil || *historical.DestinationRef != *out.DestinationRef {
		t.Fatal("original immutable success hidden by withdrawn source", e)
	}
	historical, e = f.s.StopHumanCollectionCopyOperation(f.ctx, f.peerDigest, f.peer, in.OperationID)
	if e != nil || historical.Status != "SUCCEEDED" {
		t.Fatal("stop erased original success", e)
	}
	edit := current.CollectionInput
	edit.Title = "本人改标题"
	for i := range edit.Entries {
		entry := edit.Entries[i]
		edit.Entries[i] = content.CollectionEntry{PlaceID: entry.PlaceID, Note: entry.Note, LocationVisibility: entry.LocationVisibility}
	}
	current, e = f.s.UpdateHumanCollection(f.ctx, f.peerDigest, f.peer, current.ID, current.Revision, edit)
	if e != nil {
		t.Fatal(e)
	}
	if current.Entries[0].Available {
		t.Fatal("ordinary title edit detached origin")
	}
	removed := current.Entries[0].PlaceID
	edit = current.CollectionInput
	edit.Entries = []content.CollectionEntry{{PlaceID: current.Entries[1].PlaceID, LocationVisibility: "hidden"}}
	current, e = f.s.UpdateHumanCollection(f.ctx, f.peerDigest, f.peer, current.ID, current.Revision, edit)
	if e != nil {
		t.Fatal(e)
	}
	appended, e := f.s.AppendHumanCollectionPlaces(f.ctx, f.peerDigest, f.peer, current.Kind, current.ID, content.CollectionAppendInput{OperationID: f.uuid(), ExpectedRevision: current.Revision, CityID: current.CityID, Entries: []content.CollectionAppendEntry{{PlaceID: removed, LocationVisibility: "hidden"}}})
	if e != nil || appended.Status != "SUCCEEDED" {
		t.Fatal("explicit independent canonical reselect rejected", e)
	}
	current, e = f.s.GetHumanCollection(f.ctx, f.peerDigest, f.peer, current.Kind, current.ID)
	if e != nil || len(current.Entries) != 2 || current.Entries[0].Available || !current.Entries[1].Available || current.Entries[1].PlaceName == "" || current.Entries[1].Latitude == nil {
		t.Fatal("reselect borrowed old labels or failed own current Place", e)
	}
	edit = current.CollectionInput
	edit.Entries = []content.CollectionEntry{{PlaceID: removed, LocationVisibility: "hidden"}}
	current, e = f.s.UpdateHumanCollection(f.ctx, f.peerDigest, f.peer, current.ID, current.Revision, edit)
	if e != nil || current.PrivatePlaceCopyRestricted {
		t.Fatal("explicit independent selection kept false publishing restriction", e)
	}
	var frozen string
	if e = f.pool.QueryRow(f.ctx, `SELECT status FROM personal_collection_copy_operations WHERE receiver_account_id=$1 AND operation_id=$2`, f.peer.ID, in.OperationID).Scan(&frozen); e != nil || frozen != "SUCCEEDED" {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, `UPDATE personal_collection_copy_operations SET status='STOPPED' WHERE receiver_account_id=$1 AND operation_id=$2`, f.peer.ID, in.OperationID)
	if e == nil || !strings.Contains(e.Error(), "immutable") {
		t.Fatal("copy history mutable", e)
	}
}
