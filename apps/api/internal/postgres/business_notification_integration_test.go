package postgres

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
)

func businessNotificationOwner(t *testing.T, f *businessConsoleFixture, route agentnotification.Route) {
	t.Helper()
	var agent string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id)
 SELECT 'personal',$1 WHERE NOT EXISTS(SELECT 1 FROM agents WHERE agent_type='personal' AND principal_account_id=$1)
 RETURNING id`, f.people[0]).Scan(&agent); e != nil {
		if e = f.pool.QueryRow(f.ctx, `SELECT id FROM agents WHERE agent_type='personal' AND principal_account_id=$1 AND status='active'`, f.people[0]).Scan(&agent); e != nil {
			t.Fatal(e)
		}
	}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: f.people[0]}
	if _, e := f.store.EnsureAgentProfile(f.ctx, agent, owner); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	_, e := f.store.PutOwnNotificationPolicy(f.ctx, agentprofile.PrivateAccess{SessionDigest: f.access[0].SessionDigest, WorkspacePrincipal: owner}, agentnotification.PutInput{Enabled: true, DefaultRoute: route, Rules: []agentnotification.Rule{}, ExpiresAt: now.UTC().Add(time.Hour).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
}

func TestBusinessNotificationNativeConcurrentReplayAndRetainedDownGuard(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	f.grant(t, id)
	businessNotificationOwner(t, f, agentnotification.Normal)
	in := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "同一具体本地审核版本"}
	start := make(chan struct{})
	results := make(chan error, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), in)
			if e == nil && c.Version != 2 {
				e = errors.New("wrong original claim version")
			}
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent exact replay", e)
		}
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE business_claim_id=$1`, id).Scan(&count); e != nil || count != 1 {
		t.Fatal("concurrent duplicate effect", count, e)
	}
	raw, e := os.ReadFile("../../migrations/081_business_claim_notifications.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	if _, e = tx.Exec(f.ctx, string(raw)); e == nil {
		t.Fatal("retained notification rollback erased history")
	}
	if e = tx.Rollback(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE business_claim_id=$1`, id).Scan(&count); e != nil || count != 1 {
		t.Fatal("failed down changed retained source", count, e)
	}
}

func TestBusinessNotificationNativeFinalGrantExpiryAfterInboxWaitRollsBack(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	f.grant(t, id)
	businessNotificationOwner(t, f, agentnotification.Normal)
	f.exec(t, `UPDATE business_review_grants SET valid_until=clock_timestamp()+interval '2 seconds' WHERE business_id=$1`, id)
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(f.ctx)
	if _, e = lock.Exec(f.ctx, `LOCK TABLE inbox_items IN SHARE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "期限等待边界"})
		done <- e
	}()
	businessNativeWait(t, f, lock.Conn().PgConn().PID())
	// Real PostgreSQL clock advances while the original writer waits, without forged approval.
	f.exec(t, `SELECT pg_sleep(2.1)`)
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, businessconsole.ErrForbidden) {
			t.Fatal("expired reviewer authority committed", e)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("native wait did not finish")
	}
	var state string
	var version int64
	var decisions, audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT state,version FROM business_claim_controls WHERE business_id=$1`, id).Scan(&state, &version); e != nil || state != "pending" || version != 1 {
		t.Fatal("failed review changed original claim", state, version, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE business_claim_id=$1`, id).Scan(&decisions); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND action='claim_approve'`, id).Scan(&audits); e != nil || decisions != 0 || audits != 0 {
		t.Fatal("failure retained partial notification/audit", decisions, audits, e)
	}
}

func TestBusinessNotificationNativeReviewPoliciesAndOriginalCAS(t *testing.T) {
	for _, route := range []agentnotification.Route{agentnotification.Immediate, agentnotification.Normal, agentnotification.Digest, agentnotification.Silent, agentnotification.Block} {
		for _, decision := range []string{"approve", "reject", "revoke"} {
			t.Run(string(route)+"/"+decision, func(t *testing.T) {
				f := newBusinessConsoleFixture(t)
				id := f.newBusinessID(t)
				f.claim(t, id)
				f.grant(t, id)
				businessNotificationOwner(t, f, route)
				version := int64(1)
				if decision == "revoke" {
					if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成本地首次审核"}); e != nil {
						t.Fatal(e)
					}
					version = 2
				}
				in := businessconsole.ReviewInput{ExpectedVersion: version, Decision: decision, Note: "合成审核结果，非真实商家证明"}
				for i := 0; i < 100; i++ {
					c, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), in)
					if e != nil || c.Version != version+1 {
						t.Fatal("native original CAS replay", c.Version, e)
					}
				}
				var decisions, audits, inboxRows int
				var cat, gotRoute string
				var priority int
				if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE business_claim_id=$1 AND event_version='claim:'||$2::bigint::text`, id, version+1).Scan(&decisions); e != nil {
					t.Fatal(e)
				}
				if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND action=$2 AND resource_version=$3`, id, "claim_"+decision, version+1).Scan(&audits); e != nil {
					t.Fatal(e)
				}
				if e := f.pool.QueryRow(f.ctx, `SELECT category,disposition,priority FROM native_notification_decisions WHERE business_claim_id=$1 AND event_version='claim:'||$2::bigint::text`, id, version+1).Scan(&cat, &gotRoute, &priority); e != nil {
					t.Fatal(e)
				}
				if decisions != 1 || audits != 1 || cat != "BUSINESS" || gotRoute != string(route) {
					t.Fatal("true native policy/current source identity", decisions, audits, cat, gotRoute)
				}
				wantPriority := map[agentnotification.Route]int{agentnotification.Immediate: 100, agentnotification.Normal: 50, agentnotification.Digest: 10, agentnotification.Silent: 0, agentnotification.Block: 0}[route]
				if priority != wantPriority {
					t.Fatal("priority bypassed AttentionPolicy")
				}
				if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.business_claim_id=$1 AND d.event_version='claim:'||$2::bigint::text`, id, version+1).Scan(&inboxRows); e != nil {
					t.Fatal(e)
				}
				wantInbox := 0
				if route == agentnotification.Immediate || route == agentnotification.Normal {
					wantInbox = 1
				}
				if inboxRows != wantInbox {
					t.Fatal("pending digest/silent/block falsely delivered", inboxRows)
				}
				items, e := f.store.ListInbox(f.ctx, f.people[0])
				if e != nil {
					t.Fatal(e)
				}
				found := 0
				for _, item := range items {
					if item.ResourceType == "business_claim_review" {
						found++
						if item.TargetBusinessID == nil || *item.TargetBusinessID != id || item.ResourceID == id || item.SemanticCategory != "BUSINESS" {
							t.Fatal("decision used as business target", item)
						}
					}
				}
				if found != wantInbox {
					t.Fatal("current original review source visibility", found, wantInbox)
				}
			})
		}
	}
}

