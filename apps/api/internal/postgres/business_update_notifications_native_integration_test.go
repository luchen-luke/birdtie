package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type businessUpdateNative struct {
	f       *businessConsoleFixture
	id      string
	profile businessconsole.Profile
}

func newBusinessUpdateNative(t *testing.T) *businessUpdateNative {
	t.Helper()
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		f.ctx = cleanup
		for _, q := range []string{
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM native_notification_schedule_deliveries WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM native_notification_schedule_slots WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM native_notification_schedules WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR source_id IN(SELECT id FROM business_public_profile_audit WHERE business_id=$2)`,
			`DELETE FROM native_notification_policies WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
		} {
			if strings.Contains(q, "$2") {
				f.exec(t, q, f.people, id)
			} else {
				f.exec(t, q, f.people)
			}
		}
		f.exec(t, `DELETE FROM business_public_profile_audit WHERE business_id=$1`, id)
		f.exec(t, `DELETE FROM business_public_profile_permissions WHERE business_id=$1`, id)
	})
	f.claim(t, id)
	f.grant(t, id)
	if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地合成经营声明"}); e != nil {
		t.Fatal(e)
	}
	input := businessProfileNativeInput(time.Now().UTC())
	if _, e := f.store.PutBusinessProfile(f.ctx, f.who(0, id), input); e != nil {
		t.Fatal(e)
	}
	p, e := f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地合成具体资料"})
	if e != nil {
		t.Fatal(e)
	}
	return &businessUpdateNative{f, id, p}
}
func (f *businessUpdateNative) follow(t *testing.T, i int) {
	t.Helper()
	if _, e := f.f.store.Follow(f.f.ctx, f.f.people[i], follow.Target{Type: follow.Business, ID: f.id}); e != nil {
		t.Fatal(e)
	}
}
func (f *businessUpdateNative) input(t *testing.T, ttl time.Duration) supplierprofile.PermissionInput {
	t.Helper()
	v, e := f.f.store.ReadBusinessPublicPermission(f.f.ctx, f.f.who(0, f.id))
	if e != nil || v.Preview == nil {
		t.Fatal("real public preview", e)
	}
	deadline := time.Now().UTC().Add(ttl).Truncate(time.Microsecond)
	if deadline.After(v.Preview.ValidUntil) {
		deadline = v.Preview.ValidUntil
	}
	return supplierprofile.PermissionInput{ExpectedVersion: v.Permission.Version, ExpectedProfileVersion: v.Preview.ProfileVersion, Action: "publish", ValidUntil: deadline.Format(time.RFC3339Nano), SourceSnapshot: v.Preview.SourceSnapshot}
}
func (f *businessUpdateNative) publish(t *testing.T, in supplierprofile.PermissionInput) supplierprofile.Permission {
	t.Helper()
	p, e := f.f.store.ChangeBusinessPublicPermission(f.f.ctx, f.f.who(0, f.id), in)
	if e != nil {
		t.Fatal("original explicit publish", e)
	}
	return p
}
func (f *businessUpdateNative) visible(t *testing.T, i int) int {
	t.Helper()
	items, e := f.f.store.ListInbox(f.f.ctx, f.f.people[i])
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, item := range items {
		if item.ResourceType == "business_update" {
			n++
			if item.TargetBusinessID == nil || *item.TargetBusinessID != f.id || item.ResourceID == f.id {
				t.Fatal("not actual public destination", item)
			}
		}
	}
	return n
}
func (f *businessUpdateNative) private(t *testing.T, i int) agentprofile.PrivateAccess {
	t.Helper()
	var aid string
	if e := f.f.pool.QueryRow(f.f.ctx, `INSERT INTO agents(principal_account_id,agent_type,status) VALUES($1,'personal','active') RETURNING id`, f.f.people[i]).Scan(&aid); e != nil {
		t.Fatal(e)
	}
	p, e := actorref.ParsePrincipal("person", f.f.people[i])
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.f.store.EnsureAgentProfile(f.f.ctx, aid, p); e != nil {
		t.Fatal(e)
	}
	return agentprofile.PrivateAccess{SessionDigest: f.f.access[i].SessionDigest, WorkspacePrincipal: p}
}

