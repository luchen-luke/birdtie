package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	mapdto "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"github.com/jackc/pgx/v5"
)

func publicPhotoNativeBytes(t *testing.T, other bool) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := color.RGBA{uint8(x * 15), uint8(y * 15), 80, 255}
			if other {
				c = color.RGBA{180, uint8(x * 12), uint8(y * 12), 255}
			}
			im.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func publicPhotoNativeAccess(f *savedPageNativeFixture) mp.Access {
	return mp.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
}
func publicPhotoNativeMoment(t *testing.T, f *savedPageNativeFixture, privateAttachment bool) (string, string) {
	t.Helper()
	_, place := f.bookmark(t)
	m, e := f.store.CreateHumanMomentDraft(f.ctx, f.digest, f.actor, content.MomentInput{CityID: "aberdeen-gb", PlaceID: place, Title: "合成明确公开动态", Body: "明确公开文字与新选照片独立", TimePrecision: "unknown", LocationPrecision: "place"})
	if e != nil {
		t.Fatal(e)
	}
	if privateAttachment {
		raw := publicPhotoNativeBytes(t, true)
		hash := sha256.Sum256(raw)
		in := media.PrivateImageInput{OperationID: f.id(t), MomentRevision: m.Revision, MIME: "image/png", ByteSize: int64(len(raw)), SHA256: hex.EncodeToString(hash[:]), PixelRisk: "UNKNOWN", Purpose: media.PrivateImagePurpose}
		p, e := f.store.PreviewHumanPrivateImage(f.ctx, f.digest, f.actor, m.ID, in)
		if e != nil {
			t.Fatal("actual private input", e)
		}
		ready, e := f.store.SaveHumanPrivateImage(f.ctx, f.digest, f.actor, m.ID, p.ID, "image/png", raw)
		if e != nil || ready.Status != "ready_private" || ready.SHA256 == "" {
			t.Fatal("actual private derivative", e)
		}
		replayed, e := f.store.SaveHumanPrivateImage(f.ctx, f.digest, f.actor, m.ID, p.ID, "image/png", raw)
		if e != nil || replayed.SHA256 != ready.SHA256 || replayed.Revision != ready.Revision {
			t.Fatal("actual private save replay changed the confirmed derivative", e)
		}
		var audits int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='private_image_saved' AND resource_type='media' AND resource_id=$2 AND decision='allowed' AND purpose=$3`, f.actor.ID, p.ID, media.PrivateImagePurpose).Scan(&audits); e != nil || audits != 1 {
			t.Fatal("actual private save/replay did not audit the confirmed purpose once", audits, e)
		}
	}
	f.exec(t, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE moments SET status='published',visibility='public',revision=2,author_confirmed_at=stamp.at,published_at=stamp.at,updated_at=stamp.at FROM stamp WHERE id=$1`, m.ID)
	return m.ID, place
}
func publicPhotoNativeInput(t *testing.T, f *savedPageNativeFixture) mp.Input {
	return mp.Input{OperationID: f.id(t), Action: "SAVE", ExpectedMomentRevision: 2, PhotoID: f.id(t), ContentType: "image/png", Content: publicPhotoNativeBytes(t, false), Rights: &mp.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}
}
func publicPhotoNativeApproved(t *testing.T, f *savedPageNativeFixture, id string, in mp.Input) (mp.Input, mp.Preview) {
	t.Helper()
	v, e := f.store.PreviewMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, in)
	if e != nil || v.PreviewDerivative == nil && in.Action == "SAVE" {
		t.Fatal("actual public approval", e)
	}
	if e = v.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("actual postencode proof", e)
	}
	in.SourceVersion = v.SourceVersion
	in.Approval = v.Approval
	return in, v
}

type publicPhotoNativeCounts struct{ slots, bytes, operations, audits int }

