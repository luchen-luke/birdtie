package agentparticipationsignal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type nativeFixture struct {
	pool                                                 *pgxpool.Pool
	store                                                *postgres.Store
	reader                                               *Reader
	ctx                                                  context.Context
	owner, host, agent, session, activity, participation string
	access, peer                                         agentevent.Access
}

func nativeTest(t *testing.T) *nativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("set disposable PostgreSQL for actual participation signal tests")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("only disposable database may be used")
	}
	f := &nativeFixture{ctx: context.Background()}
	pool, e := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	f.pool = pool
	f.store = postgres.New(pool, false)
	f.reader = NewReader(pool, false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		owned := []string{}
		for _, id := range []string{f.owner, f.host} {
			if validID(id) {
				owned = append(owned, id)
			}
		}
		for _, q := range []string{`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE host_account_id=ANY($1::uuid[])`, `DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`} {
			if _, err := pool.Exec(ctx, q, owned); err != nil {
				t.Error("exact owned fixture cleanup", err)
			}
		}
		pool.Close()
	})
	for _, target := range []*string{&f.owner, &f.host} {
		if e = pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type,status) VALUES(gen_random_uuid(),'person','active') RETURNING id`).Scan(target); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(f.ctx, `INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'仅本地合成参与用户','私密fixture正文不进入信号','private')`, *target); e != nil {
			t.Fatal(e)
		}
	}
	if e = pool.QueryRow(f.ctx, `INSERT INTO agents(id,agent_type,principal_account_id,status) VALUES(gen_random_uuid(),'personal',$1,'active') RETURNING id`, f.owner).Scan(&f.agent); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		owner  string
		access *agentevent.Access
	}{{f.owner, &f.access}, {f.host, &f.peer}} {
		digest := sha256.Sum256([]byte(tc.owner))
		tc.access.SessionDigest = digest
		var sid string
		if e = pool.QueryRow(f.ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '2 hours',now()+interval '1 hour') RETURNING id`, tc.owner, digest[:]).Scan(&sid); e != nil {
			t.Fatal(e)
		}
		if tc.owner == f.owner {
			f.session = sid
		}
	}
	a, e := f.store.CreateSocialDraft(f.ctx, f.host, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.host}, CityID: "aberdeen-gb", Title: "合成024活动标题不输出", Summary: "合成024活动私密说明不输出", StartsAt: time.Now().Add(48 * time.Hour), EndsAt: time.Now().Add(50 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal("native draft", e)
	}
	f.activity = a.ID
	if _, e = f.store.PublishSocialActivity(f.ctx, f.host, a.ID); e != nil {
		t.Fatal("native publish", e)
	}
	p, _, e := f.store.JoinActivity(f.ctx, f.owner, a.ID)
	if e != nil {
		t.Fatal("native join", e)
	}
	f.participation = p.ID
	return f
}
func (f *nativeFixture) req() Request {
	return Request{f.participation, "78000000-0000-4000-8000-000000000011"}
}
func (f *nativeFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}