func TestBusinessPublicUpdateNativeCurrentLifecycle(t *testing.T) {
	f := newBusinessUpdateNative(t)
	f.follow(t, 2)
	t.Run("verified_but_not_public_and_unfollowed_zero", func(t *testing.T) {
		if f.visible(t, 2) != 0 || f.visible(t, 3) != 0 {
			t.Fatal("review is not publication")
		}
	})
	in := f.input(t, time.Minute)
	f.publish(t, in)
	f.publish(t, in)
	t.Run("same_intent_one_real_audit_and_decision", func(t *testing.T) {
		if f.visible(t, 2) != 1 || f.visible(t, 3) != 0 {
			t.Fatal("wrong current follower")
		}
		var n, a int
		if e := f.f.pool.QueryRow(f.f.ctx, `SELECT (SELECT count(*) FROM native_notification_decisions d JOIN business_public_profile_audit a ON a.id=d.business_public_update_id WHERE a.business_id=$1),(SELECT count(*) FROM business_public_profile_audit WHERE business_id=$1 AND notification_source)`, f.id).Scan(&n, &a); e != nil || n != 1 || a != 1 {
			t.Fatal(n, a, e)
		}
	})
	t.Run("unfollow_refollow_cannot_resurrect_old_event", func(t *testing.T) {
		if e := f.f.store.Unfollow(f.f.ctx, f.f.people[2], follow.Target{Type: follow.Business, ID: f.id}); e != nil {
			t.Fatal(e)
		}
		if f.visible(t, 2) != 0 {
			t.Fatal("unfollow remains visible")
		}
		f.follow(t, 2)
		if f.visible(t, 2) != 0 {
			t.Fatal("Follow ABA revived old event")
		}
	})
	// Explicitly publish a fresh concrete permission revision for the new Follow.
	f.publish(t, f.input(t, time.Minute))
	t.Run("both_direction_block_ABA_permanent_retirement", func(t *testing.T) {
		var principal string
		if e := f.f.pool.QueryRow(f.f.ctx, `SELECT account_id FROM businesses WHERE id=$1`, f.id).Scan(&principal); e != nil {
			t.Fatal(e)
		}
		for _, ids := range [][2]string{{f.f.people[2], principal}, {principal, f.f.people[2]}} {
			if f.visible(t, 2) != 1 {
				t.Fatal("fresh publication not visible")
			}
			f.f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[0], ids[1])
			if f.visible(t, 2) != 0 {
				t.Fatal("blocked visible")
			}
			f.f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, ids[0], ids[1])
			if f.visible(t, 2) != 0 {
				t.Fatal("unblock resurrected old event")
			}
			f.publish(t, f.input(t, time.Minute))
		}
	})
	t.Run("ordinary_edit_and_review_are_not_public_event", func(t *testing.T) {
		input := businessProfileNativeInput(time.Now().UTC())
		input.ExpectedVersion = 2
		input.Facts.Description = "下一具体版本必须重新批准"
		if _, e := f.f.store.PutBusinessProfile(f.f.ctx, f.f.who(0, f.id), input); e != nil {
			t.Fatal(e)
		}
		if f.visible(t, 2) != 0 {
			t.Fatal("stale profile stayed visible")
		}
		if _, e := f.f.store.ReviewBusinessProfile(f.f.ctx, f.f.who(1, f.id), businessconsole.ReviewInput{ExpectedVersion: 3, Decision: "approve", Note: "明确批准新版本"}); e != nil {
			t.Fatal(e)
		}
		if f.visible(t, 2) != 0 {
			t.Fatal("review silently published")
		}
		f.publish(t, f.input(t, time.Minute))
		if f.visible(t, 2) != 1 {
			t.Fatal("explicit new version missing")
		}
	})
	t.Run("actual_permission_deadline_public_fields_and_notice_expire", func(t *testing.T) {
		f.publish(t, f.input(t, 2*time.Second))
		time.Sleep(2200 * time.Millisecond)
		if f.visible(t, 2) != 0 {
			t.Fatal("expired notice")
		}
		view, e := f.f.store.ReadPublicBusiness(f.f.ctx, f.id, f.f.people[2])
		if e != nil || view.ProfileStatus != "unpublished" || view.Description != "" {
			t.Fatal("expired public fields", view, e)
		}
	})
}