func publicPhotoNativeCount(t *testing.T, f *savedPageNativeFixture) publicPhotoNativeCounts {
	t.Helper()
	var c publicPhotoNativeCounts
	e := f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM public_moment_photos WHERE owner_account_id=$1),(SELECT coalesce(sum(octet_length(thumbnail)),0) FROM public_moment_photos WHERE owner_account_id=$1),(SELECT count(*) FROM public_moment_photo_operations WHERE actor_account_id=$1),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='explicit_public_moment_photo')`, f.actor.ID).Scan(&c.slots, &c.bytes, &c.operations, &c.audits)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func publicPhotoNativeZero(t *testing.T, f *savedPageNativeFixture, before publicPhotoNativeCounts) {
	t.Helper()
	if after := publicPhotoNativeCount(t, f); after != before {
		t.Fatal("late effect/receipt/audit committed", before, after)
	}
}
func publicPhotoNativeSave(t *testing.T, f *savedPageNativeFixture, id string, in mp.Input) (mp.Receipt, mp.Input) {
	t.Helper()
	in, _ = publicPhotoNativeApproved(t, f, id, in)
	r, e := f.store.CommitMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, in)
	if e != nil || !r.Valid(id, in.OperationID) {
		t.Fatal("actual public save", e)
	}
	return r, in
}
func publicPhotoNativeMapItem(t *testing.T, r mapdto.Receipt, id string) mapdto.Item {
	t.Helper()
	for _, i := range r.View.Items {
		if i.Kind == "MOMENT" && i.ID == id {
			return i
		}
	}
	t.Fatal("canonical Moment absent from actual map")
	return mapdto.Item{}
}

func TestPublicMomentPhotoNativeCanonicalProjectionAndIndependentConsent(t *testing.T) {
	f := savedPageNative(t)
	id, place := publicPhotoNativeMoment(t, f, true)
	var privateReady int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM private_moment_images WHERE moment_id=$1 AND(derivative IS NOT NULL OR status<>'deleted')`, id).Scan(&privateReady); e != nil || privateReady != 0 {
		t.Fatal("unchanged100 trigger failed private retirement", e)
	}
	var privateAssets int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM private_moment_images i JOIN media_assets a ON a.id=i.id WHERE i.moment_id=$1 AND(a.state<>'deleted' OR a.deleted_at IS NULL)`, id).Scan(&privateAssets); e != nil || privateAssets != 0 {
		t.Fatal("unchanged100 trigger left a current private asset after publication", e)
	}
	before, e := f.store.CapturePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, id, false)
	if e != nil || before.View.Photo != nil {
		t.Fatal("private derivative automatically became public", e)
	}
	in := publicPhotoNativeInput(t, f)
	r, approved := publicPhotoNativeSave(t, f, id, in)
	counts := publicPhotoNativeCount(t, f)
	if counts.slots != 1 || counts.bytes == 0 || counts.operations != 1 || counts.audits != 1 {
		t.Fatal("canonical public effects not once", counts)
	}
	var agents int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.actor.ID).Scan(&agents); e != nil || agents != 0 {
		t.Fatal("photo introduced Agent prerequisite", e)
	}
	again, e := f.store.CommitMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, approved)
	if e != nil || again.PayloadDigest != r.PayloadDigest {
		t.Fatal("exact original operation replay", e)
	}
	publicPhotoNativeZero(t, f, counts)
	view, e := f.store.CapturePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, id, false)
	if e != nil || view.View.Photo == nil || view.View.Photo.MomentRevision != 2 {
		t.Fatal("actual public v3 projection", e)
	}
	final, e := f.store.FinalizePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, view)
	if e != nil || final.Photo == nil || final.Photo.ExpiresAt != view.View.Photo.ExpiresAt || final.ValidUntil != view.View.ValidUntil || final.ObservedAt != view.View.ObservedAt {
		t.Fatal("v3 refreshed original lease", e)
	}
	meta := *final.Photo
	encoded, e := f.store.ReadMomentPhotoContent(f.ctx, mp.Access{}, mp.Selector{MomentID: id, MomentRevision: 2, PhotoID: meta.PhotoID, Version: meta.Version}, func(m mp.Metadata, raw []byte) ([]byte, error) {
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != m.SHA256 {
			return nil, mp.ErrChanged
		}
		return raw, nil
	})
	if e != nil || len(encoded) != meta.ByteCount {
		t.Fatal("actual current JPEG content", e)
	}
	httpHandler := coverFrameNativeHandler(f, f.store)
	photoReq := httptest.NewRequest("GET", meta.ThumbnailPath, nil)
	photoHTTP := httptest.NewRecorder()
	httpHandler.ServeHTTP(photoHTTP, photoReq)
	photoHash := sha256.Sum256(photoHTTP.Body.Bytes())
	if photoHTTP.Code != 200 || photoHTTP.Header().Get("Content-Type") != "image/jpeg" || hex.EncodeToString(photoHash[:]) != meta.SHA256 {
		t.Fatal("registered current content route did not return original JPEG", photoHTTP.Code)
	}
	for _, author := range []bool{false, true} {
		if author {
			var agent string
			if e = f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)RETURNING id`, f.actor.ID).Scan(&agent); e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.EnsureAgentProfile(f.ctx, agent, actorref.PrincipalRef{Type: actorref.Person, ID: f.actor.ID}); e != nil {
				t.Fatal(e)
			}
			f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成公开作者','public') ON CONFLICT(account_id) DO UPDATE SET display_name=EXCLUDED.display_name,visibility='public'`, f.actor.ID)
		}
		q := mapMediaNativeQuery()
		q.Media = false
		q.MomentMedia = true
		if author {
			q.AuthorID = f.actor.ID
		}
		mapped, e := f.store.ReadMapLayers(f.ctx, mapdto.Access{}, q)
		if e != nil {
			t.Fatal("actual optin v4", e)
		}
		item := publicPhotoNativeMapItem(t, mapped, id)
		if item.MomentPhoto == nil || item.MomentPhoto.PhotoID != meta.PhotoID || item.MomentPhoto.ThumbnailPath != meta.ThumbnailPath || item.Anchor.PlaceID != place {
			t.Fatal("map photo borrowed ID/anchor or another picture")
		}
		current, e := f.store.FinalizeMapLayers(f.ctx, mapdto.Access{}, mapped)
		if e != nil {
			t.Fatal(e)
		}
		for _, i := range current.Items {
			if i.ID == id && i.Kind == "MOMENT" && (i.MomentPhoto == nil || i.MomentPhoto.ExpiresAt != item.MomentPhoto.ExpiresAt) {
				t.Fatal("map renewed optional lease")
			}
		}
	}
	f.exec(t, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.actor.ID)
	withoutLabel, e := f.store.CapturePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, id, true)
	if e != nil || withoutLabel.View.AuthorID != nil || withoutLabel.View.Photo == nil {
		t.Fatal("author label privacy revoked explicit photo consent", e)
	}
	legacy, e := f.store.GetPublicMoment(f.ctx, content.PublicMomentAccess{}, id)
	if e != nil {
		t.Fatal(e)
	}
	machine, _ := json.Marshal(legacy)
	if strings.Contains(string(machine), "photo") || strings.Contains(string(machine), "contentBase64") {
		t.Fatal("legacy/machine DTO acquired public pixels")
	}
	deleteInput := mp.Input{OperationID: f.id(t), Action: "DELETE", ExpectedMomentRevision: 2, PhotoID: meta.PhotoID, ExpectedPhotoVersion: 1}
	deleteInput, _ = publicPhotoNativeApproved(t, f, id, deleteInput)
	if _, e = f.store.CommitMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, deleteInput); e != nil {
		t.Fatal(e)
	}
	if retired, e := f.store.FinalizePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, view); e != nil || retired.Photo != nil || retired.Title != view.View.Title {
		t.Fatal("image deletion retired authorized text or retained pixels", e)
	}
	if _, e = f.store.ReadMomentPhotoContent(f.ctx, mp.Access{}, mp.Selector{MomentID: id, MomentRevision: 2, PhotoID: meta.PhotoID, Version: 1}, func(_ mp.Metadata, b []byte) ([]byte, error) { return b, nil }); !errors.Is(e, mp.ErrNotFound) {
		t.Fatal("old image version still readable", e)
	}
	var revision int64
	if e = f.pool.QueryRow(f.ctx, `SELECT revision FROM moments WHERE id=$1`, id).Scan(&revision); e != nil || revision != 2 {
		t.Fatal("photo write changed text revision", e)
	}
	// Exercise the real registered public endpoint, not only a raw Store method.
	h := coverFrameNativeHandler(f, f.store)
	request := httptest.NewRequest("GET", "/v1/moments/"+id, nil)
	request.Header.Set("X-Birdtie-Public-Moment-Media", "photo-v1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "public-moment-v3") {
		t.Fatal("actual v3 handler not wired", w.Code)
	}
}

