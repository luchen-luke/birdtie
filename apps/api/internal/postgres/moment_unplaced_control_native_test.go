package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mapdto "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
)

// One routed, native chain in the independently owned disposable fixture.
// Root runs it with the final 156 schema; it never alters a shared/real source.
func TestMomentUnplacedControlNativeOwnedEditAndPublicationBoundary(t *testing.T) {
	f := savedPageNative(t)
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal("current synthetic token")
	}
	f.digest = digest
	f.exec(t, `UPDATE sessions SET token_sha256=$2 WHERE id=$1`, f.session, digest[:])
	router := httpapi.New(f.store, f.store, nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	request := func(method, path string, value any) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if value != nil {
			if json.NewEncoder(&body).Encode(value) != nil {
				t.Fatal("synthetic wire input")
			}
		}
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Authorization", "Bearer "+token)
		if value != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	write := func(in content.MomentControlInput) (content.MomentControlReceipt, content.MomentControlInput) {
		w := request(http.MethodPost, "/v1/me/moment-controls/previews", in)
		var preview struct{ Data content.MomentControlPreview }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || content.MomentControlInputHash(preview.Data.Input) != content.MomentControlInputHash(in.Input) {
			t.Fatal("actual unplaced preview route", w.Code)
		}
		in.SourceVersion, in.Approval = preview.Data.SourceVersion, preview.Data.Approval
		w = request(http.MethodPut, "/v1/me/moment-controls/operations/"+in.OperationID, in)
		var result struct{ Data content.MomentControlReceipt }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !content.ValidMomentControlReceipt(result.Data, in.OperationID) {
			t.Fatal("actual unplaced commit route", w.Code)
		}
		return result.Data, in
	}
	value := content.MomentInput{CityID: "", Title: "仅隔离无城市旧记录", Body: "本人保全正文", TimePrecision: "unknown", LocationPrecision: "none"}
	created, approved := write(content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &value})
	var isNull bool
	if err = f.pool.QueryRow(f.ctx, `SELECT city_id IS NULL AND place_id IS NULL AND location_precision='none' AND visibility='private' AND status='draft' FROM moments WHERE id=$1`, created.MomentID).Scan(&isNull); err != nil || !isNull {
		t.Fatal("unknown location did not persist as private NULL", err)
	}
	get := func() content.Moment {
		w := request(http.MethodGet, "/v1/me/moments/"+created.MomentID, nil)
		var result struct{ Data content.Moment }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.CityID != "" {
			t.Fatal("NULL city did not survive registered own reader", w.Code)
		}
		return result.Data
	}
	if got := get(); got.Revision != 1 || got.Body != value.Body {
		t.Fatal("current own unknown-city body unavailable")
	}
	value.Body = "本人编辑后的保全正文"
	update := content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE", MomentID: created.MomentID, ExpectedRevision: 1, Input: &value}
	updated, updateApproved := write(update)
	if got := get(); got.Revision != 2 || got.Body != value.Body || got.PlaceID != "" || len(got.ActivityIDs) != 0 {
		t.Fatal("ordinary unplaced editing lost source or added a context")
	}
	historyResponse := request(http.MethodGet, "/v1/me/moments/history?status=active", nil)
	var history struct{ Data content.OwnMomentHistoryPage }
	if historyResponse.Code != 200 || json.Unmarshal(historyResponse.Body.Bytes(), &history) != nil || len(history.Data.Items) != 1 || history.Data.Items[0].ID != created.MomentID || history.Data.Items[0].CityID != "" || history.Data.Items[0].Body != value.Body {
		t.Fatal("registered owner history omitted unplaced private content", historyResponse.Code)
	}
	// Original facts and replay never depend on a fabricated City or old body.
	replayed, err := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, updateApproved)
	if err != nil || content.MomentControlHash(replayed) != content.MomentControlHash(updated) {
		t.Fatal("unplaced original operation changed", err)
	}
	w := request(http.MethodGet, "/v1/me/moment-controls/operations/"+approved.OperationID, nil)
	var original struct{ Data content.MomentControlReceipt }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &original) != nil || content.MomentControlHash(original.Data) != content.MomentControlHash(created) {
		t.Fatal("unplaced original CREATE fact unavailable", w.Code)
	}
	peer, _ := orgHumanNativePerson(t, f)
	if _, err = f.store.GetHumanMoment(f.ctx, peer.SessionDigest, identity.Actor{ID: peer.AccountID, AccountType: "person"}, created.MomentID); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("unplaced private record crossed owner", err)
	}
	// Unknown-city approvals still bind the original principal row and clock.
	stale := update
	stale.OperationID, stale.ExpectedRevision = f.id(t), 2
	stale, _ = mc144Preview(t, f, stale)
	f.exec(t, `UPDATE accounts SET status=status WHERE id=$1`, f.actor.ID)
	if _, err = f.store.CommitMomentControl(f.ctx, f.digest, f.actor, stale); !errors.Is(err, identity.ErrControlChanged) {
		t.Fatal("unknown-city approval survived a principal epoch change", err)
	}
	future := value
	at := time.Now().Add(72 * time.Hour)
	future.OccurredAt, future.TimePrecision = &at, "instant"
	if _, err = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &future}); !errors.Is(err, identity.ErrControlInvalid) {
		t.Fatal("unknown-city branch skipped current future-clock check", err)
	}
	// Both owner writer paths permit an explicit removal of chosen geography.
	legacy, err := f.store.CreateMomentDraft(f.ctx, f.actor.ID, value)
	if err != nil || legacy.CityID != "" {
		t.Fatal("legacy private writer diverged from unplaced rules", err)
	}
	if legacy, err = f.store.UpdateMomentDraft(f.ctx, f.actor.ID, legacy.ID, legacy.Revision, value); err != nil || legacy.CityID != "" {
		t.Fatal("legacy private update rejected original NULL city", err)
	}
	known := value
	known.CityID, known.LocationPrecision = "aberdeen-gb", "city"
	placed, err := f.store.CreateMomentDraft(f.ctx, f.actor.ID, known)
	if err != nil {
		t.Fatal(err)
	}
	if placed, err = f.store.UpdateMomentDraft(f.ctx, f.actor.ID, placed.ID, placed.Revision, value); err != nil || placed.CityID != "" {
		t.Fatal("explicit City removal failed", err)
	}
	// The old registered snapshot publication is also content-first.
	oldPreview := request(http.MethodGet, "/v1/me/moments/"+placed.ID+"/publication", nil)
	var oldP struct {
		Data content.MomentPublicationPreview
	}
	if oldPreview.Code != 200 || json.Unmarshal(oldPreview.Body.Bytes(), &oldP) != nil {
		t.Fatal("old publication preview still required Place", oldPreview.Code)
	}
	oldCommit := request(http.MethodPost, "/v1/me/moments/"+placed.ID+"/publication", content.MomentPublicationInput{Revision: placed.Revision, Snapshot: oldP.Data.Snapshot, ConfirmPublic: true})
	if oldCommit.Code != 200 {
		t.Fatal("old publication commit failed after source validation", oldCommit.Code)
	}
	value.Title = ""
	// PUBLISH still approves the concrete draft, so remove only its title first.
	untitled, _ := write(content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE", MomentID: created.MomentID, ExpectedRevision: 2, Input: &value})
	textPublished, _ := write(content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: created.MomentID, ExpectedRevision: untitled.AppliedRevision, Input: &value})
	if w = request(http.MethodGet, "/v1/moments/"+created.MomentID, nil); w.Code != 200 {
		t.Fatal("ordinary detail hid public no-city text", w.Code)
	}
	withdraw := content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: created.MomentID, ExpectedRevision: textPublished.AppliedRevision}
	withdrawn, _ := mc144Write(t, f, withdraw)
	if withdrawn.AppliedRevision != textPublished.AppliedRevision+1 {
		t.Fatal("unknown-city terminal revision")
	}
	if err = f.pool.QueryRow(f.ctx, `SELECT city_id IS NULL AND status='withdrawn' AND visibility='private' FROM moments WHERE id=$1`, created.MomentID).Scan(&isNull); err != nil || !isNull {
		t.Fatal("withdrawal invented a City", err)
	}
	// Actual owned JPEG staging and one-transaction pure-media publication,
	// followed by a public edit which carries bytes/rights to new revision.
	empty := content.MomentInput{TimePrecision: "unknown", LocationPrecision: "none"}
	mediaDraft, _ := write(content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &empty})
	mediaAccess := mm.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	scope := f.id(t)
	stage, e := f.store.PrepareMomentMedia(f.ctx, mediaAccess, mm.StageInput{OperationID: f.id(t), LocalScope: scope, ContentType: "image/png", Content: publicPhotoNativeBytes(t, false)})
	if e != nil || stage.Material == nil {
		t.Fatal("real stage", e)
	}
	inputs := []mm.AttachmentInput{{MediaID: stage.Material.MediaID, MediaVersion: stage.Material.Version, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	pub := content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: mediaDraft.MomentID, ExpectedRevision: 1, Input: &empty, LocalScope: scope, Media: &inputs}
	wrong := pub
	wrong.OperationID, wrong.LocalScope = f.id(t), f.id(t)
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, wrong); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("foreign local composition accepted", e)
	}
	missing := []mm.AttachmentInput{inputs[0]}
	missing[0].MediaID = f.id(t)
	wrong = pub
	wrong.OperationID, wrong.Media = f.id(t), &missing
	if _, e = f.store.PreviewMomentControl(f.ctx, f.digest, f.actor, wrong); e == nil {
		t.Fatal("missing media published a partial parent")
	}
	mediaPublished, mediaApproval := write(pub)
	current, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, mediaDraft.MomentID)
	if e != nil || current.Status != "READY" || len(current.Attachments) != 1 || current.MomentRevision != mediaPublished.AppliedRevision {
		t.Fatal("pure media not atomically readable", e)
	}
	oldMedia := current
	if fact, e := f.store.CommitMomentControl(f.ctx, f.digest, f.actor, mediaApproval); e != nil || content.MomentControlHash(fact) != content.MomentControlHash(mediaPublished) {
		t.Fatal("atomic media replay changed fact", e)
	}
	for _, id := range []string{mediaDraft.MomentID, placed.ID} {
		if w = request(http.MethodGet, "/v1/moments/"+id, nil); w.Code != 200 {
			t.Fatal("no-city ordinary PUBLIC detail missing", w.Code)
		}
	}
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成目录作者','public')`, f.actor.ID)
	f.exec(t, `INSERT INTO agents(id,agent_type,principal_account_id,status)VALUES($1,'personal',$2,'active')`, f.id(t), f.actor.ID)
	if page, e := f.store.ReadPublicMomentHistory(f.ctx, content.PublicMomentAccess{}, content.MomentHistoryQuery{Scope: "author", AuthorID: f.actor.ID}); e != nil || len(page.Items) != 2 {
		t.Fatal("author history omitted no-city body/media", e)
	}
	_, nativePlace := f.bookmark(t)
	placeOnly := content.MomentInput{Body: "仅明确选择地点", PlaceID: nativePlace, TimePrecision: "unknown", LocationPrecision: "place"}
	placeDraft, _ := write(content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &placeOnly})
	placePublic, _ := write(content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: placeDraft.MomentID, ExpectedRevision: 1, Input: &placeOnly})
	placeProof, e := f.store.CapturePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, placePublic.MomentID, false, false)
	if e != nil {
		t.Fatal("Place-only current source", e)
	}
	placeView, e := f.store.FinalizePublicMomentWithContext(f.ctx, content.PublicMomentAccess{}, placeProof)
	if e != nil || placeView.CityID != "" || placeView.CityName != "" || placeView.PlaceID != nativePlace {
		t.Fatal("Place-only publication inferred author City or disappeared", e)
	}
	mapRead, e := f.store.ReadMapLayers(f.ctx, mapdto.Access{}, mapdto.Query{CityID: "aberdeen-gb", West: -2.2, South: 57.1, East: -2.0, North: 57.3})
	if e != nil {
		t.Fatal("actual map SQL for untitled NULL-city real Place", e)
	}
	mapMatches := 0
	for _, item := range mapRead.View.Items {
		if item.Kind != "MOMENT" {
			continue
		}
		if item.ID == mediaDraft.MomentID || item.ID == placed.ID {
			t.Fatal("unplaced publication received a fake map point")
		}
		if item.ID == placePublic.MomentID {
			mapMatches++
			if item.Title != "" || item.Anchor.PlaceID != nativePlace || item.Anchor.Latitude != 57.15 || item.Anchor.Longitude != -2.1 || item.Anchor.CoordinateSystem != "wgs84" || item.Anchor.Precision != "point" || item.Entity.ID != placePublic.MomentID || item.Detail.ID != placePublic.MomentID {
				t.Fatal("map substituted title, point, or canonical Moment identity")
			}
		}
	}
	if mapMatches != 1 {
		t.Fatal("actual public map omitted untitled Place-only Moment", mapMatches)
	}
	// A selected City may be added and explicitly removed without a fake pin.
	withCity := empty
	withCity.CityID, withCity.LocationPrecision = "aberdeen-gb", "city"
	edit := content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: mediaDraft.MomentID, ExpectedRevision: mediaPublished.AppliedRevision, Input: &withCity}
	addCity, _ := write(edit)
	edit.OperationID, edit.ExpectedRevision, edit.Input = f.id(t), addCity.AppliedRevision, &empty
	removed, _ := write(edit)
	current, e = f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, mediaDraft.MomentID)
	if e != nil || current.Status != "READY" || len(current.Attachments) != 1 || current.MomentRevision != removed.AppliedRevision || current.Attachments[0].DisplaySHA != oldMedia.Attachments[0].DisplaySHA || current.Attachments[0].DisplayPath == oldMedia.Attachments[0].DisplayPath || current.Attachments[0].RightsDigest != oldMedia.Attachments[0].RightsDigest {
		t.Fatal("public edit lost bytes/rights or revived old URLs", e)
	}
	if _, e = f.store.ReadPublicMomentMediaContent(f.ctx, mm.Access{}, mm.ContentSelector{MomentID: mediaDraft.MomentID, MomentRevision: oldMedia.MomentRevision, SetVersion: oldMedia.SetVersion, MediaID: inputs[0].MediaID, MediaVersion: inputs[0].MediaVersion, Variant: "display"}, func(_ mm.Attachment, b []byte) ([]byte, error) { return b, nil }); e == nil {
		t.Fatal("old media URL survived revision")
	}
	deletion := mm.Input{OperationID: f.id(t), Action: "DELETE", ExpectedMomentRevision: removed.AppliedRevision, ExpectedSetVersion: current.SetVersion, Attachments: []mm.AttachmentInput{}}
	if _, e = f.store.PreviewMomentMedia(f.ctx, mediaAccess, mediaDraft.MomentID, deletion); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("last media deletion left empty public parent", e)
	}
	for _, body := range []string{"", " \n\t "} {
		titleOnly := empty
		titleOnly.Title = "Title cannot be content"
		titleOnly.Body = body
		if w = request(http.MethodPost, "/v1/me/moment-controls/previews", content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: mediaDraft.MomentID, ExpectedRevision: 1, Input: &titleOnly}); w.Code != 400 {
			t.Fatal("title-only reached publication source", w.Code)
		}
	}
	// Sharing uses the same canonical ID and current PUBLIC authority. Its
	// generated description never writes an invented title to the Moment.
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if label, e := visibleChatEntity(f.ctx, tx, peer.AccountID, "moment", mediaDraft.MomentID); e != nil || label == "" {
		t.Fatal("share resolver hid untitled no-city media", e)
	}
	tx.Rollback(f.ctx)
	var title string
	if e = f.pool.QueryRow(f.ctx, `SELECT title FROM moments WHERE id=$1`, mediaDraft.MomentID).Scan(&title); e != nil || title != "" {
		t.Fatal("share invented author title", e)
	}
	// The final database constraint rejects an empty publication independently
	// of route validation, without blocking ordinary media expiry cleanup.
	if _, e = f.pool.Exec(f.ctx, `INSERT INTO moments(author_account_id,title,body,time_precision,location_precision,status,visibility,revision,author_confirmed_at,published_at)VALUES($1,'title only','','unknown','none','published','public',2,clock_timestamp(),clock_timestamp())`, f.actor.ID); e == nil {
		t.Fatal("native empty public content constraint missing")
	}
	// Original 145 thumbnails keep their own approval/receipt contract. An
	// ordinary edit must ask for explicit reselection instead of silently
	// dropping the thumbnail; deleting its last content is likewise refused.
	legacyPhotoMoment, _ := publicPhotoNativeMoment(t, f, false)
	photoReceipt, _ := publicPhotoNativeSave(t, f, legacyPhotoMoment, publicPhotoNativeInput(t, f))
	legacyTx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = legacyTx.Exec(f.ctx, `UPDATE moments SET title='',body='' WHERE id=$1`, legacyPhotoMoment); e != nil {
		t.Fatal(e)
	}
	if _, e = legacyTx.Exec(f.ctx, `UPDATE public_moment_photos SET source_binding=(SELECT binding FROM birdtie_public_moment_photo_source($1,clock_timestamp())) WHERE moment_id=$1`, legacyPhotoMoment); e != nil {
		t.Fatal(e)
	}
	if e = legacyTx.Commit(f.ctx); e != nil {
		t.Fatal("synthetic original thumbnail-only publication", e)
	}
	legacyMoment, e := f.store.GetHumanMoment(f.ctx, f.digest, f.actor, legacyPhotoMoment)
	if e != nil {
		t.Fatal(e)
	}
	legacyInput := content.MomentInput{CityID: legacyMoment.CityID, PlaceID: legacyMoment.PlaceID, Title: "", Body: "", TimePrecision: legacyMoment.TimePrecision, LocationPrecision: legacyMoment.LocationPrecision}
	blockedUpdate := request(http.MethodPost, "/v1/me/moment-controls/previews", content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: legacyPhotoMoment, ExpectedRevision: legacyMoment.Revision, Input: &legacyInput})
	if blockedUpdate.Code != 409 || !bytes.Contains(blockedUpdate.Body.Bytes(), []byte("legacy_moment_photo_reselection_required")) {
		t.Fatal("legacy photo silently lost or generic rejection", blockedUpdate.Code)
	}
	deletePhoto := mp.Input{OperationID: f.id(t), Action: "DELETE", PhotoID: photoReceipt.PhotoID, ExpectedMomentRevision: legacyMoment.Revision, ExpectedPhotoVersion: photoReceipt.AppliedPhotoVersion}
	if _, e = f.store.PreviewMomentPhoto(f.ctx, publicPhotoNativeAccess(f), legacyPhotoMoment, deletePhoto); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("last legacy photo deletion left blank public content", e)
	}
}