func TestBusinessPublicUpdateNativeDigestRealDueAndConcurrentPublish(t *testing.T) {
	f := newBusinessUpdateNative(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	f.f.ctx = ctx
	f.follow(t, 0)
	f.follow(t, 2)
	owner := f.private(t, 0)
	peer := f.private(t, 2)
	for _, a := range []agentprofile.PrivateAccess{owner, peer} {
		if _, e := f.f.store.PutOwnNotificationPolicy(ctx, a, agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Digest, Rules: []agentnotification.Rule{}, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}); e != nil {
			t.Fatal(e)
		}
	}
	due := notificationScheduleNativeDue(t, ctx, f.f.pool)
	for _, a := range []agentprofile.PrivateAccess{owner, peer} {
		_, e := f.f.store.PutOwnNotificationSchedule(ctx, a, ns.PutInput{Settings: ns.Settings{Enabled: true, TimeZone: "UTC", LocalMinute: due.Hour()*60 + due.Minute(), GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, MaxContactsPerDay: 1, Categories: []agentnotification.Category{agentnotification.CategoryBusiness}}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)})
		if e != nil {
			t.Fatal(e)
		}
	}
	first := f.input(t, 5*time.Minute)
	f.publish(t, first)
	if f.visible(t, 0) != 0 || f.visible(t, 2) != 0 {
		t.Fatal("DIGEST created immediate Inbox")
	}
	notificationScheduleNativeWait(t, ctx, f.f.pool, due)
	secondPool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer secondPool.Close()
	other := New(secondPool, false)
	second := f.input(t, 5*time.Minute)
	// Real production admission/account/table locks and original source writer,
	// shared owner and ordinary follower, two independent pools. No clock edits.
	var wg sync.WaitGroup
	errorsCh := make(chan error, 3)
	wg.Add(3)
	go func() { defer wg.Done(); _, e := f.f.store.ProcessNotificationSchedules(ctx, 10); errorsCh <- e }()
	go func() { defer wg.Done(); _, e := other.ProcessNotificationSchedules(ctx, 10); errorsCh <- e }()
	go func() {
		defer wg.Done()
		_, e := other.ChangeBusinessPublicPermission(ctx, f.f.who(0, f.id), second)
		errorsCh <- e
	}()
	wg.Wait()
	close(errorsCh)
	for e := range errorsCh {
		if e != nil {
			t.Fatal("actual original concurrent paths", e)
		}
	}
	t.Run("same_owner_follower_and_peer_one_slot_each_no_duplicate_budget", func(t *testing.T) {
		for _, i := range []int{0, 2} {
			var slots, delivered int
			if e := f.f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM native_notification_schedule_slots WHERE owner_id=$1),(SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered)`, f.f.people[i]).Scan(&slots, &delivered); e != nil || slots != 1 || delivered != 1 {
				t.Fatal(slots, delivered, e)
			}
		}
		if _, e := other.ProcessNotificationSchedules(ctx, 10); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("block_retirement_and_Inbox_delete_do_not_clear_rolling_budget", func(t *testing.T) {
		var principal string
		_ = f.f.pool.QueryRow(ctx, `SELECT account_id FROM businesses WHERE id=$1`, f.id).Scan(&principal)
		f.f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.f.people[2], principal)
		f.f.exec(t, `DELETE FROM inbox_items WHERE recipient_account_id=$1`, f.f.people[0])
		for _, i := range []int{0, 2} {
			var n int
			if e := f.f.pool.QueryRow(ctx, `SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered`, f.f.people[i]).Scan(&n); e != nil || n != 1 {
				t.Fatal("quota lost after source/Inbox delete", n, e)
			}
		}
	})
	t.Run("explicit_off_disables_new_business_digest", func(t *testing.T) {
		p, e := f.f.store.GetOwnNotificationSchedule(ctx, owner)
		if e != nil {
			t.Fatal(e)
		}
		settings := p.Settings
		settings.Enabled = false
		if p.ExpiresAt == nil {
			t.Fatal("current schedule missing deadline")
		}
		if _, e = f.f.store.PutOwnNotificationSchedule(ctx, owner, ns.PutInput{ExpectedVersion: p.Version, Settings: settings, ExpiresAt: *p.ExpiresAt}); e != nil {
			t.Fatal(e)
		}
		var visible int
		if e = f.f.pool.QueryRow(ctx, `SELECT count(*) FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.recipient_id=$1 AND birdtie_native_notification_visible(d)`, f.f.people[0]).Scan(&visible); e != nil || visible != 0 {
			t.Fatal(visible, e)
		}
	})
}

func TestBusinessPublicUpdateNativeWaitRechecksSource(t *testing.T) {
	for _, scenario := range []string{"permission_revoke", "review_grant_revoke", "profile_edit", "TTL"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBusinessUpdateNative(t)
			f.follow(t, 2)
			existingSources := 0
			if scenario == "permission_revoke" {
				f.publish(t, f.input(t, time.Minute))
				existingSources = 1
			}
			in := f.input(t, time.Minute)
			if scenario == "TTL" {
				in = f.input(t, 2*time.Second)
			}
			// Hold the original decision relation so publish has passed native Account /
			// Session admission but cannot mutate permission/audit yet.
			blocker, e := f.f.pool.Begin(f.f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			if _, e = blocker.Exec(f.f.ctx, `LOCK TABLE native_notification_decisions IN SHARE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { _, e := f.f.store.ChangeBusinessPublicPermission(f.f.ctx, f.f.who(0, f.id), in); done <- e }()
			businessPublicUpdateWaitLock(t, f.f.pool, "native_notification_decisions")
			if scenario == "TTL" {
				time.Sleep(2200 * time.Millisecond)
			} else {
				// Scoped diagnostics mutate only this fixture's source while the real
				// Store is at its relation wait. Not a faked clock or authorization API.
				query := `UPDATE business_console_profiles SET version=version+1,state='pending',reviewed_at=NULL,reviewed_by=NULL,review_note='' WHERE business_id=$1`
				// The held Business row would serialize a business change; profile change
				// is unheld here. Permission/claim retirement after committed publication
				// is covered separately; do not create a diagnostic deadlock.
				if scenario == "review_grant_revoke" {
					query = `UPDATE business_review_grants SET state='revoked' WHERE business_id=$1`
				}
				if scenario == "permission_revoke" {
					query = `UPDATE business_public_profile_permissions SET version=version+1,state='revoked',updated_at=clock_timestamp() WHERE business_id=$1`
				}
				if _, e = blocker.Exec(f.f.ctx, query, f.id); e != nil {
					t.Fatal(e)
				}
			}
			if e = blocker.Commit(f.f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				expected := businessconsole.ErrConflict
				if scenario == "TTL" {
					expected = businessconsole.ErrInvalid
				}
				if !errors.Is(e, expected) {
					t.Fatal("wait reused old source/TTL", e)
				}
			case <-time.After(12 * time.Second):
				t.Fatal("publish did not finish")
			}
			var n int
			if e = f.f.pool.QueryRow(f.f.ctx, `SELECT count(*) FROM business_public_profile_audit WHERE business_id=$1 AND notification_source`, f.id).Scan(&n); e != nil || n != existingSources {
				t.Fatal("rejected write persisted source", n, e)
			}
		})
	}
}
func businessPublicUpdateWaitLock(t *testing.T, p *pgxpool.Pool, relation string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		e := p.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks WHERE relation=$1::regclass AND NOT granted`, relation).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("real relation wait not observed")
}

// This is the original Store owner admission/locking path. No clock or schedule
// data is forged: without a plan it exits after the native source locks. The
// separate real-due test covers actual public Process nomination and delivery.
type businessPublicLockTraceTx struct {
	pgx.Tx
	t *testing.T
}

func (tx businessPublicLockTraceTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	result, e := tx.Tx.Exec(ctx, sql, args...)
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		tx.t.Log("ACTUAL_SCHEDULER_SQLSTATE " + pe.Code)
	}
	return result, e
}
func TestBusinessPublicUpdateNativeBlockScheduleLockOrder(t *testing.T) {
	ownedMigrationDatabase(t) // Diagnostic gates never alter the shared parent schema.
	for _, i := range []int{0, 2} {
		for _, order := range []string{"block_first", "schedule_first"} {
			t.Run(order+map[int]string{0: "_owner_follow", 2: "_peer_follow"}[i], func(t *testing.T) {
				f := newBusinessUpdateNative(t)
				f.follow(t, i)
				f.publish(t, f.input(t, time.Minute))
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				var principal string
				if e := f.f.pool.QueryRow(ctx, `SELECT account_id FROM businesses WHERE id=$1`, f.id).Scan(&principal); e != nil {
					t.Fatal(e)
				}
				gate, e := f.f.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer gate.Rollback(context.Background())
				blockDone := make(chan error, 1)
				scheduleDone := make(chan error, 1)
				if order == "block_first" {
					// Hold the actual INSERT after its relation lock but before
					// its original106 AFTER trigger. No replacement writer/clock.
					f.f.exec(t, `CREATE FUNCTION bu_block_test_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtextextended('bu-block:'||NEW.blocker_account_id::text,0)); RETURN NEW; END $$`)
					f.f.exec(t, `CREATE TRIGGER bu_block_test_gate BEFORE INSERT ON account_blocks FOR EACH ROW EXECUTE FUNCTION bu_block_test_gate()`)
					t.Cleanup(func() {
						f.f.exec(t, `DROP TRIGGER bu_block_test_gate ON account_blocks`)
						f.f.exec(t, `DROP FUNCTION bu_block_test_gate()`)
					})
					if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('bu-block:'||$1::text,0))`, f.f.people[i]); e != nil {
						t.Fatal(e)
					}
					go func() { blockDone <- f.f.store.BlockAccount(ctx, f.f.people[i], principal) }()
					deadline := time.Now().Add(5 * time.Second)
					for {
						var n int
						if e = f.f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&n); e != nil {
							t.Fatal(e)
						}
						if n > 0 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("actual BlockAccount INSERT diagnostic wait absent")
						}
						time.Sleep(10 * time.Millisecond)
					}
					go func() {
						tx, e := f.f.pool.Begin(ctx)
						if e == nil {
							defer tx.Rollback(context.Background())
							_, e = processNotificationScheduleOwnerTx(ctx, businessPublicLockTraceTx{tx, t}, f.f.people[i], false)
						}
						scheduleDone <- e
					}()
					businessPublicUpdateWaitLock(t, f.f.pool, "account_blocks")
				} else {
					if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('notification-schedule:'||$1::text,0))`, f.f.people[i]); e != nil {
						t.Fatal(e)
					}
					go func() {
						tx, e := f.f.pool.Begin(ctx)
						if e == nil {
							defer tx.Rollback(context.Background())
							_, e = processNotificationScheduleOwnerTx(ctx, businessPublicLockTraceTx{tx, t}, f.f.people[i], false)
						}
						scheduleDone <- e
					}()
					deadline := time.Now().Add(5 * time.Second)
					for {
						var n int
						if e = f.f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&n); e != nil {
							t.Fatal(e)
						}
						if n > 0 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("schedule native advisory wait not observed")
						}
						time.Sleep(10 * time.Millisecond)
					}
					go func() { blockDone <- f.f.store.BlockAccount(ctx, f.f.people[i], principal) }()
					businessPublicUpdateWaitLock(t, f.f.pool, "account_blocks")
				}
				var locks string
				if e = f.f.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('pid',l.pid,'relation',c.relname,'mode',l.mode,'granted',l.granted,'blocking',pg_blocking_pids(l.pid)) ORDER BY l.pid,c.relname,l.mode),'[]'::jsonb)::text FROM pg_locks l LEFT JOIN pg_class c ON c.oid=l.relation WHERE l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND(c.relname IN('native_notification_decisions','account_blocks') OR NOT l.granted)`).Scan(&locks); e != nil {
					t.Fatal(e)
				}
				t.Log("ACTUAL_NATIVE_LOCKS " + locks)
				if e = gate.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				be, se := <-blockDone, <-scheduleDone
				var pg *pgconn.PgError
				if errors.As(be, &pg) {
					t.Log("BLOCK_PG_SQLSTATE " + pg.Code)
				}
				if be != nil || se != nil {
					t.Fatal("original Block and scheduler must complete without deadlock", be, se)
				}
				if f.visible(t, i) != 0 {
					t.Fatal("current block retained public notice")
				}
			})
		}
	}
}

