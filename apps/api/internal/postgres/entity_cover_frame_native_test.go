package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func coverFrameNativeToken(t *testing.T, f *savedPageNativeFixture) string {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET token_sha256=$1 WHERE id=$2`, d[:], f.session)
	f.digest = d
	return token
}
func coverFrameNativeHandler(f *savedPageNativeFixture, access identity.AccessStore) http.Handler {
	return httpapi.New(f.store, access, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, false, nil, f.pool, nil)
}

type coverFrameContentWait struct {
	*Store
	projected, release chan struct{}
}

func (s *coverFrameContentWait) ReadEntityCoverOriginalContent(ctx context.Context, a ec.Access, r ec.Ref, v int64) (ec.OriginalContent, error) {
	out, e := s.Store.ReadEntityCoverOriginalContent(ctx, a, r, v)
	close(s.projected)
	select {
	case <-s.release:
	case <-ctx.Done():
		return out, ec.ErrUnavailable
	}
	return out, e
}
func TestEntityCoverFrameNativeOriginalStateAndContentSessionABA(t *testing.T) {
	f := savedPageNative(t)
	token := coverFrameNativeToken(t, f)
	ref := entityCoverNativeActivity(t, f)
	in, _ := entityCoverNativeWrite(t, f, ref, entityCoverNativeInput(t, f, 0))
	a := entityCoverNativeAccess(f)
	old, e := f.store.ReadEntityCover(f.ctx, a, ref)
	if e != nil || old.Cover == nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = f.store.ValidateEntityCoverProjection(f.ctx, a, ref, old.Cover, old.SourceProof); !errors.Is(e, ec.ErrChanged) {
		t.Fatal("137 SQL original authority failed to retire old State", e)
	}
	frame, e := f.store.CaptureEntityCoverSession(f.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	bound := ec.WithOriginalSession(f.ctx, a, frame)
	fresh, e := f.store.ReadEntityCover(bound, a, ref)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '45 minutes' WHERE id=$1`, f.session)
	if e = f.store.ValidateEntityCoverSession(f.ctx, a, frame); e != nil {
		t.Fatal("ordinary idle renewed frame", e)
	}
	if e = f.store.ValidateEntityCoverProjection(bound, a, ref, fresh.Cover, fresh.SourceProof); e != nil {
		t.Fatal("ordinary idle renewed SQL source", e)
	}
	anon, e := f.store.ReadEntityCover(f.ctx, ec.Access{}, ref)
	if e != nil || anon.Cover == nil {
		t.Fatal("pure anonymous visibility changed", e)
	}
	if e = f.store.ValidateEntityCoverProjection(f.ctx, ec.Access{}, ref, anon.Cover, anon.SourceProof); e != nil {
		t.Fatal(e)
	}
	wrapped := &coverFrameContentWait{Store: f.store, projected: make(chan struct{}), release: make(chan struct{})}
	handler := coverFrameNativeHandler(f, wrapped)
	req := httptest.NewRequest("GET", ec.Path(ref, 1), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); handler.ServeHTTP(w, req); done <- w }()
	select {
	case <-wrapped.projected:
	case <-time.After(4 * time.Second):
		t.Fatal("actual first canonical content not read")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	close(wrapped.release)
	select {
	case w := <-done:
		if w.Code != 409 || w.Header().Get("Content-Type") == "image/jpeg" || strings.Contains(w.Body.String(), "thumbnailPath") {
			t.Fatal("restore borrowed new content authorization", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("content response did not retire")
	}
	in.OperationID = f.id(t)
	in.ExpectedVersion = 1
	_, _ = entityCoverNativeWrite(t, f, ref, in)
	if _, raw, e := f.store.ReadEntityCoverContent(f.ctx, a, ref, 1); e == nil || len(raw) != 0 {
		t.Fatal("retired original version returned pixels", e)
	}
}
func TestEntityCoverFrameNativeLostManagerOwnHistoryAndStoppedLatePut(t *testing.T) {
	f := savedPageNative(t)
	ref := entityCoverNativeOrg(t, f)
	a := entityCoverNativeAccess(f)
	_, r := entityCoverNativeWrite(t, f, ref, entityCoverNativeInput(t, f, 0))
	other, _ := orgHumanNativePerson(t, f)
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, ref.ID, other.AccountID)
	f.exec(t, `UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, ref.ID, a.AccountID)
	f.exec(t, `UPDATE organizations SET visibility='private' WHERE id=$1`, ref.ID)
	got, e := f.store.ReadEntityCoverOperation(f.ctx, a, ref, r.OperationID)
	if e != nil || got != r {
		t.Fatal("lost manager original tiny receipt hidden", e)
	}
	got, e = f.store.StopEntityCoverOperation(f.ctx, a, ref, r.OperationID, 0)
	if e != nil || got != r {
		t.Fatal("stop erased original success", e)
	}
	if _, e = f.store.ReadEntityCoverOperation(f.ctx, ec.Access{AccountID: other.AccountID, AccountType: other.AccountType, SessionDigest: other.SessionDigest}, ref, r.OperationID); !errors.Is(e, ec.ErrNotFound) {
		t.Fatal("cross actor receipt escaped", e)
	}
	bad := ref
	bad.Type = "activity"
	if _, e = f.store.ReadEntityCoverOperation(f.ctx, a, bad, r.OperationID); !errors.Is(e, ec.ErrChanged) {
		t.Fatal("cross ref receipt escaped", e)
	}
	op := f.id(t)
	stop, e := f.store.StopEntityCoverOperation(f.ctx, a, ref, op, 1)
	if e != nil || stop.Status != "STOPPED" || stop.AppliedVersion != 0 {
		t.Fatal("lost manager own zero-effect stop", e)
	}
	f.exec(t, `UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, ref.ID, a.AccountID)
	f.exec(t, `UPDATE organizations SET visibility='public' WHERE id=$1`, ref.ID)
	late := entityCoverNativeInput(t, f, 1)
	late.OperationID = op
	out, e := f.store.ChangeEntityCover(f.ctx, a, ref, late)
	if e != nil || out.Status != "STOPPED" {
		t.Fatal("late original PUT defeated tombstone", e)
	}
	var v, audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT version FROM entity_covers WHERE entity_type=$1 AND entity_id=$2`, ref.Type, ref.ID).Scan(&v); e != nil || v != 1 {
		t.Fatal("stopped PUT changed bytes/version", e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND purpose='entity_cover'`, ref.ID).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("original effect/audit duplicated", e)
	}
	frame, e := f.store.CaptureEntityCoverSession(f.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	bound := ec.WithOriginalSession(f.ctx, a, frame)
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if _, e = f.store.ReadEntityCoverOperation(bound, a, ref, r.OperationID); !errors.Is(e, ec.ErrChanged) {
		t.Fatal("tiny receipt borrowed restored Session", e)
	}
}
func coverFrameNativeBusiness(t *testing.T, f *savedPageNativeFixture) string {
	t.Helper()
	b := &businessConsoleFixture{ctx: f.ctx, pool: f.pool, store: f.store, people: []string{f.actor.ID}, access: []businessconsole.Access{{ActingPersonID: f.actor.ID, SessionDigest: f.digest}}}
	for i := 0; i < 3; i++ {
		a, _ := orgHumanNativePerson(t, f)
		b.people = append(b.people, a.AccountID)
		b.access = append(b.access, businessconsole.Access{ActingPersonID: a.AccountID, SessionDigest: a.SessionDigest})
	}
	id := f.id(t)
	b.claim(t, id)
	b.grant(t, id)
	review := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成隔离封面来源审核"}
	if _, e := b.store.ReviewBusinessClaim(f.ctx, b.who(1, id), review); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.PutBusinessProfile(f.ctx, b.who(0, id), businessProfileNativeInput(time.Now().UTC())); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.ReviewBusinessProfile(f.ctx, b.who(1, id), review); e != nil {
		t.Fatal(e)
	}
	view, e := b.store.ReadBusinessPublicPermission(f.ctx, b.who(0, id))
	if e != nil || view.Preview == nil {
		t.Fatal(e)
	}
	if _, e = b.store.ChangeBusinessPublicPermission(f.ctx, b.who(0, id), supplierprofile.PermissionInput{ExpectedProfileVersion: view.Preview.ProfileVersion, Action: "publish", ValidUntil: view.Preview.ValidUntil.Format(time.RFC3339Nano), SourceSnapshot: view.Preview.SourceSnapshot}); e != nil {
		t.Fatal(e)
	}
	activity, e := f.store.CreateSocialDraft(f.ctx, f.actor.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: id}, CityID: "aberdeen-gb", Title: "合成商家封面活动", Summary: "隔离测试", Modality: "online", PhysicalPlaceStatus: "not_applicable", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.PublishSocialActivity(f.ctx, f.actor.ID, activity.ID); e != nil {
		t.Fatal(e)
	}
	entityCoverNativeWrite(t, f, ec.Ref{Type: "activity", ID: activity.ID}, entityCoverNativeInput(t, f, 0))
	return id
}
func TestEntityCoverFrameNativeRegisteredCatalogSQLWait(t *testing.T) {
	for _, kind := range []string{"areaPulse", "businessUpcoming"} {
		t.Run(kind, func(t *testing.T) {
			f := savedPageNative(t)
			token := coverFrameNativeToken(t, f)
			url := "/v1/cities/aberdeen-gb/pulse?bounds=-3,57,-1,58"
			if kind == "businessUpcoming" {
				id := coverFrameNativeBusiness(t, f)
				url = "/v1/businesses/" + id
			} else {
				ref := entityCoverNativeActivity(t, f)
				entityCoverNativeWrite(t, f, ref, entityCoverNativeInput(t, f, 0))
			}
			handler := coverFrameNativeHandler(f, f.store)
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			var blockerPID int
			if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); e != nil {
				t.Fatal(e)
			}
			if _, e = lock.Exec(f.ctx, `LOCK TABLE activities IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			req := httptest.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); handler.ServeHTTP(w, req); done <- w }()
			wait := false
			end := time.Now().Add(4 * time.Second)
			for time.Now().Before(end) {
				var n int
				// The full business projection exceeds track_activity_query_size:
				// its activities relation can lie beyond the visible query prefix.
				// Observe this exact relation and blocker, not truncated SQL text.
				if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity waiter JOIN pg_locks waiting ON waiting.pid=waiter.pid
 WHERE waiter.datname=current_database() AND waiter.wait_event_type='Lock'
 AND waiting.locktype='relation' AND waiting.relation='activities'::regclass AND NOT waiting.granted
 AND $1=ANY(pg_blocking_pids(waiter.pid))`, blockerPID).Scan(&n); e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					wait = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !wait {
				t.Fatal("actual catalog SQL wait absent")
			}
			f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 409 || strings.Contains(w.Body.String(), "thumbnailPath") {
					t.Fatal("catalog SQL wait borrowed restored authorization", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("catalog response did not retire")
			}
		})
	}
}
