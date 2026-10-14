package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The tracer delays the original HumanSocial authenticator after its native
// Account/Session locks, before its original fresh-clock UPDATE. It changes no
// SQL, returned rows, grants, receipts, clocks, sessions, or trigger behavior.
type participationsAuthRefreshBarrier struct {
	armed   atomic.Bool
	arrived chan int
	resume  chan struct{}
	once    sync.Once
}

func (b *participationsAuthRefreshBarrier) TraceQueryStart(ctx context.Context, conn *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "UPDATE sessions se SET idle_expires_at=LEAST(se.expires_at,stamp.at+interval '30 minutes')") && b.armed.CompareAndSwap(true, false) {
		b.arrived <- int(conn.PgConn().PID())
		select {
		case <-b.resume:
		case <-ctx.Done():
		}
	}
	return ctx
}

func (*participationsAuthRefreshBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}
func (b *participationsAuthRefreshBarrier) release() { b.once.Do(func() { close(b.resume) }) }

// Every child uses the existing disposable/native migration fixture. The
// additional pools copy the child's actual config, never the active phone DB.
func participationsAuthPool(t *testing.T, b *interestFixture, name string, tracer pgx.QueryTracer) (*pgxpool.Pool, int) {
	t.Helper()
	cfg := b.pool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	cfg.ConnConfig.Tracer = tracer
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal("create child-only original auth pool", e)
	}
	t.Cleanup(pool.Close)
	var pid int
	if e = pool.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	return pool, pid
}

func participationsAuthWait(t *testing.T, b *interestFixture, waiter, holder int) time.Time {
	t.Helper()
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		var began *time.Time
		e := b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a
		 WHERE a.datname=current_database() AND a.pid=$1 AND a.wait_event_type='Lock'
		 AND $2::integer=ANY(pg_blocking_pids(a.pid)) AND a.query ILIKE '%sessions%'),
		 (SELECT xact_start FROM pg_stat_activity WHERE datname=current_database() AND pid=$1)`, waiter, holder).Scan(&waiting, &began)
		if e != nil {
			t.Fatal("observe exact original auth Session wait", e)
		}
		if waiting && began != nil {
			t.Logf("actual native auth waiter=%d holder=%d transactionStart=%s", waiter, holder, began.UTC().Format(time.RFC3339Nano))
			return *began
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact native original auth Session barrier absent", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func participationsAuthCapture(t *testing.T, b *interestFixture, a activityplan.CurrentAccess, kind string) activityplan.CurrentValidation {
	t.Helper()
	var check activityplan.CurrentValidation
	var e error
	if kind == "plans" {
		_, check, e = b.s.ListActivityPlansCurrent(b.ctx, a)
	} else {
		_, check, e = b.s.ListParticipationsCurrent(b.ctx, a)
	}
	if e != nil || check == nil || check(b.ctx) != nil {
		t.Fatal("original native list proof", e)
	}
	return check
}

func participationsAuthResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("original native authenticator did not finish")
		return context.DeadlineExceeded
	}
}

func TestParticipationsCurrentNativeOriginalAuthOutOfOrderRenewal(t *testing.T) {
	for _, kind := range []string{"participations", "plans"} {
		t.Run(kind, func(t *testing.T) {
			f, a, _ := plansAuthenticateCurrentFixture(t, kind)
			b := f.f
			// Only establish an ordinary live window before any captured proof.
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '5 minutes' WHERE id=$1`, b.session)
			check := participationsAuthCapture(t, b, a, kind)
			before, e := b.s.captureOwnPlans(b.ctx, a)
			if e != nil {
				t.Fatal(e)
			}
			var initialGeneration int64
			var initialXmin string
			var initialIdle time.Time
			if e = b.row(`SELECT authorization_generation,xmin::text,idle_expires_at FROM sessions WHERE id=$1`, b.session).Scan(&initialGeneration, &initialXmin, &initialIdle); e != nil {
				t.Fatal(e)
			}
			barrier := &participationsAuthRefreshBarrier{arrived: make(chan int, 1), resume: make(chan struct{})}
			guardedPool, _ := participationsAuthPool(t, b, "participations-current-newer-auth-"+b.session, barrier)
			olderPool, olderPID := participationsAuthPool(t, b, "participations-current-older-auth-"+b.session, nil)
			ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
			defer cancel()
			defer barrier.release()
			barrier.armed.Store(true)
			newer := make(chan error, 1)
			go func() {
				actor, err := New(guardedPool, false).AuthenticateHumanSocial(ctx, a.SessionDigest)
				if err == nil && (actor.ID != a.OwnerID || actor.AccountType != "person") {
					err = errors.New("current human auth changed actual owner")
				}
				newer <- err
			}()
			var holderPID int
			select {
			case holderPID = <-barrier.arrived:
			case <-ctx.Done():
				t.Fatal("original fresh-clock authentication barrier absent", ctx.Err())
			}
			older := make(chan error, 1)
			go func() {
				actor, err := New(olderPool, false).Authenticate(ctx, a.SessionDigest)
				if err == nil && (actor.ID != a.OwnerID || actor.AccountType != "person") {
					err = errors.New("original auth changed actual owner")
				}
				older <- err
			}()
			oldTransactionStart := participationsAuthWait(t, b, olderPID, holderPID)
			var resumeAt time.Time
			if e = b.row(`SELECT clock_timestamp()`).Scan(&resumeAt); e != nil || !resumeAt.After(oldTransactionStart) {
				t.Fatal("native ordering was not proved", e)
			}
			barrier.release()
			if e = participationsAuthResult(t, newer); e != nil {
				t.Fatal("actual newer HumanSocial auth", e)
			}
			if e = participationsAuthResult(t, older); e != nil {
				t.Fatal("actual older original auth", e)
			}
			var finalGeneration int64
			var finalXmin string
			var finalIdle time.Time
			if e = b.row(`SELECT authorization_generation,xmin::text,idle_expires_at FROM sessions WHERE id=$1`, b.session).Scan(&finalGeneration, &finalXmin, &finalIdle); e != nil {
				t.Fatal(e)
			}
			after, captureError := b.s.captureOwnPlans(b.ctx, a)
			checkError := check(b.ctx)
			t.Logf("native concurrency generation %d->%d, idle %s->%s, resume=%s, sourceEqual=%t, validator=%v", initialGeneration, finalGeneration, initialIdle.UTC().Format(time.RFC3339Nano), finalIdle.UTC().Format(time.RFC3339Nano), resumeAt.UTC().Format(time.RFC3339Nano), before.proof == after.proof, checkError)
			if initialXmin == finalXmin || !finalIdle.After(initialIdle) {
				t.Fatal("actual two-auth renewal positive control absent")
			}
			if finalGeneration != initialGeneration || finalIdle.Before(resumeAt.Add(30*time.Minute)) || captureError != nil || before.proof != after.proof || checkError != nil {
				t.Fatal("normal original auth queue retired list source or overwrote newer idle with its pre-wait clock", captureError, checkError)
			}
		})
	}
}

