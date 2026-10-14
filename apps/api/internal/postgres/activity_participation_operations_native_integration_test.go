package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rsvpPGFixture struct {
	t                 *testing.T
	ctx               context.Context
	pool              *pgxpool.Pool
	store             *Store
	a                 ea.Access
	host, city, place string
}

func rsvpPG(t *testing.T) *rsvpPGFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &rsvpPGFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false)}
	for i := 0; i < 2; i++ {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'独占本地人类报名','合成非真实用户','public')`, id)
		f.exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, id)
		if i == 0 {
			_, digest, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',statement_timestamp()+interval '1 hour',statement_timestamp()+interval '1 hour')`, id, digest[:])
			f.a = ea.Access{Actor: identity.Actor{ID: id, AccountType: "person"}, SessionDigest: digest}
		} else {
			f.host = id
		}
	}
	f.city = "rsvp117-" + f.uuid()
	f.place = f.uuid()
	f.exec(`INSERT INTO cities(id,name,country_code,region,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'独占本地恢复城市','GB','LOCAL_SYNTHETIC','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:AIR038','合成维护者',$2)`, f.city, f.host)
	f.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, f.city)
	f.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'独占合成报名地点','sports',57.15,-2.1,'point','published','LOCAL_SYNTHETIC_FIXTURE','local:AIR038','合成维护者',$3)`, f.place, f.city, f.host)
	return f
}
func (f *rsvpPGFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		f.t.Fatal("owned RSVP fixture SQL", e)
	}
}
func (f *rsvpPGFixture) uuid() string {
	f.t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		f.t.Fatal(e)
	}
	return id
}
func (f *rsvpPGFixture) activity() string {
	f.t.Helper()
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		f.t.Fatal(e)
	}
	v, e := f.store.CreateSocialDraft(f.ctx, f.host, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.host}, CityID: f.city, PlaceID: f.place, Title: "独占合成人类 RSVP 恢复", Summary: "本地合成", Description: "NO_PRIVATE_ACTIVITY_BODY_IN_RECEIPT", CategoryCode: "badminton", Visibility: "public", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		f.t.Fatal(e)
	}
	if _, e = f.store.PublishSocialActivity(f.ctx, f.host, v.ID); e != nil {
		f.t.Fatal(e)
	}
	return v.ID
}
func (f *rsvpPGFixture) bound(id, op string) ea.BoundCondition {
	f.t.Helper()
	r, e := f.store.ReadEntityActions(f.ctx, f.a, ea.Ref{Type: "activity", ID: id})
	if e != nil {
		f.t.Fatal(e)
	}
	return ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Join, Operation: op}
}
func (f *rsvpPGFixture) counts(id string) (p, a, r int) {
	f.t.Helper()
	if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2),(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='human_rsvp' AND target_resource_id=$1),(SELECT count(*) FROM human_participation_operation_receipts WHERE activity_id=$1 AND owner_account_id=$2)`, id, f.a.Actor.ID).Scan(&p, &a, &r); e != nil {
		f.t.Fatal(e)
	}
	return
}
func rsvpPGSameReceipt(t *testing.T, a, b ap.OperationReceipt) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("immutable original history changed", a, b)
	}
}

