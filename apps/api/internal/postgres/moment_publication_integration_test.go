package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"strings"
	"sync"
	"testing"
	"time"
)

type momentPublicationNativeActor struct {
	actor  identity.Actor
	digest [32]byte
}

func momentPublicationOrdinary(t *testing.T, f *placeMemoryFixture) momentPublicationNativeActor {
	t.Helper()
	b := f.private.base
	id := agentMemoryID(t, f.private)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM account_blocks WHERE blocker_account_id=$1 OR blocked_account_id=$1`, `DELETE FROM audit_events WHERE actor_account_id=$1`, `DELETE FROM moments WHERE author_account_id=$1`, `DELETE FROM sessions WHERE account_id=$1`, `DELETE FROM accounts WHERE id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, id); e != nil {
				t.Error("ordinary publication cleanup", e)
			}
		}
	})
	b.exec(`INSERT INTO accounts(id,account_type,handle,status) VALUES($1,'person',$2,'active')`, id, "moment-public-"+strings.ReplaceAll(id, "-", ""))
	b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, id, digest[:])
	return momentPublicationNativeActor{identity.Actor{ID: id, AccountType: "person"}, digest}
}
func momentPublicationDraft(t *testing.T, f *placeMemoryFixture, a momentPublicationNativeActor) content.Moment {
	t.Helper()
	b := f.private.base
	old := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	m, e := b.store.CreateHumanMomentDraft(b.ctx, a.digest, a.actor, content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "本人明确公开的合成记录", Body: strings.Repeat("公开节选", 90), OccurredAt: &old, TimePrecision: "day", LocationPrecision: "place"})
	if e != nil {
		t.Fatal("actual human draft", e)
	}
	return m
}
func momentPublicationPublish(t *testing.T, f *placeMemoryFixture, a momentPublicationNativeActor, m content.Moment) content.MomentPublicationReceipt {
	t.Helper()
	b := f.private.base
	p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
	if e != nil {
		t.Fatal("actual preview", e)
	}
	r, e := b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
	if e != nil {
		t.Fatal("actual publication", e)
	}
	return r
}
func TestMomentPublicationNativeOrdinaryLifecycle(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	m := momentPublicationDraft(t, f, a)
	var agentCount int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, a.actor.ID).Scan(&agentCount); e != nil || agentCount != 0 {
		t.Fatal("ordinary person must have no Agent", e)
	}
	empty, e := b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || empty.RecentMomentCount != 0 {
		t.Fatal("private draft exposed", e)
	}
	before := momentPublicationEffects(t, f, a.actor.ID)
	p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
	if e != nil || content.ValidateMomentPublicationPreview(p, m.ID) != nil {
		t.Fatal("actual public preview", e)
	}
	if before != momentPublicationEffects(t, f, a.actor.ID) {
		t.Fatal("preview wrote source/audit")
	}
	r, e := b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
	if e != nil || r.Revision != m.Revision+1 || r.Status != "published" {
		t.Fatal("actual publication", e)
	}
	got, e := New(b.pool, false).GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || got.RecentMomentCount != 1 || len(got.RecentMoments) != 1 || got.RecentMoments[0].Revision != r.Revision || len([]rune(got.RecentMoments[0].Excerpt)) != 280 {
		t.Fatal("actual public persisted recent excerpt", got, e)
	}
	if got.RecentMoments[0].PublishedAt.Year() == 1999 {
		t.Fatal("self claimed historical occurrence became publication/visit")
	}
	raw, _ := json.Marshal(got)
	for _, s := range []string{a.actor.ID, "occurredAt", "authorAccountId", "latitude", "longitude", "activityIds", "agent", "token_sha256"} {
		if strings.Contains(string(raw), s) {
			t.Fatal("private source exported", s)
		}
	}
	if _, e = b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true}); !errors.Is(e, content.ErrConflict) {
		t.Fatal("repeat old concrete approval", e)
	}
	if e = b.store.WithdrawHumanMoment(b.ctx, a.digest, a.actor, m.ID, r.Revision); e != nil {
		t.Fatal("native withdrawal", e)
	}
	got, e = b.store.GetPublicPlaceSocialHistory(b.ctx, ph.Access{}, f.place)
	if e != nil || got.RecentMomentCount != 0 {
		t.Fatal("withdrawn remained public", e)
	}
}
func momentPublicationEffects(t *testing.T, f *placeMemoryFixture, id string) string {
	t.Helper()
	var raw string
	b := f.private.base
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('m',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]'::jsonb) FROM moments m WHERE author_account_id=$1),'a',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE actor_account_id=$1))::text`, id).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestMomentPublicationNativeCurrentSnapshotRejects(t *testing.T) {
	for _, mode := range []string{"wrong_person", "no_confirm", "wrong_revision", "forged", "source_changed", "account_restored"} {
		t.Run(mode, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationOrdinary(t, f)
			m := momentPublicationDraft(t, f, a)
			p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
			if e != nil {
				t.Fatal(e)
			}
			in := content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true}
			use := a
			switch mode {
			case "wrong_person":
				use = momentPublicationOrdinary(t, f)
			case "no_confirm":
				in.ConfirmPublic = false
			case "wrong_revision":
				in.Revision++
			case "forged":
				in.Snapshot = in.Snapshot[:len(in.Snapshot)-1] + "z"
			case "source_changed":
				_, e = b.store.UpdateHumanMomentDraft(b.ctx, a.digest, a.actor, m.ID, m.Revision, content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "换过的具体内容", Body: "没有旧版本批准", TimePrecision: "unknown", LocationPrecision: "place"})
				if e != nil {
					t.Fatal(e)
				}
			case "account_restored":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, a.actor.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, a.actor.ID)
			}
			before := momentPublicationEffects(t, f, a.actor.ID)
			out, e := b.store.PublishHumanMoment(b.ctx, use.digest, use.actor, m.ID, in)
			if e == nil || out.MomentID != "" || before != momentPublicationEffects(t, f, a.actor.ID) {
				t.Fatal("native rejected approval leaked/wrote", e, out)
			}
		})
	}
}
func TestMomentPublicationNativeConcurrentCAS(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	m := momentPublicationDraft(t, f, a)
	p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, content.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
}
func momentPublicationWait(t *testing.T, f *placeMemoryFixture, pid int, query string) {
	t.Helper()
	b := f.private.base
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2)`, pid, "%"+query+"%").Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual native source wait not observed")
}
func TestMomentPublicationNativeLateSession(t *testing.T) {
	for _, mode := range []string{"preview_expiry", "preview_revoke", "publish_expiry", "publish_revoke"} {
		t.Run(mode, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationOrdinary(t, f)
			m := momentPublicationDraft(t, f, a)
			p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
			if e != nil {
				t.Fatal(e)
			}
			if strings.HasSuffix(mode, "expiry") {
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, a.digest[:])
			}
			before := momentPublicationEffects(t, f, a.actor.ID)
			blocker, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if e = blocker.QueryRow(b.ctx, `SELECT pg_backend_pid() FROM moments WHERE id=$1 FOR UPDATE`, m.ID).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				if strings.HasPrefix(mode, "preview") {
					_, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
					done <- e
				} else {
					_, e := b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
					done <- e
				}
			}()
			momentPublicationWait(t, f, pid, "FROM moments")
			if strings.HasSuffix(mode, "revoke") {
				if e = b.store.RevokeSession(b.ctx, a.digest); e != nil {
					t.Fatal(e)
				}
			} else {
				deadline := time.Now().Add(4 * time.Second)
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, a.digest[:]).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("real expiry not reached")
					}
					time.Sleep(15 * time.Millisecond)
				}
			}
			if e = blocker.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, identity.ErrUnauthorized) || before != momentPublicationEffects(t, f, a.actor.ID) {
					t.Fatal("late current session accepted/wrote", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native request not released")
			}
		})
	}
}
func TestMomentPublicationNativeFinalClockRollsBackWrites(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	m := momentPublicationDraft(t, f, a)
	p, e := b.store.PreviewHumanMomentPublication(b.ctx, a.digest, a.actor, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	// This owned test-only trigger establishes a late native clock boundary
	// after the actual source UPDATE and before transaction completion.
	fn := "mom002_audit_wait_" + strings.ReplaceAll(a.actor.ID, "-", "")
	t.Cleanup(func() {
		for _, q := range []string{"DROP TRIGGER IF EXISTS " + fn + " ON audit_events", "DROP FUNCTION IF EXISTS " + fn + "()"} {
			if _, e := b.pool.Exec(context.Background(), q); e != nil {
				t.Error("owned late clock trigger cleanup", e)
			}
		}
	})
	b.exec(`CREATE FUNCTION ` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_account_id='` + a.actor.ID + `'::uuid AND NEW.action='publish_public' THEN PERFORM pg_sleep(1); END IF; RETURN NEW; END $$`)
	b.exec(`CREATE TRIGGER ` + fn + ` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION ` + fn + `()`)
	b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '500 milliseconds',idle_expires_at=clock_timestamp()+interval '450 milliseconds' WHERE token_sha256=$1`, a.digest[:])
	before := momentPublicationEffects(t, f, a.actor.ID)
	out, e := b.store.PublishHumanMoment(b.ctx, a.digest, a.actor, m.ID, content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
	if !errors.Is(e, identity.ErrUnauthorized) || out.MomentID != "" || before != momentPublicationEffects(t, f, a.actor.ID) {
		t.Fatal("final native clock reused early allowance or leaked writes", e, out)
	}
}
