package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func journeyPublicationDraft(t *testing.T, f *collectionPublicationNativeFixture) (content.PersonalCollection, content.CollectionPublicationInput, []string) {
	t.Helper()
	actor := &savedPageNativeFixture{ctx: f.ctx, pool: f.pool, store: f.s, actor: f.owner, digest: f.digest, session: f.session}
	mids := []string{}
	for i := 0; i < 3; i++ {
		in := mc144Input(t, actor)
		in.Input.Body = "本机隔离批准公开动态"
		draft, _ := mc144Write(t, actor, in)
		mc144Write(t, actor, content.MomentControlInput{OperationID: f.uuid(), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: in.Input})
		mids = append(mids, draft.MomentID)
	}
	entries := []content.JourneyEntry{{ID: f.uuid(), EntryType: "STOP", Position: 1, Title: "PRIVATE_STOP_TITLE", Note: "PRIVATE_STOP_NOTE", CityID: "aberdeen-gb", PlaceID: f.places[0]}, {ID: f.uuid(), EntryType: "NOTE", Position: 2, Note: "PRIVATE_BUDGET_MEMBERS_ACCOMMODATION"}, {ID: f.uuid(), EntryType: "STOP", Position: 3, Title: "PRIVATE_THIRD", CityID: "aberdeen-gb", PlaceID: f.places[1]}, {ID: f.uuid(), EntryType: "STOP", Position: 4, CityID: "aberdeen-gb", PlaceID: f.places[2]}}
	v, e := f.s.CreateHumanCollection(f.ctx, f.digest, f.owner, content.CollectionCreate{OperationID: f.uuid(), CollectionInput: content.CollectionInput{Kind: "journey", Title: "PRIVATE_PLAN_TITLE", Summary: "PRIVATE_PLAN_SUMMARY", Entries: []content.CollectionEntry{}, JourneyDetails: &content.JourneyDetails{SchemaVersion: content.JourneyDetailsSchema, StartDate: "2099-01-01", EndDate: "2099-01-03", Days: 3, Itinerary: entries}}})
	if e != nil {
		t.Fatal("rich original draft", e)
	}
	choices := []content.JourneyPublicationStopInput{}
	for i, index := range []int{0, 2, 3} {
		choices = append(choices, content.JourneyPublicationStopInput{EntryID: entries[index].ID, Position: entries[index].Position, Title: "本人批准站名", Note: "本人批准说明", LocationVisibility: "place", MomentID: mids[i], MomentRevision: 2, CommentMomentID: mids[i]})
	}
	return v, content.CollectionPublicationInput{OperationID: f.uuid(), Revision: v.Revision, Entries: []content.CollectionPublicationEntry{}, JourneySnapshot: &content.JourneyPublicationInput{Title: "明确公开旅程", Summary: "只公开批准摘要", Stops: choices}}, mids
}