// TTL is anchored to the mutable native source updated_at. Creation and
// participation identity remain immutable, including under disclosure schema078.
// This explicitly models an aged retained source, not a new live RSVP.
func (f *nativeFixture) expireRetainedSource(t *testing.T) {

	t.Helper()
	const identityQuery = `SELECT jsonb_build_array(p.id,p.activity_id,p.participant_account_id,p.created_at)::text,COALESCE((to_jsonb(p)->>'disclosure_revision')::bigint,0) FROM activity_participations p WHERE id=$1`
	var before, after string
	var oldRevision, newRevision int64
	if e := f.pool.QueryRow(f.ctx, identityQuery, f.participation).Scan(&before, &oldRevision); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE activity_participations SET updated_at=now()-interval '16 minutes' WHERE id=$1`, f.participation)
	if e := f.pool.QueryRow(f.ctx, identityQuery, f.participation).Scan(&after, &newRevision); e != nil {
		t.Fatal(e)
	}
	if before != after || (oldRevision > 0 && newRevision != oldRevision+1) {
		t.Fatal("aging source changed stable identity or failed to advance its native disclosure revision")
	}
}
func TestParticipationSignalNativeJoinPendingCancelAndRestoreIntegration(t *testing.T) {
	f := nativeTest(t)
	going, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil || going.Kind != RSVPGoing || going.Attendance != "UNKNOWN" || going.StableInterest != "UNKNOWN" {
		t.Fatal("actual going signal", e)
	}
	for i := 0; i < 20; i++ {
		s, e := f.reader.Collect(f.ctx, f.access, f.req())
		if e != nil || s.SignalID != going.SignalID || !s.ExpiresAt.Equal(going.ExpiresAt) {
			t.Fatal("retry changed current-state receipt", e)
		}
	}
	if e = f.reader.Revalidate(f.ctx, f.access, going); e != nil {
		t.Fatal(e)
	}
	// No in-memory receipt cache is required: a new Reader derives the same
	// current native source version and cannot extend its source-anchored TTL.
	reattached, err := NewReader(f.pool, false).Collect(f.ctx, f.access, f.req())
	if err != nil || reattached.SignalID != going.SignalID || !reattached.ExpiresAt.Equal(going.ExpiresAt) {
		t.Fatal("fresh reader lost persisted source binding", err)
	}
	raw, _ := json.Marshal(going)
	for _, bad := range []string{"合成024活动标题", "合成024活动私密说明", "私密fixture正文", "token_sha256", "confirmed", "consent_epoch", "latitude"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal("extra private data/authority copied")
		}
	}
	// Native schema supports pending, but current JoinActivity only writes going.
	// This is a retained SQL state-shape test, not a live request-approval feature.
	f.exec(t, `UPDATE activity_participations SET status='pending',updated_at=clock_timestamp() WHERE id=$1`, f.participation)
	pending, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil || pending.Kind != RSVPPending {
		t.Fatal("pending mislabeled joined", e)
	}
	if e = f.reader.Revalidate(f.ctx, f.access, going); !errors.Is(e, ErrDenied) {
		t.Fatal("old state restored", e)
	}
	if _, e = f.store.CancelParticipation(f.ctx, f.owner, f.activity); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.activity)
	cancelled, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil || cancelled.Kind != RSVPCancelled || cancelled.ActivityID != "" || cancelled.ActivityRevision != 0 {
		t.Fatal("retained invalidation disclosed hidden activity", e)
	}
	second, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil || cancelled.SignalID != second.SignalID {
		t.Fatal(e)
	}
	if e = f.reader.Revalidate(f.ctx, f.access, pending); !errors.Is(e, ErrDenied) {
		t.Fatal("pending replay ignored cancellation", e)
	}
}
func TestParticipationSignalNativeOrganizerIntegrityIntegration(t *testing.T) {
	f := nativeTest(t)
	_, err := f.pool.Exec(f.ctx, `DELETE FROM activity_organizers WHERE activity_id=$1`, f.activity)
	if err == nil {
		t.Fatal("native schema allowed a retained Activity without organizer")
	}
	s, err := f.reader.Collect(f.ctx, f.access, f.req())
	if err != nil || s.Kind != RSVPGoing {
		t.Fatal("failed native mutation changed valid current state", err)
	}
}
func TestParticipationSignalNativeRescheduleInvalidatesReceiptIntegration(t *testing.T) {
	f := nativeTest(t)
	before, err := f.reader.Collect(f.ctx, f.access, f.req())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.UpdateSocialActivity(f.ctx, f.host, f.activity, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.host}, CityID: "aberdeen-gb", Title: "合成024改期活动不输出", Summary: "合成024私密说明不输出", StartsAt: time.Now().Add(72 * time.Hour), EndsAt: time.Now().Add(74 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if err != nil || updated.Revision <= before.ActivityRevision {
		t.Fatal("native reschedule failed", err)
	}
	if err = f.reader.Revalidate(f.ctx, f.access, before); !errors.Is(err, ErrDenied) {
		t.Fatal("old receipt survived native reschedule", err)
	}
	after, err := f.reader.Collect(f.ctx, f.access, f.req())
	if err != nil || after.ActivityRevision != updated.Revision || after.SignalID == before.SignalID || after.Attendance != "UNKNOWN" || after.StableInterest != "UNKNOWN" {
		t.Fatal("reschedule invented attendance or interest", err)
	}
}
func TestParticipationSignalNativeCurrentIdentityAndTargetIntegration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *nativeFixture)
	}{
		{"anonymous", func(t *testing.T, f *nativeFixture) { f.access = agentevent.Access{} }}, {"peer", func(t *testing.T, f *nativeFixture) { f.access = f.peer }},
		{"revoked", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.session)
		}},
		{"absolute_expired", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.session)
		}},
		{"idle_expired", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.session)
		}},
		{"dev_phone", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, f.session)
		}},
		{"person_suspended", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.owner)
		}},
		{"host_suspended", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.host)
		}},
		{"host_reverse_block", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.host, f.owner)
		}},
		{"activity_private", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE activities SET visibility='private' WHERE id=$1`, f.activity)
		}},
		{"agent_retired", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE agents SET status='retired' WHERE id=$1`, f.agent)
		}},
		{"metadata_missing", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agent)
		}},
		{"activity_hidden", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.activity)
		}},
		{"activity_expired", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.activity)
		}},
		{"activity_ended", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `UPDATE activities SET starts_at=now()-interval '2 hours',ends_at=now()-interval '1 hour' WHERE id=$1`, f.activity)
		}},
		{"activity_cancelled", func(t *testing.T, f *nativeFixture) {
			if _, e := f.store.CancelSocialActivity(f.ctx, f.host, f.activity); e != nil {
				t.Fatal(e)
			}
		}},
		{"blocked_host", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.owner, f.host)
		}},
		{"source_deleted", func(t *testing.T, f *nativeFixture) {
			f.exec(t, `DELETE FROM activity_participations WHERE id=$1`, f.participation)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeTest(t)
			tc.mutate(t, f)
			s, e := f.reader.Collect(f.ctx, f.access, f.req())
			if !errors.Is(e, ErrDenied) || s.SignalID != "" {
				t.Fatal("invalid current boundary returned payload", e)
			}
		})
	}
}
func TestParticipationSignalNativeVersionTTLTimezoneAndConcurrencyIntegration(t *testing.T) {
	f := nativeTest(t)
	first, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil {
		t.Fatal(e)
	}
	for _, zone := range []string{"Asia/Shanghai", "Pacific/Honolulu", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["TimeZone"] = zone
			p, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			s, e := NewReader(p, false).Collect(f.ctx, f.access, f.req())
			if e != nil || s.SignalID != first.SignalID {
				t.Fatal("timezone changed native token", e)
			}
		})
	}
	var wg sync.WaitGroup
	issues := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := f.reader.Collect(f.ctx, f.access, f.req())
			if e != nil {
				issues <- e
			} else if s.SignalID != first.SignalID {
				issues <- errors.New("concurrent retry changed signal")
			}
		}()
	}
	wg.Wait()
	close(issues)
	for e := range issues {
		t.Error(e)
	}
	// Same native revision/time is deliberately kept: lifecycle digest still
	// detects the changed schedule instead of turning it into attendance.
	f.exec(t, `UPDATE activities SET starts_at=starts_at+interval '1 hour',ends_at=ends_at+interval '1 hour' WHERE id=$1`, f.activity)
	if e = f.reader.Revalidate(f.ctx, f.access, first); !errors.Is(e, ErrDenied) {
		t.Fatal("reschedule ignored", e)
	}
	fresh, e := f.reader.Collect(f.ctx, f.access, f.req())
	if e != nil || fresh.Attendance != "UNKNOWN" || fresh.ActivityRevision != first.ActivityRevision {
		t.Fatal(e)
	}
	// The retained native row digest detects mutations even if updated_at was
	// not advanced by an out-of-band repair. It remains an opaque version.
	f.exec(t, `UPDATE activity_participations SET status='pending' WHERE id=$1`, f.participation)
	if e = f.reader.Revalidate(f.ctx, f.access, fresh); !errors.Is(e, ErrDenied) {
		t.Fatal("same-clock source change accepted", e)
	}
	f.exec(t, `UPDATE activity_participations SET status='going' WHERE id=$1`, f.participation)
	f.exec(t, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agent)
	f.exec(t, `INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES($1,'PERSON',$2)`, f.agent, f.owner)
	if e = f.reader.Revalidate(f.ctx, f.access, fresh); !errors.Is(e, ErrDenied) {
		t.Fatal("metadata rebuild resurrected old signal", e)
	}
	f.expireRetainedSource(t)
	if s, e := f.reader.Collect(f.ctx, f.access, f.req()); !errors.Is(e, ErrExpired) || s.SignalID != "" {
		t.Fatal("source TTL was renewed", e)
	}
}

type phaseTracer struct {
	mu               sync.Mutex
	count            int
	at               int
	entered, release chan struct{}
}

func (p *phaseTracer) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "participation_signal_current_source_v1") {
		p.mu.Lock()
		p.count++
		n := p.count
		p.mu.Unlock()
		if n == p.at {
			close(p.entered)
			select {
			case <-p.release:
			case <-ctx.Done():
			}
		}
	}
	return ctx
}
func (*phaseTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestParticipationSignalFinalNativeBoundaryAndLateContextIntegration(t *testing.T) {
	for _, change := range []string{"revoke", "session_expire", "source_expire", "source_cancel", "activity_reschedule", "host_suspend", "metadata_remove", "context_cancel"} {
		t.Run(change, func(t *testing.T) {
			f := nativeTest(t)
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			tracer := &phaseTracer{at: 2, entered: make(chan struct{}), release: make(chan struct{})}
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.Tracer = tracer
			// A caller's pool default must never turn the final authorization
			// check into a retained REPEATABLE READ snapshot.
			cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
			p, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			done := make(chan error, 1)
			go func() {
				s, e := NewReader(p, false).Collect(ctx, f.access, f.req())
				if e == nil || s.SignalID != "" {
					done <- errors.New("late changed boundary returned payload")
				} else {
					done <- e
				}
			}()
			select {
			case <-tracer.entered:
			case <-ctx.Done():
				t.Fatal("actual final query barrier not reached")
			}
			switch change {
			case "revoke":
				f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			case "session_expire":
				f.exec(t, `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.session)
			case "source_expire":
				f.expireRetainedSource(t)
			case "activity_reschedule":
				f.exec(t, `UPDATE activities SET starts_at=starts_at+interval '1 hour',ends_at=ends_at+interval '1 hour' WHERE id=$1`, f.activity)
			case "host_suspend":
				f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.host)
			case "source_cancel":
				if _, e = f.store.CancelParticipation(f.ctx, f.owner, f.activity); e != nil {
					t.Fatal(e)
				}
			case "metadata_remove":
				f.exec(t, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agent)
			case "context_cancel":
				cancel()
			}
			close(tracer.release)
			e = <-done
			if change == "context_cancel" {
				if !errors.Is(e, context.Canceled) {
					t.Fatal(e)
				}
			} else if change == "source_expire" {
				if !errors.Is(e, ErrExpired) {
					t.Fatal("current source expiry not rejected", e)
				}
			} else if !errors.Is(e, ErrDenied) {
				t.Fatal("current final boundary not rejected", e)
			}
		})
	}
}