func TestBusinessNotificationNativeCurrentSourceAndRecipientABA(t *testing.T) {
	for _, change := range []string{"recipient_removed", "recipient_suspended", "business_suspended", "principal_suspended", "reviewer_suspended", "grant_expired", "grant_revoke_restore", "blocked"} {
		t.Run(change, func(t *testing.T) {
			f := newBusinessConsoleFixture(t)
			id := f.newBusinessID(t)
			f.claim(t, id)
			f.grant(t, id)
			businessNotificationOwner(t, f, agentnotification.Normal)
			if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成核验"}); e != nil {
				t.Fatal(e)
			}
			switch change {
			case "recipient_removed":
				f.exec(t, `UPDATE business_memberships SET role='member' WHERE business_id=$1 AND user_account_id=$2`, id, f.people[0])
			case "recipient_suspended":
				f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.people[0])
			case "business_suspended":
				f.exec(t, `UPDATE businesses SET status='hidden' WHERE id=$1`, id)
			case "principal_suspended":
				f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=(SELECT account_id FROM businesses WHERE id=$1)`, id)
			case "reviewer_suspended":
				f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.people[1])
			case "grant_expired":
				f.exec(t, `UPDATE business_review_grants SET valid_from=clock_timestamp()-interval '2 hour',valid_until=clock_timestamp()-interval '1 hour' WHERE business_id=$1`, id)
			case "grant_revoke_restore":
				f.exec(t, `UPDATE business_review_grants SET state='revoked' WHERE business_id=$1`, id)
				f.exec(t, `UPDATE business_review_grants SET state='active' WHERE business_id=$1`, id)
			case "blocked":
				f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.people[1], f.people[0])
				t.Cleanup(func() {
					f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.people[1], f.people[0])
				})
			}
			var count int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions d WHERE d.business_claim_id=$1 AND birdtie_native_notification_visible(d)`, id).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if count != 0 {
				t.Fatal("stale native recipient/source disclosed", change, count)
			}
		})
	}
}