func TestPublicMomentPhotoNativeOriginalFrameSourceAndDeadlineWait(t *testing.T) {
	f := savedPageNative(t)
	for _, kind := range []string{"SessionABA", "principalABA", "ordinaryIdleRenewal", "sourceDeadline"} {
		t.Run(kind, func(t *testing.T) {
			id, place := publicPhotoNativeMoment(t, f, false)
			in := publicPhotoNativeInput(t, f)
			var deadline time.Time
			if kind == "sourceDeadline" {
				if e := f.pool.QueryRow(f.ctx, `UPDATE places SET expires_at=clock_timestamp()+interval '6 seconds' WHERE id=$1 RETURNING expires_at`, place).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			in, _ = publicPhotoNativeApproved(t, f, id, in)
			ctx := mc144Bound(t, f)
			before := publicPhotoNativeCount(t, f)
			query, key := `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session
			if kind == "principalABA" {
				query, key = `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.actor.ID
			}
			if kind == "ordinaryIdleRenewal" {
				query, key = `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, id
			}
			outer, pid := mc144Outer(t, f, query, key)
			done := make(chan error, 1)
			go func() { _, e := f.store.CommitMomentPhoto(ctx, publicPhotoNativeAccess(f), id, in); done <- e }()
			hcNativeWaitPID(t, f, pid)
			switch kind {
			case "SessionABA":
				if _, e := outer.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session); e != nil {
					t.Fatal(e)
				}
				if _, e := outer.Exec(f.ctx, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session); e != nil {
					t.Fatal(e)
				}
			case "principalABA":
				if _, e := outer.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.actor.ID); e != nil {
					t.Fatal(e)
				}
				if _, e := outer.Exec(f.ctx, `UPDATE accounts SET status='active' WHERE id=$1`, f.actor.ID); e != nil {
					t.Fatal(e)
				}
			case "ordinaryIdleRenewal":
				f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '40 minutes' WHERE id=$1`, f.session)
			case "sourceDeadline":
				if wait := time.Until(deadline.Add(80 * time.Millisecond)); wait > 0 {
					time.Sleep(wait)
				}
			}
			if e := outer.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			if kind == "ordinaryIdleRenewal" {
				mc144Result(t, done, nil)
				after := publicPhotoNativeCount(t, f)
				if after.slots != before.slots+1 || after.operations != before.operations+1 || after.audits != before.audits+1 {
					t.Fatal("ordinary idle renew lost exact effect")
				}
			} else {
				mc144Result(t, done, mp.ErrChanged)
				publicPhotoNativeZero(t, f, before)
			}
		})
	}
	t.Run("contentEncodeOriginalSessionABA", func(t *testing.T) {
		id, _ := publicPhotoNativeMoment(t, f, false)
		r, _ := publicPhotoNativeSave(t, f, id, publicPhotoNativeInput(t, f))
		ctx := mc144Bound(t, f)
		var called bool
		data, e := f.store.ReadMomentPhotoContent(ctx, publicPhotoNativeAccess(f), mp.Selector{MomentID: id, MomentRevision: 2, PhotoID: r.PhotoID, Version: 1}, func(_ mp.Metadata, b []byte) ([]byte, error) {
			called = true
			f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
			return b, nil
		})
		if !called || !errors.Is(e, mp.ErrChanged) || len(data) != 0 {
			t.Fatal("original content frame supplied pixels after ABA", e)
		}
	})
	t.Run("actualRightsExpiryAfterEncoding", func(t *testing.T) {
		id, _ := publicPhotoNativeMoment(t, f, false)
		in := publicPhotoNativeInput(t, f)
		in.Rights.RetentionUntil = time.Now().UTC().Add(4 * time.Second)
		r, _ := publicPhotoNativeSave(t, f, id, in)
		view, e := f.store.CapturePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, id, false)
		if e != nil || view.View.Photo == nil {
			t.Fatal(e)
		}
		data, e := f.store.ReadMomentPhotoContent(f.ctx, mp.Access{}, mp.Selector{MomentID: id, MomentRevision: 2, PhotoID: r.PhotoID, Version: 1}, func(_ mp.Metadata, b []byte) ([]byte, error) {
			if wait := time.Until(in.Rights.RetentionUntil.Add(80 * time.Millisecond)); wait > 0 {
				time.Sleep(wait)
			}
			return b, nil
		})
		if !errors.Is(e, mp.ErrChanged) || len(data) != 0 {
			t.Fatal("actual PG expiry afterencoding emitted pixels", e)
		}
		current, e := f.store.FinalizePublicMomentWithPhoto(f.ctx, content.PublicMomentAccess{}, view)
		if e != nil || current.Photo != nil || current.Title != view.View.Title {
			t.Fatal("independent expired photo retired text", e)
		}
	})
}

