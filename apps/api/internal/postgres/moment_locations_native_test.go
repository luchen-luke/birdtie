package postgres

import (
	"bytes"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"image"
	"image/jpeg"
	"reflect"
	"testing"
	"time"
)

// One synthetic, independently owned native chain. Root supplies the guarded
// disposable database; no production identities, media or services are used.
func TestMomentLocationsNativeAtomicCurrentProjectionAndRecovery(t *testing.T) {
	f := savedPageNative(t)
	_, first := f.bookmark(t)
	permission := mc144RetainedPlace(t, f, first, 0)
	city := "synthetic160-" + f.id(t)
	f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'第二合成城市','合成地区','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','disposable://locations160','合成')`, city)
	second := f.id(t)
	f.exec(t, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,$2,'第二合成地点','park',52,1,'point','published','LOCAL_SYNTHETIC_FIXTURE','disposable://locations160','合成')`, second, city)
	links := []content.MomentLocationInput{{ID: f.id(t), Kind: "PLACE", PlaceID: first, Precision: "place"}, {ID: f.id(t), Kind: "PLACE", PlaceID: second, Precision: "place"}, {ID: f.id(t), Kind: "CITY", CityID: city, Precision: "none"}}
	value := content.MomentInput{Body: "无标题的合成正文160", TimePrecision: "unknown", LocationPrecision: "none", Locations: &links}
	draft, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &value})
	var rawEmpty bool
	if e := f.pool.QueryRow(f.ctx, `SELECT city_id IS NULL AND place_id IS NULL AND location_precision='none' FROM moments WHERE id=$1`, draft.MomentID).Scan(&rawEmpty); e != nil || !rawEmpty {
		t.Fatal("multi-location became primary geography", e)
	}
	var pixels bytes.Buffer
	if e := jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); e != nil {
		t.Fatal(e)
	}
	scope := f.id(t)
	access := mm.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	staged, e := f.store.PrepareMomentMedia(f.ctx, access, mm.StageInput{OperationID: f.id(t), LocalScope: scope, ContentType: "image/jpeg", Content: pixels.Bytes()})
	if e != nil || staged.Material == nil {
		t.Fatal("actual approved image preparation", e)
	}
	attachments := []mm.AttachmentInput{{MediaID: staged.Material.MediaID, MediaVersion: staged.Material.Version, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	published, original := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: &value, LocalScope: scope, Media: &attachments})
	originalInput := *original.Input
	originalLinks := append([]content.MomentLocationInput{}, (*original.Input.Locations)...)
	originalInput.Locations = &originalLinks
	original.Input = &originalInput
	id, revision := published.MomentID, published.AppliedRevision
	owner := content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}
	read := func(own bool) content.MomentLocationsView {
		t.Helper()
		a := content.PublicMomentAccess{}
		if own {
			a = owner
		}
		r, e := f.store.CaptureMomentLocations(f.ctx, a, id, own)
		if e != nil {
			t.Fatal("native location capture", e)
		}
		v, e := f.store.FinalizeMomentLocations(f.ctx, a, r)
		if e != nil {
			t.Fatal("native location final frame", e)
		}
		return v
	}
	owned := read(true).Items.([]content.MomentLocationResolved)
	visible := read(false).Items.([]content.PublicMomentLocation)
	if len(owned) != 3 || len(visible) != 2 || owned[0].ID != links[0].ID || visible[0].DisplayOrder != 0 || visible[1].DisplayOrder != 1 || visible[0].Anchor == nil || visible[1].Anchor == nil {
		t.Fatal("stable owned/public projection or hidden relation leaked")
	}
	peer, _ := orgHumanNativePerson(t, f)
	if _, e = f.store.CaptureMomentLocations(f.ctx, content.PublicMomentAccess{Actor: identity.Actor{ID: peer.AccountID, AccountType: "person"}, SessionDigest: peer.SessionDigest}, id, true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("foreign owner locations readable", e)
	}
	pins := func(city string) []mp.Item {
		t.Helper()
		r, e := f.store.ReadMapLayers(f.ctx, mp.Access{}, mp.Query{CityID: city, West: -180, South: -90, East: 180, North: 90, Locations: true, MomentMedia: true})
		if e != nil {
			t.Fatal("native v5 map", e)
		}
		out := []mp.Item{}
		for _, v := range r.View.Items {
			if v.Kind == "MOMENT" && v.ID == id {
				out = append(out, v)
			}
		}
		return out
	}
	a, b := pins("aberdeen-gb"), pins(city)
	if len(a) != 1 || len(b) != 1 || a[0].PinIdentity != links[0].ID || b[0].PinIdentity != links[1].ID || a[0].Detail.ID != id || b[0].Detail.ID != id {
		t.Fatal("cross-city pins lost relation identity or changed detail")
	}
	for _, place := range []string{first, second} {
		summary, e := f.store.GetPublicPlaceSocialHistory(f.ctx, ph.Access{}, place)
		if e != nil || summary.RecentMomentCount != 1 {
			t.Fatal("distinct Moment place count", e)
		}
	}
	// Reorder preserves identities and atomically carries the already approved
	// media through the parent revision; no current rights are fabricated.
	links[0], links[1] = links[1], links[0]
	updated, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &value})
	revision = updated.AppliedRevision
	if got := read(true).Items.([]content.MomentLocationResolved); got[0].ID != links[0].ID || got[1].ID != links[1].ID {
		t.Fatal("reorder allocated new relation identities")
	}
	legacy := value
	legacy.Locations = nil
	legacy.Body = "旧客户端正文编辑保留完整集合"
	updated, _ = mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &legacy})
	revision = updated.AppliedRevision
	if len(read(true).Items.([]content.MomentLocationResolved)) != 3 {
		t.Fatal("omission cleared the full selection")
	}
	badLegacy := legacy
	badLegacy.CityID, badLegacy.LocationPrecision = city, "city"
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &badLegacy}); !errors.Is(e, content.ErrMomentLocationsClientRequired) {
		t.Fatal("old single selector truncated multiple locations", e)
	}
	f.exec(t, `UPDATE place_source_permissions SET state='PROHIBITED',version=version+1,revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, permission)
	visible = read(false).Items.([]content.PublicMomentLocation)
	if len(visible) != 1 || visible[0].PlaceID != second || visible[0].DisplayOrder != 0 {
		t.Fatal("one revoked source retired unrelated public relation")
	}
	for _, r := range read(true).Items.([]content.MomentLocationResolved) {
		if r.PlaceID == first && (r.Available || r.PlaceName != "" || r.CityName != "" || r.Anchor != nil) {
			t.Fatal("retired source retained old metadata")
		}
	}
	if len(pins("aberdeen-gb")) != 0 || len(pins(city)) != 1 {
		t.Fatal("independent pin retirement failed")
	}
	if v, e := f.store.GetPublicMoment(f.ctx, content.PublicMomentAccess{}, id); e != nil || v.Body != legacy.Body {
		t.Fatal("location retirement hid body", e)
	}
	if set, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, id); e != nil || set.Status != "READY" || len(set.Attachments) != 1 {
		t.Fatal("location retirement hid current152 media", e)
	}
	if replay, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, original); e != nil || !reflect.DeepEqual(replay, published) {
		t.Fatal("source retirement changed original publication fact", e)
	}
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成作者160','public')`, f.actor.ID)
	f.exec(t, `INSERT INTO agents(id,agent_type,principal_account_id,status)VALUES($1,'personal',$2,'active')`, f.id(t), f.actor.ID)
	for _, q := range []content.MomentHistoryQuery{{Scope: "author", AuthorID: f.actor.ID}, {Scope: "city", CityID: city}} {
		var author any
		if q.AuthorID != "" {
			author = q.AuthorID
		}
		var proof string
		var pageJSON []byte
		var deadline *time.Time
		var cityOK, authorOK bool
		if e := f.pool.QueryRow(f.ctx, publicHistorySQL(), nil, nil, make([]byte, 32), f.store.devPhoneEnabled, true, q.Scope, q.CityID, author, nil, nil, time.Now().UTC(), nil, nil).Scan(&proof, &pageJSON, &deadline, &cityOK, &authorOK); e != nil {
			t.Fatalf("synthetic history SQL scope=%s: %v", q.Scope, e)
		}
		if _, e := f.store.CapturePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, id, true, true); e != nil {
			t.Fatalf("synthetic history item context scope=%s: %v", q.Scope, e)
		}
		page, e := f.store.ReadPublicMomentHistory(f.ctx, content.PublicMomentAccess{}, q)
		if e != nil || len(page.Items) != 1 || page.Items[0].ID != id {
			t.Fatalf("history dropped or duplicated multi-location content scope=%s: %v", q.Scope, e)
		}
	}
	// Existing unavailable relation can stay unchanged in a new body approval.
	retained := value
	retained.Body = "保留失效位置关系但不公开其标签"
	updated, _ = mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &retained})
	revision = updated.AppliedRevision
	// A newly selected second source retiring between preview/commit causes no
	// partial parent, relation, attachment, audit or original operation success.
	third := f.id(t)
	f.exec(t, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,$2,'第三合成地点','park',52.1,1.1,'point','published','LOCAL_SYNTHETIC_FIXTURE','disposable://locations160','合成')`, third, city)
	fresh := []content.MomentLocationInput{links[0], {ID: f.id(t), Kind: "PLACE", PlaceID: third, Precision: "place"}}
	failed := retained
	failed.Locations = &fresh
	in, _ := mc144Preview(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &failed})
	before := mc144Count(t, f)
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, third)
	if _, e = f.store.CommitMomentControl(f.ctx, f.digest, f.actor, in); e == nil {
		t.Fatal("retired newly selected source committed")
	}
	mc144NoEffect(t, f, before)
	if current, e := f.store.GetHumanMoment(f.ctx, f.digest, f.actor, id); e != nil || current.Revision != revision {
		t.Fatal("failed selection changed parent", e)
	}
	empty := []content.MomentLocationInput{}
	cleared := retained
	cleared.Locations = &empty
	updated, _ = mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &cleared})
	revision = updated.AppliedRevision
	if len(read(true).Items.([]content.MomentLocationResolved)) != 0 || len(read(false).Items.([]content.PublicMomentLocation)) != 0 || len(pins(city)) != 0 {
		t.Fatal("explicit empty selection did not retire every relation")
	}
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: id, ExpectedRevision: revision, Input: &retained}); e == nil {
		t.Fatal("removed stable IDs were resurrected")
	}
	if v, e := f.store.GetPublicMoment(f.ctx, content.PublicMomentAccess{}, id); e != nil || v.Body != retained.Body {
		t.Fatal("clear removed content", e)
	}
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: id, ExpectedRevision: revision})
	if _, e = f.store.CaptureMomentLocations(f.ctx, content.PublicMomentAccess{}, id, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn location sidecar remained public", e)
	}
	// The retained snapshot endpoint also discloses and seals the full set.
	oldLinks := []content.MomentLocationInput{{ID: f.id(t), Kind: "PLACE", PlaceID: second, Precision: "place"}, {ID: f.id(t), Kind: "CITY", CityID: "aberdeen-gb", Precision: "city"}}
	oldDraft, e := f.store.CreateHumanMomentDraft(f.ctx, f.digest, f.actor, content.MomentInput{Body: "旧快照旁路的完整合成集合", TimePrecision: "unknown", LocationPrecision: "none", Locations: &oldLinks})
	if e != nil {
		t.Fatal("legacy writer complete location selection", e)
	}
	oldPreview, e := f.store.PreviewHumanMomentPublication(f.ctx, f.digest, f.actor, oldDraft.ID)
	if e != nil || len(oldPreview.Locations) != 2 || oldPreview.PlaceID != "" || oldPreview.CityID != "" || !content.MomentControlHex(oldPreview.LocationsSourceVersion) {
		t.Fatal("legacy preview concealed complete selection", e)
	}
	if _, e = f.store.PublishHumanMoment(f.ctx, f.digest, f.actor, oldDraft.ID, content.MomentPublicationInput{Revision: oldDraft.Revision, Snapshot: oldPreview.Snapshot, ConfirmPublic: true}); e != nil {
		t.Fatal("legacy full-set publication failed", e)
	}
	// The153 transaction carries independently ordered relation sets and a
	// real media-only child; geographic retirement cannot withdraw either child.
	// Prepare its own independent stage, rather than deriving any new authority
	// from the earlier Moment's now withdrawn collection.
	threadScope := f.id(t)
	threadStage, e := f.store.PrepareMomentMedia(f.ctx, access, mm.StageInput{OperationID: f.id(t), LocalScope: threadScope, ContentType: "image/jpeg", Content: pixels.Bytes()})
	if e != nil || threadStage.Material == nil {
		t.Fatal("actual independent thread image preparation", e)
	}
	threadAttachments := []mm.AttachmentInput{{MediaID: threadStage.Material.MediaID, MediaVersion: threadStage.Material.Version, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	firstThreadLinks := []content.MomentLocationInput{{ID: f.id(t), Kind: "PLACE", PlaceID: second, Precision: "place"}}
	secondThreadLinks := []content.MomentLocationInput{{ID: f.id(t), Kind: "CITY", CityID: "aberdeen-gb", Precision: "city"}}
	thread := content.MomentThreadInput{OperationID: f.id(t), Action: "CREATE_PUBLISH", LocalScope: threadScope, Audience: "PUBLIC", Items: []content.MomentThreadItem{{Input: content.MomentInput{TimePrecision: "unknown", LocationPrecision: "none", Locations: &firstThreadLinks}, Media: threadAttachments}, {Input: content.MomentInput{Body: "第二条合成串文正文", TimePrecision: "unknown", LocationPrecision: "none", Locations: &secondThreadLinks}, Media: []mm.AttachmentInput{}}}}
	preview, e := f.store.PreviewMomentThread(f.ctx, f.digest, f.actor, thread)
	if e != nil || len(preview.Locations) != 2 || len(preview.Locations[0]) != 1 || len(preview.Locations[1]) != 1 {
		t.Fatal("thread did not preview every entry location", e)
	}
	thread.SourceVersion, thread.Approval = preview.SourceVersion, preview.Approval
	threadFact, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, thread)
	if e != nil || len(threadFact.Members) != 2 {
		t.Fatal("atomic thread locations/media-only publication", e)
	}
	f.exec(t, `UPDATE cities SET publication_status='hidden' WHERE id=$1`, city)
	threadView, e := f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, threadFact.ThreadID, false)
	if e != nil || len(threadView.Members) != 2 || threadView.Members[0].MomentID == nil || threadView.Members[1].MomentID == nil {
		t.Fatal("location expiry withdrew thread children", e)
	}
	if set, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, threadFact.Members[0].MomentID); e != nil || set.Status != "READY" {
		t.Fatal("geography retired media-only content", e)
	}
}
