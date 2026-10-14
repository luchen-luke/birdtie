package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
)

func photoNativeAccess(f *savedPageNativeFixture) pp.Access {
	return pp.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
}
func photoNativeReviewer(t *testing.T, f *savedPageNativeFixture) pp.Access {
	t.Helper()
	id := f.id(t)
	_, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, id)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.id(t), id, d[:])
	f.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role)VALUES('aberdeen-gb',$1,'reviewer')`, id)
	return pp.Access{AccountID: id, AccountType: "person", SessionDigest: d}
}
func photoNativeSetup(t *testing.T) (*savedPageNativeFixture, string, pp.Access) {
	t.Helper()
	f := savedPageNative(t)
	_, id := f.bookmark(t)
	f.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role)VALUES('aberdeen-gb',$1,'contributor')`, f.actor.ID)
	return f, id, photoNativeReviewer(t, f)
}
func photoNativeInput(t *testing.T, f *savedPageNativeFixture) pp.Input {
	t.Helper()
	in := entityCoverNativeInput(t, f, 0)
	return pp.Input{OperationID: in.OperationID, Action: "SUBMIT", ContentType: in.ContentType, Content: in.Content, Rights: in.Rights}
}
func photoNativeWrite(t *testing.T, f *savedPageNativeFixture, a pp.Access, id string, in pp.Input) (pp.Input, pp.Receipt) {
	t.Helper()
	preview, e := f.store.PreviewPlacePhoto(f.ctx, a, id, in)
	if e != nil {
		t.Fatal("actual specific photo preview", e)
	}
	if in.Action != "WITHDRAW" && (preview.PreviewDerivative == nil || preview.PreviewDerivative.Width != 320 || preview.PreviewDerivative.Height != 180) {
		t.Fatal("actual wide preview missing")
	}
	in.Approval = preview.Token
	r, e := f.store.ChangePlacePhoto(f.ctx, a, id, in)
	if e != nil {
		t.Fatal("actual photo write", e)
	}
	return in, r
}
func photoNativePublish(t *testing.T, f *savedPageNativeFixture, reviewer pp.Access, id string) (pp.Input, pp.Receipt) {
	t.Helper()
	_, candidate := photoNativeWrite(t, f, photoNativeAccess(f), id, photoNativeInput(t, f))
	return photoNativeWrite(t, f, reviewer, id, pp.Input{OperationID: f.id(t), Action: "APPROVE", CandidateID: candidate.CandidateID, CandidateVersion: 1, RightsConfirmed: true, PhotoConfirmed: true})
}
func TestBatchB2PlacePhotoIndependentReviewReceiptsAndHumanProjection(t *testing.T) {
	f, id, reviewer := photoNativeSetup(t)
	a := photoNativeAccess(f)
	var placeBefore string
	if e := f.pool.QueryRow(f.ctx, `SELECT xmin::text FROM places WHERE id=$1`, id).Scan(&placeBefore); e != nil {
		t.Fatal(e)
	}
	in, candidate := photoNativeWrite(t, f, a, id, photoNativeInput(t, f))
	if candidate.CandidateID != in.OperationID || candidate.CandidateVersion != 1 || candidate.AppliedVersion != 0 {
		t.Fatal("draft candidate masqueraded as publication")
	}
	if r, e := f.store.ChangePlacePhoto(f.ctx, a, id, in); e != nil || r != candidate {
		t.Fatal("original SUBMIT replay duplicated", e)
	}
	state, e := f.store.ReadPlacePhoto(f.ctx, pp.Access{}, id)
	if e != nil || state.PlacePhoto != nil || state.Version != 0 {
		t.Fatal("unreviewed image public", e)
	}
	self := pp.Input{OperationID: f.id(t), Action: "APPROVE", CandidateID: candidate.CandidateID, CandidateVersion: 1, RightsConfirmed: true, PhotoConfirmed: true}
	if _, e = f.store.PreviewPlacePhoto(f.ctx, a, id, self); !errors.Is(e, pp.ErrForbidden) {
		t.Fatal("contributor self approval", e)
	}
	f.exec(t, `UPDATE city_editor_memberships SET role='reviewer' WHERE city_id='aberdeen-gb' AND account_id=$1`, a.AccountID)
	if _, e = f.store.PreviewPlacePhoto(f.ctx, a, id, self); !errors.Is(e, pp.ErrForbidden) {
		t.Fatal("same person became independent reviewer", e)
	}
	_, other := f.bookmark(t)
	if _, e = f.store.PreviewPlacePhoto(f.ctx, reviewer, other, self); !errors.Is(e, pp.ErrNotFound) {
		t.Fatal("candidate borrowed another Place", e)
	}
	f.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,content_status,publication_status,source_label,source_ref,maintainer_label)SELECT 'photo-other-city','隔离另一城市',region,country_code,time_zone,content_status,'published',source_label,source_ref,maintainer_label FROM cities WHERE id='aberdeen-gb'`)
	f.exec(t, `UPDATE city_editor_memberships SET city_id='photo-other-city' WHERE city_id='aberdeen-gb' AND account_id=$1`, reviewer.AccountID)
	if _, e = f.store.PreviewPlacePhoto(f.ctx, reviewer, id, self); !errors.Is(e, pp.ErrForbidden) {
		t.Fatal("another city reviewer approved Place photo", e)
	}
	f.exec(t, `UPDATE city_editor_memberships SET city_id='aberdeen-gb' WHERE city_id='photo-other-city' AND account_id=$1`, reviewer.AccountID)
	approval, r := photoNativeWrite(t, f, reviewer, id, self)
	if r.AppliedVersion != 1 || r.CandidateVersion != 2 {
		t.Fatal("canonical photo CAS")
	}
	if replay, e := f.store.ChangePlacePhoto(f.ctx, reviewer, id, approval); e != nil || replay != r {
		t.Fatal("review replay changed effect", e)
	}
	state, e = f.store.ReadPlacePhoto(f.ctx, pp.Access{}, id)
	if e != nil || state.PlacePhoto == nil || state.PlacePhoto.EntityID != id {
		t.Fatal("approved current photo", e)
	}
	m, raw, e := f.store.ReadPlacePhotoContent(f.ctx, pp.Access{}, id, 1)
	if e != nil || len(raw) != m.ByteCount {
		t.Fatal("actual retained JPEG", e)
	}
	detail, e := f.store.GetPlace(f.ctx, id)
	if e != nil || detail.PlacePhoto == nil || detail.PhotoSourceProof == "" {
		t.Fatal("canonical detail projection", e)
	}
	page, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, saved.Query{Kind: "place"}, "")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, i := range page.Items {
		if i.TargetID == id {
			found = i.PlacePhoto != nil
		}
	}
	if !found {
		t.Fatal("original Saved photo missing")
	}
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, saved.Query{Kind: "place"}, "", page); e != nil {
		t.Fatal("fixed original metadata lease", e)
	}
	legacy, e := f.store.ListSaved(f.ctx, a.AccountID)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range legacy {
		if i.PlacePhoto != nil {
			t.Fatal("image entered old Agent Saved")
		}
	}
	unknown := f.id(t)
	if _, e = f.store.ReadPlacePhotoOperation(f.ctx, a, id, unknown); !errors.Is(e, pp.ErrNotFound) {
		t.Fatal("404 substituted for no effect", e)
	}
	stop, e := f.store.StopPlacePhotoOperation(f.ctx, a, id, unknown, 1)
	if e != nil || stop.Status != "STOPPED" {
		t.Fatal("original stop", e)
	}
	late := photoNativeInput(t, f)
	late.OperationID = unknown
	late.ExpectedVersion = 1
	if result, e := f.store.ChangePlacePhoto(f.ctx, a, id, late); e != nil || result.Status != "STOPPED" {
		t.Fatal("late POST passed original tombstone", e)
	}
	var absent int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM place_photo_candidates WHERE id=$1`, unknown).Scan(&absent); e != nil || absent != 0 {
		t.Fatal("stopped proposal retained pixels", e)
	}
	if result, e := f.store.StopPlacePhotoOperation(f.ctx, a, id, in.OperationID, 0); e != nil || result != candidate {
		t.Fatal("stop rewrote already-successful history", e)
	}
	f.exec(t, `UPDATE city_editor_memberships SET state='revoked' WHERE city_id='aberdeen-gb' AND account_id=$1`, a.AccountID)
	_, withdrawn := photoNativeWrite(t, f, a, id, pp.Input{OperationID: f.id(t), ExpectedVersion: 1, Action: "WITHDRAW", CandidateID: in.OperationID, CandidateVersion: 2})
	if withdrawn.AppliedVersion != 2 {
		t.Fatal("own withdrawal after editor revoke")
	}
	var erased bool
	if e = f.pool.QueryRow(f.ctx, `SELECT thumbnail IS NULL FROM place_photo_candidates WHERE id=$1`, in.OperationID).Scan(&erased); e != nil || !erased {
		t.Fatal("withdrawal did not physically clear derivative", e)
	}
	if _, _, e = f.store.ReadPlacePhotoContent(f.ctx, pp.Access{}, id, 1); !errors.Is(e, pp.ErrNotFound) {
		t.Fatal("withdrawn content revived", e)
	}
	old, e := f.store.ReadPlacePhotoOperation(f.ctx, a, id, in.OperationID)
	if e != nil || old.Action != "SUBMIT" || old.CandidateVersion != 1 {
		t.Fatal("history lost after withdrawal", e)
	}
	var placeAfter string
	f.pool.QueryRow(f.ctx, `SELECT xmin::text FROM places WHERE id=$1`, id).Scan(&placeAfter)
	if placeBefore != placeAfter {
		t.Fatal("photo changed canonical facts")
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "138_place_photos.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(f.ctx, string(down)); e == nil {
		t.Fatal("photo operation history downgraded")
	}
	_, _ = conn.Exec(context.Background(), "ROLLBACK")
}
func TestBatchB2PlacePhotoSourceSessionLeaseAndActualMachineProjection(t *testing.T) {
	f, id, reviewer := photoNativeSetup(t)
	photoNativePublish(t, f, reviewer, id)
	a := photoNativeAccess(f)
	state, e := f.store.ReadPlacePhoto(f.ctx, a, id)
	if e != nil {
		t.Fatal(e)
	}
	editorial, e := f.store.ReadPlacePhotoEditorial(f.ctx, a, id)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = f.store.ValidatePlacePhotoProjection(f.ctx, a, id, state.PlacePhoto, state.SourceProof); !errors.Is(e, pp.ErrChanged) {
		t.Fatal("old state Session generation revived", e)
	}
	if e = f.store.ValidatePlacePhotoEditorial(f.ctx, a, id, editorial); !errors.Is(e, pp.ErrChanged) {
		t.Fatal("old editorial Session generation revived", e)
	}
	state, e = f.store.ReadPlacePhoto(f.ctx, a, id)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE place_photo_publications SET version=version+1 WHERE place_id=$1`, id)
	if e = f.store.ValidatePlacePhotoProjection(f.ctx, a, id, state.PlacePhoto, state.SourceProof); !errors.Is(e, pp.ErrChanged) {
		t.Fatal("publication source ABA revived old version", e)
	}
	f.exec(t, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1)`, a.AccountID)
	f.exec(t, `INSERT INTO city_contexts(city_id)VALUES('aberdeen-gb')ON CONFLICT(city_id) DO NOTHING`)
	task, e := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: a.AccountID, ActingUserID: a.AccountID, CityID: "aberdeen-gb", Query: "合成分页收藏", Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": "合成分页收藏", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "合成分页收藏"}}})
	if e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	access := arp.Access{Actor: f.actor, SessionDigest: f.digest, TaskID: task.ID, ExpectedTask: expected}
	query := arp.Query{CityID: task.CityID, Kind: "place", TimePreference: "anytime", CompareIDs: []string{}}
	r, e := f.store.ReadAgentResultProjection(f.ctx, access, query)
	if e != nil {
		t.Fatal("actual typed human result", e)
	}
	found := 0
	for _, i := range r.Items {
		if i.Entity == (arp.Ref{Type: "place", ID: id}) {
			found++
			if i.PlacePhoto == nil || i.PlacePhoto.EntityID != id {
				t.Fatal("canonical typed photo")
			}
		}
	}
	if found != 1 {
		t.Fatal("canonical place missing/duplicated among actual sources", found)
	}
	if e = f.store.RevalidateAgentResultProjection(f.ctx, access, query, r); e != nil {
		t.Fatal("fresh clock rejected fixed result lease", e)
	}
	fresh, e := f.store.ReadAgentResultProjection(f.ctx, access, query)
	if e != nil || !unchangedOutputProjection(r, fresh) {
		t.Fatal("shared result comparator renewed photo", e)
	}
	private := agentprofile.PrivateAccess{SessionDigest: f.digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: a.AccountID}}
	agent, e := f.store.ResolveOwnContextAgent(f.ctx, private)
	if e != nil {
		t.Fatal(e)
	}
	request := acb.Request{Access: private, Agent: agent, TaskID: task.ID, TaskUpdatedAt: task.UpdatedAt, RequestID: "LOCAL_PHOTO_NATIVE_CONTEXT", CityID: task.CityID, CurrentQuery: task.Filters["currentQuery"], Selection: acb.PlaceSearch, Mode: acb.RulesPublicQuery, PlaceIDs: []string{id}, DeadlineAt: time.Now().UTC().Add(5 * time.Minute)}
	built, e := f.store.BuildOwnAgentContext(f.ctx, request)
	if e != nil || len(built.Places) != 1 || built.Places[0].PlacePhoto != nil || built.Places[0].PhotoSourceProof != "" {
		t.Fatal("human image purpose entered actual machine context", e)
	}
	wire, _ := json.Marshal(built.Bundle)
	if strings.Contains(string(wire), "placePhoto") || strings.Contains(string(wire), "thumbnailPath") || built.Bundle.ModelAccess != "UNAVAILABLE" {
		t.Fatal("new image/model grant")
	}
	f.exec(t, `UPDATE city_editor_memberships SET state='revoked' WHERE city_id='aberdeen-gb' AND account_id=$1`, reviewer.AccountID)
	f.exec(t, `UPDATE city_editor_memberships SET state='active' WHERE city_id='aberdeen-gb' AND account_id=$1`, reviewer.AccountID)
	if e = f.store.RevalidateAgentResultProjection(f.ctx, access, query, r); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("editor membership ABA revived photo source", e)
	}
}
func TestBatchB2PlacePhotoWaitSessionFenceAndPhysicalExpiryPurge(t *testing.T) {
	f, id, _ := photoNativeSetup(t)
	a := photoNativeAccess(f)
	in := photoNativeInput(t, f)
	preview, e := f.store.PreviewPlacePhoto(f.ctx, a, id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = preview.Token
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, `SELECT id FROM places WHERE id=$1 FOR UPDATE`, id); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, err := f.store.ChangePlacePhoto(f.ctx, a, id, in); done <- err }()
	waited := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM places WHERE id=$1 FOR UPDATE%'`).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("actual photo resource wait absent")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, pp.ErrChanged) {
			t.Fatal("late Session ABA retained photo", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("photo writer did not retire")
	}
	var n int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM place_photo_candidates WHERE id=$1`, in.OperationID).Scan(&n)
	if n != 0 {
		t.Fatal("retired writer left JPEG")
	}
	expiring := photoNativeInput(t, f)
	expiring.Rights.RetentionUntil = time.Now().UTC().Add(1200 * time.Millisecond)
	_, r := photoNativeWrite(t, f, a, id, expiring)
	cover := entityCoverNativeActivity(t, f)
	_, coverReceipt := entityCoverNativeWrite(t, f, cover, entityCoverNativeInput(t, f, 0))
	f.exec(t, `UPDATE entity_covers SET retention_until=clock_timestamp()-interval '1 second' WHERE entity_type=$1 AND entity_id=$2`, cover.Type, cover.ID)
	database := f.pool.Config().ConnConfig.Database
	marker := "birdtie-human-media-owned:" + f.id(t)
	f.exec(t, `COMMENT ON DATABASE `+pgx.Identifier{database}.Sanitize()+` IS `+"'"+marker+"'")
	if _, e = f.store.PurgeHumanMediaDerivatives(f.ctx, database, marker+"wrong", 100); !errors.Is(e, ErrMediaRetentionGuard) {
		t.Fatal("wrong ownership marker reached purge", e)
	}
	sleep := time.Until(expiring.Rights.RetentionUntil) + 20*time.Millisecond
	if sleep > 0 {
		time.Sleep(sleep)
	}
	result, e := f.store.PurgeHumanMediaDerivatives(f.ctx, database, marker, 100)
	if e != nil || result.PlacePhotos != 1 || result.EntityCovers != 1 {
		t.Fatal("actual retained byte cleanup", result, e)
	}
	var erased bool
	if e = f.pool.QueryRow(f.ctx, `SELECT thumbnail IS NULL FROM place_photo_candidates WHERE id=$1`, r.CandidateID).Scan(&erased); e != nil || !erased {
		t.Fatal("expired pixels still stored", e)
	}
	var coverErased bool
	if e = f.pool.QueryRow(f.ctx, `SELECT thumbnail IS NULL AND rights IS NULL FROM entity_covers WHERE entity_type=$1 AND entity_id=$2`, cover.Type, cover.ID).Scan(&coverErased); e != nil || !coverErased {
		t.Fatal("137 expired derivative still stored", e)
	}
	original, e := f.store.ReadPlacePhotoOperation(f.ctx, a, id, r.OperationID)
	if e != nil || original.CandidateVersion != 1 || original.Action != "SUBMIT" {
		t.Fatal("expiry erased operation receipt", e)
	}
	history, e := f.store.ReadEntityCoverOperation(f.ctx, entityCoverNativeAccess(f), cover, coverReceipt.OperationID)
	if e != nil || history != coverReceipt {
		t.Fatal("cover purge erased original history", e)
	}
	repeat, e := f.store.PurgeHumanMediaDerivatives(f.ctx, database, marker, 100)
	if e != nil || repeat.PlacePhotos != 0 || repeat.EntityCovers != 0 {
		t.Fatal("purge replay changed history", repeat, e)
	}
	t.Run("sourceClockExpiresDuringActualLedgerWait", func(t *testing.T) {
		f.exec(t, `UPDATE places SET expires_at=clock_timestamp()+interval '900 milliseconds' WHERE id=$1`, id)
		fresh := photoNativeInput(t, f)
		preview, e := f.store.PreviewPlacePhoto(f.ctx, a, id, fresh)
		if e != nil {
			t.Fatal(e)
		}
		fresh.Approval = preview.Token
		lock, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(context.Background())
		if _, e = lock.Exec(f.ctx, `LOCK TABLE place_photo_operations IN SHARE MODE`); e != nil {
			t.Fatal(e)
		}
		finished := make(chan error, 1)
		go func() { _, err := f.store.ChangePlacePhoto(f.ctx, a, id, fresh); finished <- err }()
		actualWait := false
		end := time.Now().Add(4 * time.Second)
		for time.Now().Before(end) {
			var count int
			e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO place_photo_operations%'`).Scan(&count)
			if e != nil {
				t.Fatal(e)
			}
			if count > 0 {
				actualWait = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !actualWait {
			t.Fatal("source-expiry test did not reach ledger wait")
		}
		sleep := time.Until(preview.ExpiresAt) + 20*time.Millisecond
		if sleep > 0 {
			time.Sleep(sleep)
		}
		if e = lock.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-finished:
			if !errors.Is(e, pp.ErrChanged) {
				t.Fatal("expired source committed photo", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("source-expiry writer did not finish")
		}
		var rows int
		e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM place_photo_candidates WHERE id=$1`, fresh.OperationID).Scan(&rows)
		if e != nil || rows != 0 {
			t.Fatal("expired source left retained pixels", e)
		}
	})
}