func publicPhotoNativeOwnedRetention(t *testing.T, f *savedPageNativeFixture) (string, string) {
	t.Helper()
	var database, original string
	if e := f.pool.QueryRow(f.ctx, `SELECT current_database(),coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname=current_database()`).Scan(&database, &original); e != nil {
		t.Fatal(e)
	}
	marker := "birdtie-human-media-owned:public-moment-photo-native"
	comment := func(v string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := f.pool.Exec(ctx, "COMMENT ON DATABASE "+pgx.Identifier{database}.Sanitize()+" IS '"+strings.ReplaceAll(v, "'", "''")+"'"); e != nil {
			t.Fatal(e)
		}
	}
	comment(marker)
	t.Cleanup(func() { comment(original) })
	return database, marker
}
func TestPublicMomentPhotoNativeOwnHistoryStopAndRetention(t *testing.T) {
	f := savedPageNative(t)
	id, place := publicPhotoNativeMoment(t, f, false)
	in := publicPhotoNativeInput(t, f)
	success, _ := publicPhotoNativeSave(t, f, id, in)
	counts := publicPhotoNativeCount(t, f)
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, place)
	original, e := f.store.ReadMomentPhotoOperation(f.ctx, publicPhotoNativeAccess(f), id, in.OperationID)
	if e != nil || original.PayloadDigest != success.PayloadDigest {
		t.Fatal("lost current source hid original fact", e)
	}
	if _, e = f.store.ReadMomentPhotoOperation(f.ctx, publicPhotoNativeAccess(f), f.id(t), in.OperationID); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("operation namespace rebound", e)
	}
	peer, _ := orgHumanNativePerson(t, f)
	if _, e = f.store.ReadMomentPhotoOperation(f.ctx, mp.Access{AccountID: peer.AccountID, AccountType: "person", SessionDigest: peer.SessionDigest}, id, in.OperationID); !errors.Is(e, mp.ErrNotFound) {
		t.Fatal("another actual Person read the owner's tiny history", e)
	}
	state, e := f.store.ReadMomentPhotoState(f.ctx, publicPhotoNativeAccess(f), id)
	if e != nil || state.Photo != nil || state.CanSave || !state.CanDelete {
		t.Fatal("expired source cleanup unavailable", e)
	}
	stopOp := f.id(t)
	stopped, e := f.store.StopMomentPhotoOperation(f.ctx, publicPhotoNativeAccess(f), id, stopOp)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal(e)
	}
	late := in
	late.OperationID = stopOp
	late.SourceVersion = strings.Repeat("a", 64)
	late.Approval = "no-current-source"
	r, e := f.store.CommitMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, late)
	if e != nil || r.Status != "STOPPED" {
		t.Fatal("late stopped producer wrote or required current source", e)
	}
	counts.operations++
	publicPhotoNativeZero(t, f, counts)
	if _, e = f.pool.Exec(f.ctx, `UPDATE public_moment_photo_operations SET status='STOPPED' WHERE actor_account_id=$1 AND operation_id=$2`, f.actor.ID, in.OperationID); e == nil {
		t.Fatal("original fact mutable")
	}
	f.exec(t, `UPDATE public_moment_photos SET version=$2 WHERE moment_id=$1`, id, mp.MaxVersion)
	terminal := mp.Input{OperationID: f.id(t), Action: "DELETE", ExpectedMomentRevision: 2, PhotoID: success.PhotoID, ExpectedPhotoVersion: mp.MaxVersion}
	terminal, _ = publicPhotoNativeApproved(t, f, id, terminal)
	if r, e := f.store.CommitMomentPhoto(f.ctx, publicPhotoNativeAccess(f), id, terminal); e != nil || r.AppliedPhotoVersion != mp.MaxVersion {
		t.Fatal("terminal max cleanup overflowed or required source", e)
	}
	expiredMoment, _ := publicPhotoNativeMoment(t, f, false)
	publicPhotoNativeSave(t, f, expiredMoment, publicPhotoNativeInput(t, f))
	f.exec(t, `UPDATE public_moment_photos SET retention_until=clock_timestamp()-interval '1 second' WHERE moment_id=$1`, expiredMoment)
	db, marker := publicPhotoNativeOwnedRetention(t, f)
	purged, e := f.store.PurgeHumanMediaDerivatives(f.ctx, db, marker, 100)
	if e != nil || !purged.PublicMomentPhotoSupported || purged.PublicMomentPhotos < 1 {
		t.Fatal("actual retired bytes not purged", e)
	}
	var raw []byte
	if e = f.pool.QueryRow(f.ctx, `SELECT thumbnail FROM public_moment_photos WHERE moment_id=$1`, expiredMoment).Scan(&raw); e != nil || raw != nil {
		t.Fatal("logical bytes remain after explicit purge", e)
	}
	if _, e = f.store.ReadMomentPhotoOperation(f.ctx, publicPhotoNativeAccess(f), id, in.OperationID); e != nil {
		t.Fatal("purge removed tiny history", e)
	}
	t.Run("stopFirstActualProducerWait", func(t *testing.T) {
		moment, _ := publicPhotoNativeMoment(t, f, false)
		input, _ := publicPhotoNativeApproved(t, f, moment, publicPhotoNativeInput(t, f))
		ctx := mc144Bound(t, f)
		before := publicPhotoNativeCount(t, f)
		outer, pid := mc144Outer(t, f, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session)
		stopDone := make(chan error, 1)
		writeDone := make(chan error, 1)
		go func() {
			r, e := f.store.StopMomentPhotoOperation(ctx, publicPhotoNativeAccess(f), moment, input.OperationID)
			if e == nil && r.Status != "STOPPED" {
				e = mp.ErrUnavailable
			}
			stopDone <- e
		}()
		stopPID := share143BlockedPID(t, f, pid)
		go func() {
			r, e := f.store.CommitMomentPhoto(ctx, publicPhotoNativeAccess(f), moment, input)
			if e == nil && r.Status != "STOPPED" {
				e = mp.ErrUnavailable
			}
			writeDone <- e
		}()
		hcNativeWaitPID(t, f, stopPID)
		if e := outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		mc144Result(t, stopDone, nil)
		mc144Result(t, writeDone, nil)
		before.operations++
		publicPhotoNativeZero(t, f, before)
	})
	t.Run("actualPre145RetentionCapability", func(t *testing.T) {
		g := savedPageNative(t)
		down, e := os.ReadFile("../../migrations/145_public_moment_photos.down.sql")
		if e != nil {
			t.Fatal(e)
		}
		g.exec(t, string(down))
		db, mark := publicPhotoNativeOwnedRetention(t, g)
		result, e := g.store.PurgeHumanMediaDerivatives(g.ctx, db, mark, 1)
		if e != nil || result.PublicMomentPhotoSupported || result.PublicMomentPhotos != 0 {
			t.Fatal("pre145 owned original maintenance broken", e)
		}
	})
	t.Run("partialCapabilityRejectsBeforeAnyPurge", func(t *testing.T) {
		g := savedPageNative(t)
		db, mark := publicPhotoNativeOwnedRetention(t, g)
		g.exec(t, `DROP FUNCTION birdtie_purge_public_moment_photo_derivatives(integer)`)
		before := publicPhotoNativeCount(t, g)
		if _, e := g.store.PurgeHumanMediaDerivatives(g.ctx, db, mark, 1); !errors.Is(e, ErrMediaRetentionGuard) {
			t.Fatal("partial public capability accepted", e)
		}
		publicPhotoNativeZero(t, g, before)
	})
}