func TestHumanParticipationOperationNativeCausalHistoryAndFences(t *testing.T) {
	f := rsvpPG(t)
	t.Run("original_join_replay_later_cancel_and_single_connection_read", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		in := ap.OperationInput{OperationID: op, Operation: "JOIN"}
		r, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, in, b)
		if e != nil || !fresh || r.Status != "COMMITTED" {
			t.Fatal(r, fresh, e)
		}
		r2, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, in, b)
		if e != nil || fresh {
			t.Fatal(fresh, e)
		}
		rsvpPGSameReceipt(t, r, r2)
		cancelOp := f.uuid()
		c, changed, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: cancelOp, Operation: "CANCEL_RSVP"}, f.bound(id, "CANCEL_RSVP"))
		if e != nil || !changed || c.State != "cancelled" || c.EffectID != r.EffectID {
			t.Fatal(c, changed, e)
		}
		old, e := f.store.ReadHumanParticipationOperation(f.ctx, f.a, id, op)
		if e != nil {
			t.Fatal(e)
		}
		rsvpPGSameReceipt(t, r, old)
		cfg := f.pool.Config().Copy()
		cfg.MaxConns = 1
		pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		old, e = New(pool, false).ReadHumanParticipationOperation(ctx, f.a, id, op)
		if e != nil {
			t.Fatal("single-connection native read stalled", e)
		}
		rsvpPGSameReceipt(t, r, old)
		p, a, n := f.counts(id)
		if p != 1 || a != 2 || n != 2 {
			t.Fatal(p, a, n)
		}
	})
	t.Run("two_pools_100_same_key_only_one_transition", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		cfg := f.pool.Config().Copy()
		pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		other := New(pool, false)
		var wg sync.WaitGroup
		results := make(chan ap.OperationReceipt, 100)
		errs := make(chan error, 100)
		fresh := make(chan bool, 100)
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				s := f.store
				if i%2 == 0 {
					s = other
				}
				r, c, e := s.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
				results <- r
				errs <- e
				fresh <- c
			}(i)
		}
		wg.Wait()
		close(results)
		close(errs)
		close(fresh)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		var first ap.OperationReceipt
		for r := range results {
			if first.OperationID == "" {
				first = r
			} else {
				rsvpPGSameReceipt(t, first, r)
			}
		}
		n := 0
		for c := range fresh {
			if c {
				n++
			}
		}
		p, a, r := f.counts(id)
		if n != 1 || p != 1 || a != 1 || r != 1 {
			t.Fatal(n, p, a, r)
		}
	})
	t.Run("close_absent_does_not_infer_current_list_and_blocks_late_post", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		in := ap.OperationInput{OperationID: op, Operation: "JOIN"}
		if _, e := f.store.ReadHumanParticipationOperation(f.ctx, f.a, id, op); !errors.Is(e, ea.ErrNotFound) {
			t.Fatal(e)
		}
		d, _ := ap.HumanParticipationRequestDigest(f.a.Actor.ID, id, in, b)
		c, e := f.store.CloseHumanParticipationOperation(f.ctx, f.a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: d})
		if e != nil || !c.Closed {
			t.Fatal(c, e)
		}
		r, fresh, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, in, b)
		if e != nil || fresh || r.Status != "NO_EFFECT" || r.Reason != "USER_CLOSED" {
			t.Fatal(r, fresh, e)
		}
		p, a, n := f.counts(id)
		if p != 0 || a != 0 || n != 1 {
			t.Fatal(p, a, n)
		}
		c2, e := f.store.ReadHumanParticipationClosure(f.ctx, f.a, id, op)
		if e != nil || c2 != c {
			t.Fatal(c2, e)
		}
		wrong := b
		wrong.SourceVersion = strings.Repeat("b", 64)
		if _, _, e = f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, in, wrong); !errors.Is(e, ap.ErrOperationConflict) {
			t.Fatal("different approved payload reused permanent key", e)
		}
	})
	t.Run("committed_close_is_opaque_not_cancel_and_survives_source_revoke", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		r, _, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
		if e != nil {
			t.Fatal(e)
		}
		f.exec(`UPDATE activities SET publication_status='draft',updated_at=clock_timestamp() WHERE id=$1`, id)
		if _, e = f.store.ReadHumanParticipationOperation(f.ctx, f.a, id, op); !errors.Is(e, ea.ErrNotFound) {
			t.Fatal("revoked source disclosed old full result", e)
		}
		c, e := f.store.CloseHumanParticipationOperation(f.ctx, f.a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: r.RequestDigest})
		if e != nil || !c.Closed {
			t.Fatal(c, e)
		}
		raw, e := scanParticipationOperation(f.pool.QueryRow(f.ctx, `SELECT `+participationOperationColumns+` FROM human_participation_operation_receipts WHERE owner_account_id=$1 AND operation_id=$2`, f.a.Actor.ID, op))
		if e != nil {
			t.Fatal(e)
		}
		rsvpPGSameReceipt(t, r, raw)
		p, e := f.store.GetParticipation(f.ctx, f.a.Actor.ID, id)
		if e != nil || p.Status != "going" {
			t.Fatal("close cancelled an actual RSVP", p, e)
		}
		if _, e = f.store.ReadHumanParticipationClosure(f.ctx, f.a, f.uuid(), op); !errors.Is(e, ap.ErrOperationConflict) {
			t.Fatal("cross-target old operation leaked", e)
		}
	})
	t.Run("owner_session_and_expired_old_approval_do_not_resurrect", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		d, _ := ap.HumanParticipationRequestDigest(f.a.Actor.ID, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
		a := f.a
		a.Actor.ID = f.host
		if _, e := f.store.CloseHumanParticipationOperation(f.ctx, a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: d}); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal(e)
		}
		old := b
		old.ValidUntil = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, _, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, old); !errors.Is(e, ea.ErrChanged) {
			t.Fatal("old approval revived", e)
		}
		p, aud, r := f.counts(id)
		if p != 0 || aud != 0 || r != 0 {
			t.Fatal(p, aud, r)
		}
	})
	t.Run("execute_wins_close_wait_returns_original_commit", func(t *testing.T) { rsvpPGRace(t, f, false) })
	t.Run("close_wins_late_execute_wait_returns_permanent_no_effect", func(t *testing.T) { rsvpPGRace(t, f, true) })
}