func businessPublicMigration(t *testing.T, p *pgxpool.Pool, name string, refuse bool) {
	t.Helper()
	raw, e := os.ReadFile("../../migrations/" + name)
	if e != nil {
		t.Fatal(e)
	}
	c, e := p.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, e = c.Exec(context.Background(), string(raw))
	if refuse {
		_, _ = c.Exec(context.Background(), "ROLLBACK")
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "23514" {
			t.Fatal("retained106 down must refuse actual23514", e)
		}
	} else if e != nil {
		_, _ = c.Exec(context.Background(), "ROLLBACK")
		t.Fatal("actual106 migration", e)
	}
}
func businessPublicSnapshot(t *testing.T, f *businessUpdateNative) string {
	t.Helper()
	var out string
	e := f.f.pool.QueryRow(f.f.ctx, `SELECT jsonb_build_object(
 'accounts',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM accounts x WHERE id=ANY($1::uuid[]) OR id=(SELECT account_id FROM businesses WHERE id=$2)),'[]'::jsonb),
 'business',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text)) FROM businesses x WHERE id=$2),'[]'::jsonb),
 'profile',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text)) FROM business_console_profiles x WHERE business_id=$2),'[]'::jsonb),
 'members',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM business_memberships x WHERE business_id=$2),'[]'::jsonb),
 'claim',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text)) FROM business_claim_controls x WHERE business_id=$2),'[]'::jsonb),
 'public',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text)) FROM business_public_profile_permissions x WHERE business_id=$2),'[]'::jsonb),
 'audit',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM business_public_profile_audit x WHERE business_id=$2),'[]'::jsonb),
 'decisions',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM native_notification_decisions x WHERE recipient_id=ANY($1::uuid[])),'[]'::jsonb),
 'inbox',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM inbox_items x WHERE recipient_account_id=ANY($1::uuid[])),'[]'::jsonb),
 'catalog',(SELECT jsonb_agg(jsonb_build_array(conname,pg_get_constraintdef(oid)) ORDER BY conname) FROM pg_constraint WHERE conrelid IN('native_notification_decisions'::regclass,'inbox_items'::regclass,'business_public_profile_audit'::regclass)),
 'functions',(SELECT jsonb_agg(pg_get_functiondef(oid) ORDER BY proname) FROM pg_proc WHERE proname IN('birdtie_native_notification_source','birdtie_native_notification_decision_guard','birdtie_native_business_public_update','birdtie_retire_business_public_notifications'))
 )::text`, f.f.people, f.id).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestBusinessPublicUpdateNativeMigrationCurrentData(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newBusinessUpdateNative(t)
	f.follow(t, 2)
	before := businessPublicSnapshot(t, f)
	t.Run("unused_down_reapply_preserves_old_current_rows_xmin_catalog", func(t *testing.T) {
		businessPublicMigration(t, f.f.pool, "106_business_public_update_notifications.down.sql", false)
		businessPublicMigration(t, f.f.pool, "106_business_public_update_notifications.sql", false)
		if after := businessPublicSnapshot(t, f); after != before {
			t.Fatal("106 roundtrip changed original current rows/xmin/catalog")
		}
	})
	f.publish(t, f.input(t, time.Minute))
	used := businessPublicSnapshot(t, f)
	t.Run("used_down_refused_preserves_new_source_effects_and_old_rows", func(t *testing.T) {
		businessPublicMigration(t, f.f.pool, "106_business_public_update_notifications.down.sql", true)
		if after := businessPublicSnapshot(t, f); after != used {
			t.Fatal("used106 refusal changed source/effects/old rows or catalog")
		}
	})
}
