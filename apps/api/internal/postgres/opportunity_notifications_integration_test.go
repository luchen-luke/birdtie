package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

// Native domain writers, durable decisions and Inbox, not a fake match result.
func TestSocialNotificationsNativeOpportunityProducer(t *testing.T) {
	for _, route := range []agentnotification.Route{agentnotification.Immediate, agentnotification.Normal, agentnotification.Digest, agentnotification.Silent, agentnotification.Block} {
		t.Run(string(route), func(t *testing.T) {
			pool := newPeopleTestPool(t)
			f := newPeoplePair(t, pool)
			f.scopes()
			_, digest, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[0], digest[:])
			t.Cleanup(func() { f.exec(`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, f.ids) })
			var agent string
			if e = pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active') RETURNING id`, f.ids[0]).Scan(&agent); e != nil {
				t.Fatal(e)
			}
			principal, e := actorref.ParsePrincipal("person", f.ids[0])
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.EnsureAgentProfile(f.ctx, agent, principal); e != nil {
				t.Fatal(e)
			}
			var now time.Time
			if e = pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				t.Fatal(e)
			}
			access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
			policy, e := f.store.PutOwnNotificationPolicy(f.ctx, access, agentnotification.PutInput{Enabled: true, DefaultRoute: route, Rules: []agentnotification.Rule{}, ExpiresAt: now.Add(time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				f.exec(`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, f.ids)
				f.exec(`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, f.ids)
				f.exec(`DELETE FROM activities WHERE host_account_id=ANY($1::uuid[])`, f.ids)
			})
			activity := socialNotificationsActivity(t, f, 1, 0)
			intent := socialNotificationsIntent(t, f, 0, true)
			if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND kind='opportunity_available'`, f.ids[0]); n != 0 {
				t.Fatal("draft emitted opportunity", n)
			}
			if _, e = f.store.ActivateSocialIntent(f.ctx, f.ids[0], intent.ID); e != nil {
				t.Fatal(e)
			}
			var disposition, event, source string
			var version uint64
			if e = pool.QueryRow(f.ctx, `SELECT disposition,event_version,source_version,policy_version FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available'`, f.ids[0], activity.ID).Scan(&disposition, &event, &source, &version); e != nil {
				t.Fatal(e)
			}
			if disposition != string(route) || event != "activity:2" || len(source) != 64 || version != policy.Version {
				t.Fatalf("native decision %s/%s/%d", disposition, event, version)
			}
			expected := 0
			if route == agentnotification.Immediate || route == agentnotification.Normal {
				expected = 1
			}
			if n := f.count(`SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='opportunity_available' AND target_activity_id=$2`, f.ids[0], activity.ID); n != expected {
				t.Fatalf("route Inbox=%d want %d", n, expected)
			}
			for i := 0; i < 100; i++ {
				tx, e := pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if e != nil {
					t.Fatal(e)
				}
				if e = routeOpportunityIntentOwner(f.ctx, tx, f.ids[0]); e != nil {
					_ = tx.Rollback(context.Background())
					t.Fatal(e)
				}
				if e = tx.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
			}
			if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND kind='opportunity_available'`, f.ids[0]); n != 1 {
				t.Fatalf("100 retries duplicated=%d", n)
			}
			second := socialNotificationsIntent(t, f, 0, true)
			if _, e = f.store.ActivateSocialIntent(f.ctx, f.ids[0], second.ID); e != nil {
				t.Fatal(e)
			}
			if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND kind='opportunity_available'`, f.ids[0]); n != 1 {
				t.Fatalf("second matching intent duplicated=%d", n)
			}
			// Source invalidation is independent from event identity and policy CAS.
			if _, e = f.store.CancelSocialIntent(f.ctx, f.ids[0], intent.ID); e != nil {
				t.Fatal(e)
			}
			items, e := f.store.ListHumanInbox(f.ctx, digest, identity.Actor{ID: f.ids[0], AccountType: "person"})
			if e != nil {
				t.Fatal(e)
			}
			for _, item := range items {
				if item.ResourceType == "opportunity_available" {
					t.Fatal("old source survived selected Intent replacement")
				}
			}
			// Closing custom policy restores ordinary routing for the next actual
			// Activity revision; it never replays this revision's blocked/digest event.
			input := agentnotification.PutInput{ExpectedVersion: policy.Version, Enabled: false, DefaultRoute: route, Rules: []agentnotification.Rule{}, ExpiresAt: now.Add(time.Hour)}
			if _, e = f.store.PutOwnNotificationPolicy(f.ctx, access, input); e != nil {
				t.Fatal(e)
			}
			if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND kind='opportunity_available'`, f.ids[0]); n != 1 {
				t.Fatal("policy change manufactured event")
			}
			start := time.Now().UTC().Add(25 * time.Hour).Truncate(time.Second)
			if _, e = f.store.UpdateSocialActivity(f.ctx, f.ids[1], activity.ID, activitypublish.Input{CityID: f.cities[0], PlaceID: f.places[0], Title: "合成活动明确改期", Summary: "Owned native revision", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed"}); e != nil {
				t.Fatal(e)
			}
			var nextRoute string
			if e = pool.QueryRow(f.ctx, `SELECT disposition FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available' AND event_version='activity:3'`, f.ids[0], activity.ID).Scan(&nextRoute); e != nil || nextRoute != "NORMAL" {
				t.Fatalf("disabled future actual revision=%s %v", nextRoute, e)
			}
		})
	}
}