func rsvpPGRace(t *testing.T, f *rsvpPGFixture, closeFirst bool) {
	t.Helper()
	f.t = t
	id, op := f.activity(), f.uuid()
	b := f.bound(id, "JOIN")
	in := ap.OperationInput{OperationID: op, Operation: "JOIN"}
	d, _ := ap.HumanParticipationRequestDigest(f.a.Actor.ID, id, in, b)
	waitStatus := "COMMITTED"
	if closeFirst {
		waitStatus = "NO_EFFECT"
	}
	f.exec(`CREATE FUNCTION rsvp117_race_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='` + waitStatus + `' THEN PERFORM pg_advisory_xact_lock(117038,5);END IF;RETURN NEW;END $$`)
	f.exec(`CREATE TRIGGER rsvp117_race_wait BEFORE INSERT ON human_participation_operation_receipts FOR EACH ROW EXECUTE FUNCTION rsvp117_race_wait()`)
	defer f.exec(`DROP TRIGGER rsvp117_race_wait ON human_participation_operation_receipts;DROP FUNCTION rsvp117_race_wait()`)
	hold, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(f.ctx, `SELECT pg_advisory_xact_lock(117038,5)`); e != nil {
		t.Fatal(e)
	}
	firstName, secondName := "rsvp117-race-first-"+op, "rsvp117-race-second-"+op
	first, _ := rsvpPGStoreNamed(t, f, firstName)
	second, _ := rsvpPGStoreNamed(t, f, secondName)
	type result struct {
		r ap.OperationReceipt
		e error
	}
	writeDone := make(chan result, 1)
	closeDone := make(chan error, 1)
	write := func(s *Store) {
		r, _, e := s.WriteHumanParticipationOperation(f.ctx, f.a, id, in, b)
		writeDone <- result{r, e}
	}
	closeOp := func(s *Store) {
		_, e := s.CloseHumanParticipationOperation(f.ctx, f.a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: d})
		closeDone <- e
	}
	if closeFirst {
		go closeOp(first)
	} else {
		go write(first)
	}
	rsvpPGWait(t, f, firstName)
	if closeFirst {
		go write(second)
	} else {
		go closeOp(second)
	}
	rsvpPGWait(t, f, secondName)
	if e = hold.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	w := <-writeDone
	if w.e != nil {
		t.Fatal(w.e)
	}
	if e = <-closeDone; e != nil {
		t.Fatal(e)
	}
	p, a, n := f.counts(id)
	if closeFirst {
		if w.r.Status != "NO_EFFECT" || p != 0 || a != 0 || n != 1 {
			t.Fatal(w.r, p, a, n)
		}
	} else {
		if w.r.Status != "COMMITTED" || p != 1 || a != 1 || n != 1 {
			t.Fatal(w.r, p, a, n)
		}
	}
}