func TestParticipationsCurrentNativeOriginalAuthenticateExpiryAfterSessionWait(t *testing.T) {
	f, a, _ := plansAuthenticateCurrentFixture(t, "participations")
	b := f.f
	var deadline time.Time
	if e := b.row(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '800 milliseconds' WHERE id=$1 RETURNING idle_expires_at`, b.session).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	check := participationsAuthCapture(t, b, a, "participations")
	pool, pid := participationsAuthPool(t, b, "participations-current-expiry-auth-"+b.session, nil)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	if _, e = held.Exec(b.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, b.session); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		actor, err := New(pool, false).Authenticate(ctx, a.SessionDigest)
		if err == nil && actor.ID != a.OwnerID {
			err = errors.New("expiry auth changed actual owner")
		}
		done <- err
	}()
	started := participationsAuthWait(t, b, pid, int(held.Conn().PgConn().PID()))
	if !started.Before(deadline) {
		t.Fatal("original auth did not start before native expiry")
	}
	for {
		var expired bool
		if e = b.row(`SELECT clock_timestamp()>=$1`, deadline).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = held.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	authError := participationsAuthResult(t, done)
	var currentIdle time.Time
	if e = b.row(`SELECT idle_expires_at FROM sessions WHERE id=$1`, b.session).Scan(&currentIdle); e != nil {
		t.Fatal(e)
	}
	checkError := check(b.ctx)
	t.Logf("actual expiry after Session wait, deadline=%s, idleAfter=%s, authenticator=%v, validator=%v", deadline.UTC().Format(time.RFC3339Nano), currentIdle.UTC().Format(time.RFC3339Nano), authError, checkError)
	if !errors.Is(authError, identity.ErrUnauthorized) || !currentIdle.Equal(deadline) || !errors.Is(checkError, identity.ErrUnauthorized) {
		t.Fatal("original auth revived a naturally expired Session after native lock wait", authError, checkError)
	}
}