func TestSocialNotificationsNativeNoAgentPublicationAndSourceBoundaries(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	f.scopes()
	t.Cleanup(func() {
		f.exec(`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, f.ids)
		f.exec(`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, f.ids)
		f.exec(`DELETE FROM activities WHERE host_account_id=ANY($1::uuid[])`, f.ids)
	})
	intent := socialNotificationsIntent(t, f, 0, true)
	if _, e := f.store.ActivateSocialIntent(f.ctx, f.ids[0], intent.ID); e != nil {
		t.Fatal(e)
	}
	a := socialNotificationsActivity(t, f, 1, 0)
	if n := f.count(`SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='opportunity_available' AND target_activity_id=$2`, f.ids[0], a.ID); n != 1 {
		t.Fatal("actual publication without Agent did not notify", n)
	}
	if n := f.count(`SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.ids[0]); n != 0 {
		t.Fatal("ordinary notification manufactured Agent", n)
	}

	// Ordinary current human Inbox has no PersonalAgent/Profile prerequisite.
	_, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[0], digest[:])
	t.Cleanup(func() { f.exec(`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, f.ids) })
	actor := identity.Actor{ID: f.ids[0], AccountType: "person"}
	items, err := f.store.ListHumanInbox(f.ctx, digest, actor)
	if err != nil || len(items) != 1 || items[0].TargetActivityID == nil || *items[0].TargetActivityID != a.ID {
		t.Fatalf("ordinary no-Agent current Inbox=%v %v", items, err)
	}
	read, err := f.store.ReadHumanInboxItem(f.ctx, digest, actor, items[0].ID)
	if err != nil || read.ReadAt == nil || read.TargetActivityID == nil || *read.TargetActivityID != a.ID {
		t.Fatalf("ordinary no-Agent current read=%v %v", read, err)
	}
	t.Run("nativeTransactionRollback", func(t *testing.T) {
		// Exercise the real same-transaction producer with a native Activity
		// revision and audit. Explicit rollback proves atomic ledger ownership;
		// it is not a fake provider failure or a public HTTP failure injection.
		var original string
		if err = pool.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, a.ID).Scan(&original); err != nil {
			t.Fatal(err)
		}
		tx, e := pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(f.ctx, `UPDATE activities SET title='Owned rollback revision',revision=revision+1 WHERE id=$1`, a.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(f.ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'owned_notification_rollback','activity',$2,'allowed','local_native_contract')`, f.ids[1], a.ID); e != nil {
			t.Fatal(e)
		}
		if e = routeOpportunityActivity(f.ctx, tx, a.ID); e != nil {
			t.Fatal(e)
		}
		var decisions, inbox int
		if e = tx.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available'),(SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND target_activity_id=$2 AND resource_type='opportunity_available')`, f.ids[0], a.ID).Scan(&decisions, &inbox); e != nil || decisions != 2 || inbox != 2 {
			t.Fatalf("real uncommitted effects=%d/%d %v", decisions, inbox, e)
		}
		if e = tx.Rollback(f.ctx); e != nil {
			t.Fatal(e)
		}
		var current string
		if e = pool.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, a.ID).Scan(&current); e != nil || current != original {
			t.Fatalf("rollback source changed %v", e)
		}
		if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available'`, f.ids[0], a.ID); n != 1 {
			t.Fatal("rollback retained decision", n)
		}
		if n := f.count(`SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND target_activity_id=$2 AND resource_type='opportunity_available'`, f.ids[0], a.ID); n != 1 {
			t.Fatal("rollback retained Inbox", n)
		}
		if n := f.count(`SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='owned_notification_rollback'`, f.ids[1]); n != 0 {
			t.Fatal("rollback retained audit", n)
		}
	})
	t.Run("concurrentCurrentReplay", func(t *testing.T) {
		// Eight actual independent RC transactions race the same current event;
		// this complements the five-route 100 serial replay contracts above.
		start := make(chan struct{})
		results := make(chan error, 8)
		for i := 0; i < 8; i++ {
			go func() {
				<-start
				tx, e := pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if e != nil {
					results <- e
					return
				}
				defer tx.Rollback(context.Background())
				if e = routeOpportunityActivity(f.ctx, tx, a.ID); e == nil {
					e = tx.Commit(f.ctx)
				}
				results <- e
			}()
		}
		close(start)
		for i := 0; i < 8; i++ {
			if e := <-results; e != nil {
				t.Error(e)
			}
		}
		if t.Failed() {
			return
		}
		if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available'`, f.ids[0], a.ID); n != 1 {
			t.Fatal("concurrent replay duplicated decision", n)
		}
		if n := f.count(`SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND target_activity_id=$2 AND resource_type='opportunity_available'`, f.ids[0], a.ID); n != 1 {
			t.Fatal("concurrent replay duplicated Inbox", n)
		}
	})
	sourceCount := func() int {
		return f.count(`SELECT count(*) FROM birdtie_native_notification_source('opportunity_available',$1,$2)`, a.ID, f.ids[0])
	}
	for _, mode := range []string{"block", "cityExpired", "placeUnpublished", "intentExpired", "unknownCity", "contextConflict", "cancelledActivity", "ownerSuspended"} {
		t.Run(mode, func(t *testing.T) {
			tx, e := pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			var q string
			var args []any
			switch mode {
			case "block":
				q = `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`
				args = []any{f.ids[0], f.ids[1]}
			case "cityExpired":
				q = `UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`
				args = []any{f.cities[0]}
			case "placeUnpublished":
				q = `UPDATE places SET publication_status='hidden' WHERE id=$1`
				args = []any{f.places[0]}
			case "intentExpired":
				q = `UPDATE social_intents SET expires_at=created_at+interval '1 microsecond' WHERE id=$1`
				args = []any{intent.ID}
			case "unknownCity":
				q = `UPDATE social_intents SET context_id=NULL WHERE id=$1`
				args = []any{intent.ID}
			case "contextConflict":
				q = `UPDATE social_intents SET context_id=$2 WHERE id=$1`
				args = []any{intent.ID, f.cityContexts[1]}
			case "cancelledActivity":
				q = `UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`
				args = []any{a.ID}
			case "ownerSuspended":
				q = `UPDATE accounts SET status='suspended' WHERE id=$1`
				args = []any{f.ids[0]}
			}
			if _, e = tx.Exec(f.ctx, q, args...); e != nil {
				t.Fatal(e)
			}
			var n int
			if e = tx.QueryRow(f.ctx, `SELECT count(*) FROM birdtie_native_notification_source('opportunity_available',$1,$2)`, a.ID, f.ids[0]).Scan(&n); e != nil || n != 0 {
				t.Fatalf("current %s source=%d %v", mode, n, e)
			}
			if e = tx.Rollback(f.ctx); e != nil {
				t.Fatal(e)
			}
			if n = sourceCount(); n != 1 {
				t.Fatalf("owned rollback lost valid source=%d", n)
			}
		})
	}
	// A valid different City source has no right to cross-match the same Place
	// text. A second native published Activity in the other City creates no row.
	other := socialNotificationsActivity(t, f, 1, 1)
	if n := f.count(`SELECT count(*) FROM native_notification_decisions WHERE recipient_id=$1 AND activity_id=$2 AND kind='opportunity_available'`, f.ids[0], other.ID); n != 0 {
		t.Fatal("cross City opportunity emitted", n)
	}
}

func socialNotificationsActivity(t *testing.T, f *newPeopleFixture, host, city int) activitypublish.Activity {
	t.Helper()
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	a, e := f.store.CreateSocialDraft(f.ctx, f.ids[host], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.ids[host]}, CityID: f.cities[city], PlaceID: f.places[city], Title: "合成强规则活动", Summary: "Owned source only", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed"})
	if e != nil {
		t.Fatal(e)
	}
	a, e = f.store.PublishSocialActivity(f.ctx, f.ids[host], a.ID)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func socialNotificationsIntent(t *testing.T, f *newPeopleFixture, city int, explicitCity bool) socialintent.Record {
	t.Helper()
	raw, e := json.Marshal(map[string]string{"placeId": f.places[city]})
	if e != nil {
		t.Fatal(e)
	}
	ctxID := ""
	if explicitCity {
		ctxID = f.cityContexts[city]
	}
	item, e := f.store.CreateSocialIntentDraft(f.ctx, f.ids[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成本人明确意图", Constraints: raw, Audience: "PRIVATE", ContextID: ctxID, Modality: "IN_PERSON", ExpiresAt: time.Now().UTC().Add(48 * time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	return item
}