func rsvpPGMigration(t *testing.T, f *rsvpPGFixture, name string) error {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	_, e = conn.Exec(f.ctx, string(raw))
	if e != nil {
		_, _ = conn.Exec(f.ctx, "ROLLBACK")
	}
	return e
}
func rsvpPGWait(t *testing.T, f *rsvpPGFixture, name string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		var wait bool
		if e := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, name).Scan(&wait); e != nil {
			t.Fatal(e)
		}
		if wait {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual native lock wait not observed")
}
func rsvpPGStoreNamed(t *testing.T, f *rsvpPGFixture, name string) (*Store, *pgxpool.Pool) {
	t.Helper()
	cfg := f.pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	p, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return New(p, false), p
}
func TestHumanParticipationOperationNativeMigrationAndFinalClock(t *testing.T) {
	f := rsvpPG(t)
	t.Run("unused_117_down_reapply_preserves_original_rows", func(t *testing.T) {
		f.t = t
		id := f.activity()
		p, _, e := f.store.JoinActivity(f.ctx, f.a.Actor.ID, id)
		if e != nil {
			t.Fatal(e)
		}
		var before, after string
		q := `SELECT row_to_json(p)::text||p.xmin::text FROM activity_participations p WHERE id=$1`
		if e = f.pool.QueryRow(f.ctx, q, p.ID).Scan(&before); e != nil {
			t.Fatal(e)
		}
		if e = rsvpPGMigration(t, f, "117_human_participation_operation_receipts.down.sql"); e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.ReadHumanParticipationClosure(f.ctx, f.a, id, f.uuid()); !errors.Is(e, ea.ErrUnavailable) {
			t.Fatal("missing schema fell back", e)
		}
		if e = rsvpPGMigration(t, f, "117_human_participation_operation_receipts.sql"); e != nil {
			t.Fatal(e)
		}
		if e = f.pool.QueryRow(f.ctx, q, p.ID).Scan(&after); e != nil || before != after {
			t.Fatal("migration rewrote original RSVP", e)
		}
	})
	t.Run("receipt_trigger_wait_then_original_source_final_gate_rolls_back", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		f.exec(`CREATE FUNCTION rsvp117_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(117038,1);RETURN NEW;END $$`)
		f.exec(`CREATE TRIGGER rsvp117_wait BEFORE INSERT ON human_participation_operation_receipts FOR EACH ROW EXECUTE FUNCTION rsvp117_wait()`)
		defer f.exec(`DROP TRIGGER rsvp117_wait ON human_participation_operation_receipts;DROP FUNCTION rsvp117_wait()`)
		hold, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer hold.Rollback(context.Background())
		if _, e = hold.Exec(f.ctx, `SELECT pg_advisory_xact_lock(117038,1)`); e != nil {
			t.Fatal(e)
		}
		name := "rsvp117-source-" + op
		s, _ := rsvpPGStoreNamed(t, f, name)
		done := make(chan error, 1)
		go func() {
			_, _, e := s.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
			done <- e
		}()
		rsvpPGWait(t, f, name)
		f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.host, f.a.Actor.ID)
		if e = hold.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		e = <-done
		if !errors.Is(e, ea.ErrChanged) {
			t.Fatal("source changed during receipt wait accepted", e)
		}
		p, a, r := f.counts(id)
		if p != 0 || a != 0 || r != 0 {
			t.Fatal("failed final source check committed effects", p, a, r)
		}
		f.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.host, f.a.Actor.ID)
	})
	t.Run("close_operation_wait_rechecks_native_session", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		d, _ := ap.HumanParticipationRequestDigest(f.a.Actor.ID, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
		f.exec(`UPDATE sessions SET expires_at=statement_timestamp()+interval '800 milliseconds',idle_expires_at=statement_timestamp()+interval '800 milliseconds' WHERE token_sha256=$1`, f.a.SessionDigest[:])
		hold, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer hold.Rollback(context.Background())
		if _, e = hold.Exec(f.ctx, participationOperationLock, f.a.Actor.ID, op); e != nil {
			t.Fatal(e)
		}
		name := "rsvp117-close-" + op
		s, _ := rsvpPGStoreNamed(t, f, name)
		done := make(chan error, 1)
		go func() {
			_, e := s.CloseHumanParticipationOperation(f.ctx, f.a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: d})
			done <- e
		}()
		rsvpPGWait(t, f, name)
		time.Sleep(900 * time.Millisecond)
		if e = hold.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("expired native session closed key after lock wait", e)
		}
		f.exec(`UPDATE sessions SET expires_at=statement_timestamp()+interval '1 hour',idle_expires_at=statement_timestamp()+interval '1 hour' WHERE token_sha256=$1`, f.a.SessionDigest[:])
		p, a, r := f.counts(id)
		if p != 0 || a != 0 || r != 0 {
			t.Fatal(p, a, r)
		}
	})
	t.Run("receipt_trigger_wait_then_close_final_clock_rolls_back", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		b := f.bound(id, "JOIN")
		d, _ := ap.HumanParticipationRequestDigest(f.a.Actor.ID, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, b)
		f.exec(`UPDATE sessions SET expires_at=statement_timestamp()+interval '800 milliseconds',idle_expires_at=statement_timestamp()+interval '800 milliseconds' WHERE token_sha256=$1`, f.a.SessionDigest[:])
		f.exec(`CREATE FUNCTION rsvp117_close_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(117038,2);RETURN NEW;END $$`)
		f.exec(`CREATE TRIGGER rsvp117_close_wait BEFORE INSERT ON human_participation_operation_receipts FOR EACH ROW EXECUTE FUNCTION rsvp117_close_wait()`)
		defer f.exec(`DROP TRIGGER rsvp117_close_wait ON human_participation_operation_receipts;DROP FUNCTION rsvp117_close_wait()`)
		hold, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer hold.Rollback(context.Background())
		if _, e = hold.Exec(f.ctx, `SELECT pg_advisory_xact_lock(117038,2)`); e != nil {
			t.Fatal(e)
		}
		name := "rsvp117-close-tail-" + op
		s, _ := rsvpPGStoreNamed(t, f, name)
		done := make(chan error, 1)
		go func() {
			_, e := s.CloseHumanParticipationOperation(f.ctx, f.a, id, op, ap.OperationCloseInput{Operation: "JOIN", RequestDigest: d})
			done <- e
		}()
		rsvpPGWait(t, f, name)
		time.Sleep(900 * time.Millisecond)
		if e = hold.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("receipt wait superseded close final clock", e)
		}
		f.exec(`UPDATE sessions SET expires_at=statement_timestamp()+interval '1 hour',idle_expires_at=statement_timestamp()+interval '1 hour' WHERE token_sha256=$1`, f.a.SessionDigest[:])
		p, a, r := f.counts(id)
		if p != 0 || a != 0 || r != 0 {
			t.Fatal(p, a, r)
		}
	})
	t.Run("immutable_used_down_forged_and_null_shapes_rejected", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		r, _, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, f.bound(id, "JOIN"))
		if e != nil {
			t.Fatal(e)
		}
		for _, q := range []string{`UPDATE human_participation_operation_receipts SET request_digest=repeat('b',64)`, `DELETE FROM human_participation_operation_receipts`, `TRUNCATE human_participation_operation_receipts CASCADE`} {
			_, e = f.pool.Exec(f.ctx, q)
			var pge *pgconn.PgError
			if !errors.As(e, &pge) || pge.Code != "55000" {
				t.Fatal("immutable guard not native", q, e)
			}
		}
		if e = rsvpPGMigration(t, f, "117_human_participation_operation_receipts.down.sql"); e == nil {
			t.Fatal("used fence downgraded")
		}
		stored, e := f.store.ReadHumanParticipationOperation(f.ctx, f.a, id, op)
		if e != nil {
			t.Fatal(e)
		}
		rsvpPGSameReceipt(t, r, stored)
		for _, kind := range []string{"borrow-old-row", "no-close-audit", "null-noeffect-reason", "null-committed-state"} {
			t.Run(kind, func(t *testing.T) {
				f.t = t
				fakeOp := f.uuid()
				tx, e := f.pool.Begin(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(context.Background())
				var state, reason, effectID any
				var effectAt any
				status := "NO_EFFECT"
				if kind == "borrow-old-row" || kind == "null-committed-state" {
					status = "COMMITTED"
					state = "going"
					effectID = r.EffectID
					effectAt = r.EffectAt
					if kind == "null-committed-state" {
						state = nil
						if _, e = tx.Exec(f.ctx, `UPDATE activity_participations SET updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at`, r.EffectID); e != nil {
							t.Fatal(e)
						}
						var stamp time.Time
						if e = tx.QueryRow(f.ctx, `SELECT updated_at FROM activity_participations WHERE id=$1`, r.EffectID).Scan(&stamp); e != nil {
							t.Fatal(e)
						}
						effectAt = stamp
						if e = insertDomainAudit(f.ctx, tx, f.a.Actor.ID, "join", "activity_participation", r.EffectID, "human_rsvp", &id); e != nil {
							t.Fatal(e)
						}
					}
				} else {
					reason = "USER_CLOSED"
					if kind == "null-noeffect-reason" {
						reason = nil
						if e = insertDomainAudit(f.ctx, tx, f.a.Actor.ID, "close", "activity_participation_operation", fakeOp, "human_rsvp_operation_close", &id); e != nil {
							t.Fatal(e)
						}
					}
				}
				_, e = tx.Exec(f.ctx, `INSERT INTO human_participation_operation_receipts(owner_account_id,activity_id,operation_id,operation,effect_key,request_digest,tool_version,status,recorded_at,effect_id,state,effect_at,reason) VALUES($1,$2,$3,'JOIN',$4,$5,$6,$7,clock_timestamp(),$8,$9,$10,$11)`, f.a.Actor.ID, id, fakeOp, ap.HumanParticipationEffectKey(f.a.Actor.ID, fakeOp), strings.Repeat("a", 64), ap.HumanParticipationToolVersion, status, effectID, state, effectAt, reason)
				var pge *pgconn.PgError
				if !errors.As(e, &pge) || pge.Code != "23514" {
					t.Fatal("malformed/forged permanent fence accepted", kind, e)
				}
			})
		}
	})
}