func TestJourneyPublicationNativeSnapshotPrivateEditRecoveryAndRetirement(t *testing.T) {
	f := collectionPublicationFixture(t)
	draft, in, mids := journeyPublicationDraft(t, f)
	access := content.PublicMomentAccess{Actor: f.peer, SessionDigest: f.peerDigest}
	if _, e := f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("private schedule discovered", e)
	}
	wrong := in
	wrong.OperationID = f.uuid()
	bad := *in.JourneySnapshot
	bad.Stops = append([]content.JourneyPublicationStopInput{}, bad.Stops...)
	bad.Stops[0].Position = 2
	wrong.JourneySnapshot = &bad
	if _, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, wrong); !errors.Is(e, content.ErrConflict) {
		t.Fatal("private nonSTOP/reordered publication", e)
	}
	pub, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, in)
	if e != nil || pub.Visibility != "private" || pub.Status != "draft" || pub.Revision != 2 || pub.PublishedSnapshot == nil || pub.JourneyLifecycle == nil || pub.JourneyLifecycle.State != "NOT_STARTED" {
		t.Fatal("same rich private identity publication", e)
	}
	if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, in); e != nil {
		t.Fatal("original immutable replay", e)
	}
	recovered, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID)
	if e != nil || recovered.PublicationOperation.ResultRevision != 2 {
		t.Fatal("unknown original result", e)
	}
	public, e := f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID)
	if e != nil || public.JourneySnapshot == nil || len(public.JourneySnapshot.Stops) != 3 || public.JourneySnapshot.Stops[1].Position != 3 {
		t.Fatal("approved public snapshot", e)
	}
	if e = f.s.RevalidatePublicCollection(f.ctx, access, public); e != nil {
		t.Fatal("original fixed public proof", e)
	}
	raw, _ := json.Marshal(public)
	for _, private := range []string{"PRIVATE_", "journeyDetails", "transportMode", "origin", "sourceText", "customAddress"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private schedule serialized", private)
		}
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	edges, e := f.s.ReadJourneyPublicationEdgesTx(f.ctx, tx, access, mids, 200)
	_ = tx.Rollback(context.Background())
	if e != nil || len(edges) != 1 || edges[0].FromPosition != 3 || edges[0].ToPosition != 4 {
		t.Fatal("public adjacent authority gap", e, edges)
	}
	changed := pub.CollectionInput
	details := *pub.JourneyDetails
	details.SourceVersion = ""
	details.Itinerary = append([]content.JourneyEntry{}, details.Itinerary...)
	details.Itinerary[0].Note = "PRIVATE_EDIT_MUST_NOT_UPDATE_PUBLIC"
	changed.JourneyDetails = &details
	changed.Title = "PRIVATE_REVISED_TITLE"
	updated, e := f.s.UpdateHumanCollection(f.ctx, f.digest, f.owner, draft.ID, 2, changed)
	if e != nil || updated.Revision != 3 {
		t.Fatal("private edit while published", e)
	}
	fresh, e := f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID)
	if e != nil || fresh.Revision != 2 || fresh.Title != public.Title || fresh.JourneySnapshot.Stops[0].Note != public.JourneySnapshot.Stops[0].Note {
		t.Fatal("private edit changed public version", e)
	}
	if e = f.s.RevalidatePublicCollection(f.ctx, access, public); e != nil {
		t.Fatal("unpublished private edit retired unchanged public fact", e)
	}
	page, e := f.s.ListJourneyDiscovery(f.ctx, f.peerDigest, f.peer, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != draft.ID {
		t.Fatal("real visible discovery", e)
	}
	if e = f.s.RevalidateJourneyDiscovery(f.ctx, f.peerDigest, f.peer, page); e != nil {
		t.Fatal("discovery final lease", e)
	}
	peer := &savedPageNativeFixture{ctx: f.ctx, pool: f.pool, store: f.s, actor: f.peer, digest: f.peerDigest, session: f.peerSession}
	hcNativeBlock(t, peer, identity.BlockControlInput{OperationID: f.uuid(), Action: "BLOCK", AccountID: f.owner.ID})
	if _, e = f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("blocked published Journey", e)
	}
	hcNativeBlock(t, peer, identity.BlockControlInput{OperationID: f.uuid(), Action: "UNBLOCK", AccountID: f.owner.ID, ExpectedVersion: 1})
	if e = f.s.RevalidatePublicCollection(f.ctx, access, public); e == nil {
		t.Fatal("block/unblock ABA revived old proof")
	}
	f.exec(`UPDATE moments SET revision=revision+1 WHERE id=$1`, mids[1])
	fresh, e = f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID)
	if e != nil {
		t.Fatal(e)
	}
	gap := fresh.JourneySnapshot.Stops[1]
	if gap.Available || gap.Position != 3 || gap.Title != "" || gap.Note != "" || gap.MomentID != "" || gap.PlaceID != nil {
		t.Fatal("changed original Moment leaked stale public stop", gap)
	}
	unpub, e := f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, content.CollectionUnpublishInput{OperationID: f.uuid(), Revision: 3})
	if e != nil || unpub.Revision != 4 || unpub.PublishedSnapshot != nil {
		t.Fatal("explicit original unpublish", e)
	}
	if _, e = f.s.GetPublicCollection(f.ctx, access, "journey", draft.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("unpublished snapshot survived", e)
	}
	var facts int
	if f.pool.QueryRow(f.ctx, `SELECT count(*)FROM journey_publication_snapshots WHERE journey_id=$1`, draft.ID).Scan(&facts) != nil || facts != 1 {
		t.Fatal("immutable publication facts lost/duplicated")
	}
}

func TestJourneyPublicationNativeDiscoveryPaginationAndUnknownLifecycle(t *testing.T) {
	f := collectionPublicationFixture(t)
	for i := 0; i < 22; i++ {
		v, _ := f.publish("journey")
		if v.JourneyDetails != nil {
			t.Fatal("simple fixture replaced")
		}
	}
	page, e := f.s.ListJourneyDiscovery(f.ctx, f.peerDigest, f.peer, "")
	if e != nil || len(page.Items) != 20 || page.NextCursor == "" {
		t.Fatal("first actual sorted public page", e, len(page.Items))
	}
	next, e := f.s.ListJourneyDiscovery(f.ctx, f.peerDigest, f.peer, page.NextCursor)
	if e != nil || len(next.Items) != 2 || next.NextCursor != "" {
		t.Fatal("second actual page", e, len(next.Items))
	}
	seen := map[string]bool{}
	for _, p := range append(page.Items, next.Items...) {
		if seen[p.ID] {
			t.Fatal("duplicate across discovery pages")
		}
		seen[p.ID] = true
	}
	if _, e = f.s.ListJourneyDiscovery(f.ctx, f.digest, f.owner, page.NextCursor); !errors.Is(e, content.ErrInvalid) {
		t.Fatal("cursor crossed viewer", e)
	}
	v, e := f.s.CreateHumanCollection(f.ctx, f.digest, f.owner, content.CollectionCreate{OperationID: f.uuid(), CollectionInput: content.CollectionInput{Kind: "journey", Title: "无日期无地点私人计划", Entries: []content.CollectionEntry{}, JourneyDetails: &content.JourneyDetails{SchemaVersion: content.JourneyDetailsSchema, Itinerary: []content.JourneyEntry{}}}})
	if e != nil || v.JourneyLifecycle == nil || v.JourneyLifecycle.State != "UNKNOWN" {
		t.Fatal("missing date/timezone guessed", e)
	}
	ownerPage, e := f.s.ListHumanCollections(f.ctx, f.digest, f.owner, "journey", "")
	if e != nil || len(ownerPage.Items) != 20 || !strings.HasPrefix(ownerPage.NextCursor, "jl1.") {
		t.Fatal("own crosscity list pagination", e)
	}
	ownerNext, e := f.s.ListHumanCollections(f.ctx, f.digest, f.owner, "journey", ownerPage.NextCursor)
	if e != nil || len(ownerNext.Items) != 3 {
		t.Fatal("own cursor continuation", e, len(ownerNext.Items))
	}
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.peerSession)
	if e = f.s.RevalidateJourneyDiscovery(f.ctx, f.peerDigest, f.peer, page); e == nil {
		t.Fatal("revoked viewer retained old discovery")
	}
}
