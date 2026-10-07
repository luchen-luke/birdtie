package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests use real native writers and PostgreSQL, with exclusively owned
// disposable identities and sources. They do not prove push, digest release,
// a provider, a production identity or a deployed reminder scheduler.
type notificationDeliveryFixture struct {
	private                                                              *agentPrivateFixture
	activities, communities, activityCandidates, placeCandidates, places []string
}

func newNotificationDeliveryFixture(t *testing.T) *notificationDeliveryFixture {
	t.Helper()
	f := &notificationDeliveryFixture{private: nativeNotificationFixture(t)}
	b := f.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Run before the inherited Session/identity cleanup. Every predicate is
		// restricted to random accounts or explicit source IDs owned here.
		for _, step := range []struct {
			sql string
			ids []string
		}{
			{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[]) OR organization_id IN(SELECT id FROM organizations WHERE account_id=ANY($1::uuid[]))`, b.accounts},
			{`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM conversation_messages WHERE conversation_id IN(SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, b.accounts},
			{`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM activity_sources WHERE candidate_id=ANY($1::uuid[])`, f.activityCandidates},
			{`DELETE FROM activity_candidates WHERE id=ANY($1::uuid[])`, f.activityCandidates},
			{`DELETE FROM city_seed_activities WHERE activity_id=ANY($1::uuid[])`, f.activities},
			{`DELETE FROM activities WHERE id=ANY($1::uuid[])`, f.activities},
			{`DELETE FROM communities WHERE id=ANY($1::uuid[])`, f.communities},
			{`DELETE FROM place_sources WHERE candidate_id=ANY($1::uuid[])`, f.placeCandidates},
			{`DELETE FROM place_candidates WHERE id=ANY($1::uuid[])`, f.placeCandidates},
			{`DELETE FROM places WHERE id=ANY($1::uuid[])`, f.places},
		} {
			if _, err := b.pool.Exec(ctx, step.sql, step.ids); err != nil {
				t.Errorf("owned native notification cleanup failed: %v", err)
			}
		}
		var remaining int
		if err := b.pool.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))+
		 (SELECT count(*) FROM activities WHERE id=ANY($2::uuid[]))+
		 (SELECT count(*) FROM communities WHERE id=ANY($3::uuid[]))`, b.accounts, f.activities, f.communities).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("owned native notification cleanup residue=%d error=%v", remaining, err)
		} else {
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY native notification source/ledger/Inbox cleanup residue=0")
		}
	})
	return f
}

func (f *notificationDeliveryFixture) policy(t *testing.T, access agentprofile.PrivateAccess, route agentnotification.Route) agentnotification.Policy {
	t.Helper()
	b := f.private.base
	current, err := b.store.GetOwnNotificationPolicy(b.ctx, access)
	if err != nil {
		t.Fatal("read actual notification policy", err)
	}
	input := nativeNotificationInput(t, f.private, route)
	input.ExpectedVersion = current.Version
	p, err := b.store.PutOwnNotificationPolicy(b.ctx, access, input)
	if err != nil {
		t.Fatal("save actual notification policy", err)
	}
	return p
}

type notificationDeliveryMetadata struct {
	DecisionID, InboxID, SourceVersion, EventVersion, Reason string
	PolicyVersion                                            uint64
	Category                                                 string
	Route                                                    agentnotification.Route
	Priority                                                 int
}

func notificationDeliveryProtectedSnapshot(t *testing.T, f *notificationDeliveryFixture) string {
	t.Helper()
	b := f.private.base
	var memories string
	if err := b.pool.QueryRow(b.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY id),'[]'::jsonb)::text FROM agent_memories m WHERE owner_id=ANY($1::uuid[])`, b.accounts).Scan(&memories); err != nil {
		t.Fatal("native Memory preservation snapshot unavailable", err)
	}
	return agentMemoryOwnedSourceSnapshot(t, f.private) + "\n" + memories
}

func (f *notificationDeliveryFixture) delivered(t *testing.T, kind agentnotification.Kind, sourceID, recipientID string, route agentnotification.Route, inboxCount int) notificationDeliveryMetadata {
	t.Helper()
	b := f.private.base
	var count, physicalInbox int
	if err := b.pool.QueryRow(b.ctx, `SELECT count(*),count(i.id) FROM native_notification_decisions d
	 LEFT JOIN inbox_items i ON i.routing_decision_id=d.id WHERE d.kind=$1 AND d.source_id=$2 AND d.recipient_id=$3`, string(kind), sourceID, recipientID).Scan(&count, &physicalInbox); err != nil || count != 1 || physicalInbox != inboxCount {
		t.Fatalf("native kind=%s decision=%d Inbox=%d expected=1/%d error=%v", kind, count, physicalInbox, inboxCount, err)
	}
	var result notificationDeliveryMetadata
	if err := b.pool.QueryRow(b.ctx, `SELECT d.id,COALESCE(i.id::text,''),d.disposition,d.priority,d.category,
	 d.policy_version,d.source_version,d.event_version,d.reason FROM native_notification_decisions d
	 LEFT JOIN inbox_items i ON i.routing_decision_id=d.id WHERE d.kind=$1 AND d.source_id=$2 AND d.recipient_id=$3`, string(kind), sourceID, recipientID).Scan(
		&result.DecisionID, &result.InboxID, &result.Route, &result.Priority, &result.Category, &result.PolicyVersion, &result.SourceVersion, &result.EventVersion, &result.Reason); err != nil {
		t.Fatal(err)
	}
	descriptor, err := agentnotification.LookupKind(kind)
	priority, _ := agentnotification.RoutePriority(route)
	if err != nil || result.Route != route || result.Priority != priority || result.Category != string(descriptor.Category) || len(result.SourceVersion) != 64 || result.EventVersion == "" {
		t.Fatal("native metadata classification mismatch")
	}
	expectedDetail := descriptor.Detail
	if kind == agentnotification.KindActivityReview || kind == agentnotification.KindPlaceReview {
		var nativeStatus string
		query := `SELECT status FROM activity_candidates WHERE id=$1`
		if kind == agentnotification.KindPlaceReview {
			query = `SELECT status FROM place_candidates WHERE id=$1`
		}
		if err = b.pool.QueryRow(b.ctx, query, sourceID).Scan(&nativeStatus); err != nil {
			t.Fatal("read actual review source status", err)
		}
		if kind == agentnotification.KindActivityReview {
			switch nativeStatus {
			case "published":
				expectedDetail = "你的活动建议已发布，请查看记录。"
			case "rejected":
				expectedDetail = "你的活动建议未通过审核，请查看记录。"
			default:
				t.Fatal("unexpected actual activity review source status")
			}
		} else {
			switch nativeStatus {
			case "published":
				expectedDetail = "你的地点建议已发布，请查看记录。"
			case "linked_duplicate":
				expectedDetail = "你的地点建议已关联现有地点，请查看记录。"
			case "rejected":
				expectedDetail = "你的地点建议未通过审核，请查看记录。"
			default:
				t.Fatal("unexpected actual place review source status")
			}
		}
	}
	items, err := b.store.ListInbox(b.ctx, recipientID)
	if err != nil {
		t.Fatal("list current native Inbox", err)
	}
	found := 0
	for _, item := range items {
		if item.ID != result.InboxID {
			continue
		}
		found++
		if item.Title != descriptor.Title || item.Detail != expectedDetail || item.Category != descriptor.LegacyCategory || item.SemanticCategory != string(descriptor.Category) || item.NotificationRoute != string(route) || item.NotificationPriority != priority {
			t.Fatal("native Inbox reused payload or mismatched fixed registry")
		}
		assertNotificationDeliveryNoPayload(t, item)
		read, err := b.store.ReadInboxItem(b.ctx, recipientID, item.ID)
		if err != nil || read.ID != item.ID || read.ReadAt == nil {
			t.Fatal("actual native Inbox read failed", err)
		}
		assertNotificationDeliveryNoPayload(t, read)
	}
	if found != inboxCount {
		t.Fatalf("native visible Inbox=%d expected=%d", found, inboxCount)
	}
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY native_kind=%s route=%s priority=%d ledger=1 Inbox=%d", kind, route, priority, inboxCount)
	return result
}

func assertNotificationDeliveryNoPayload(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"NOTIFICATION_PRIVATE_BODY", "NOTIFICATION_PRIVATE_QUERY", "NOTIFICATION_PRIVATE_TITLE", "structuredValue", "agentNotes", "token_sha256"} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("native notification leaked private or source payload")
		}
	}
}

func (f *notificationDeliveryFixture) friend(t *testing.T) (connection.Conversation, string) {
	t.Helper()
	b := f.private.base
	req, err := b.store.CreateFriendRequest(b.ctx, b.person.ID, b.other.ID, "合成明确联系申请")
	if err != nil {
		t.Fatal("native friend request", err)
	}
	f.delivered(t, agentnotification.KindConnectionRequest, req.ID, b.other.ID, agentnotification.Normal, 1)
	if _, err = b.store.DecideRequest(b.ctx, b.other.ID, req.ID, "accept"); err != nil {
		t.Fatal("native friend acceptance", err)
	}
	f.delivered(t, agentnotification.KindConnectionDecision, req.ID, b.person.ID, agentnotification.Normal, 1)
	ties, err := b.store.ListTies(b.ctx, b.person.ID)
	if err != nil || len(ties) != 1 {
		t.Fatal("actual accepted Tie missing", err)
	}
	cv, err := b.store.StartFriendConversation(b.ctx, b.person.ID, ties[0].ID)
	if err != nil {
		t.Fatal("native explicit friend conversation", err)
	}
	return cv, ties[0].ID
}

func (f *notificationDeliveryFixture) message(t *testing.T, cv connection.Conversation) connection.Message {
	t.Helper()
	b := f.private.base
	m, err := b.store.SendMessage(b.ctx, b.person.ID, cv.ID, "NOTIFICATION_PRIVATE_BODY 合成私密消息")
	if err != nil {
		t.Fatal("native message send", err)
	}
	return m
}

func TestNativeNotificationDeliveryFriendMessagesFiveRoutesIntegration(t *testing.T) {
	for _, route := range []agentnotification.Route{agentnotification.Normal, agentnotification.Immediate, agentnotification.Digest, agentnotification.Silent, agentnotification.Block} {
		t.Run(string(route), func(t *testing.T) {
			f := newNotificationDeliveryFixture(t)
			b := f.private.base
			original := notificationDeliveryProtectedSnapshot(t, f)
			cv, _ := f.friend(t)
			f.policy(t, f.private.peer, route)
			m := f.message(t, cv)
			count := 0
			if route == agentnotification.Normal || route == agentnotification.Immediate {
				count = 1
			}
			f.delivered(t, agentnotification.KindDirectMessage, m.ID, b.other.ID, route, count)
			// Replay of the same server-resolved native event is not a second
			// delivery, regardless of pending/suppressed disposition.
			for range 100 {
				tx, err := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				inserted, err := routeNativeNotification(b.ctx, tx, agentnotification.KindDirectMessage, m.ID, b.other.ID)
				if err != nil || inserted {
					_ = tx.Rollback(b.ctx)
					t.Fatal("native event replay was not idempotent", err)
				}
				if err = tx.Commit(b.ctx); err != nil {
					t.Fatal(err)
				}
			}
			f.delivered(t, agentnotification.KindDirectMessage, m.ID, b.other.ID, route, count)
			if notificationDeliveryProtectedSnapshot(t, f) != original {
				t.Fatal("native delivery changed Profile/Private/Memory authority")
			}
		})
	}
}

func TestNativeNotificationDeliverySourceAndPolicyRevocationIntegration(t *testing.T) {
	for _, scenario := range []string{"native_block", "removed_tie", "deleted_message", "inactive_sender", "inactive_recipient", "policy_version", "policy_expiry"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNotificationDeliveryFixture(t)
			b := f.private.base
			cv, tieID := f.friend(t)
			if scenario == "policy_expiry" {
				input := nativeNotificationInput(t, f.private, agentnotification.Normal)
				input.ExpiresAt = time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
				if _, err := b.store.PutOwnNotificationPolicy(b.ctx, f.private.peer, input); err != nil {
					t.Fatal(err)
				}
			}
			m := f.message(t, cv)
			proof := f.delivered(t, agentnotification.KindDirectMessage, m.ID, b.other.ID, agentnotification.Normal, 1)
			switch scenario {
			case "native_block":
				if err := b.store.BlockAccount(b.ctx, b.other.ID, b.person.ID); err != nil {
					t.Fatal(err)
				}
			case "removed_tie":
				if err := b.store.RemoveTie(b.ctx, b.other.ID, tieID); err != nil {
					t.Fatal(err)
				}
			case "deleted_message":
				b.exec(`DELETE FROM conversation_messages WHERE id=$1`, m.ID)
			case "inactive_sender":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			case "inactive_recipient":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.ID)
			case "policy_version":
				f.policy(t, f.private.peer, agentnotification.Block)
			case "policy_expiry":
				var expiry time.Time
				if err := b.pool.QueryRow(b.ctx, `SELECT expires_at FROM native_notification_policies WHERE owner_id=$1`, b.other.ID).Scan(&expiry); err != nil {
					t.Fatal(err)
				}
				// Wait to the actual absolute database deadline, not a guessed
				// delay. No shared resource/clock is changed.
				if _, err := b.pool.Exec(b.ctx, `SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())),0)+0.002)`, expiry); err != nil {
					t.Fatal(err)
				}
			}
			items, err := b.store.ListInbox(b.ctx, b.other.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.ID == proof.InboxID || item.TargetConversationID != nil && *item.TargetConversationID == cv.ID {
					t.Fatal("withdrawn notification target remains visible")
				}
			}
			item, err := b.store.ReadInboxItem(b.ctx, b.other.ID, proof.InboxID)
			if !errors.Is(err, inbox.ErrNotFound) || item.ID != "" || item.TargetConversationID != nil {
				t.Fatal("withdrawn read released a target", err)
			}
			if scenario == "policy_expiry" {
				later := f.message(t, cv)
				current := f.delivered(t, agentnotification.KindDirectMessage, later.ID, b.other.ID, agentnotification.Normal, 1)
				if current.Reason != "expired" {
					t.Fatal("expiry did not restore ordinary NORMAL for a new event")
				}
			}
		})
	}
}

func TestNativeNotificationDeliveryRejectsForgedSourceAndAudienceIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	cv, _ := f.friend(t)
	m := f.message(t, cv)
	f.delivered(t, agentnotification.KindDirectMessage, m.ID, b.other.ID, agentnotification.Normal, 1)
	var before string
	if err := b.pool.QueryRow(b.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.id),'[]'::jsonb)::text FROM native_notification_decisions d WHERE recipient_id=ANY($1::uuid[])`, b.accounts).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		kind      agentnotification.Kind
		recipient string
		want      error
	}{
		{"self_is_not_message_receiver", agentnotification.KindDirectMessage, b.person.ID, nil},
		{"organization_is_not_person_receiver", agentnotification.KindDirectMessage, b.org.ID, nil},
		{"business_is_not_person_receiver", agentnotification.KindDirectMessage, b.business.ID, nil},
		{"wrong_existing_source_kind", agentnotification.KindActivityCancelled, b.other.ID, nil},
		{"unknown_kind", agentnotification.Kind("model_selected_kind"), b.other.ID, agentnotification.ErrInvalid},
		{"unavailable_business_producer", agentnotification.KindBusinessUpdate, b.other.ID, agentnotification.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(b.ctx)
			inserted, err := routeNativeNotification(b.ctx, tx, test.kind, m.ID, test.recipient)
			if inserted || !errors.Is(err, test.want) {
				t.Fatal("forged source/receiver produced native notification", err)
			}
			if err = tx.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
	var after string
	if err := b.pool.QueryRow(b.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.id),'[]'::jsonb)::text FROM native_notification_decisions d WHERE recipient_id=ANY($1::uuid[])`, b.accounts).Scan(&after); err != nil || after != before {
		t.Fatal("forged route changed durable ledger", err)
	}
}

func (f *notificationDeliveryFixture) organization(t *testing.T) organization.Organization {
	t.Helper()
	b := f.private.base
	o, err := b.store.CreateOrganization(b.ctx, b.person.ID, organization.CreateInput{OrganizationType: "club", Name: "合成通知组织"})
	if err != nil {
		t.Fatal("actual organization create", err)
	}
	// The foundation cleanup owns all added principals through this slice.
	b.accounts = append(b.accounts, o.AccountID)
	return o
}

func TestNativeNotificationDeliveryOrganizationLifecycleIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	o := f.organization(t)
	m, err := b.store.InviteMember(b.ctx, b.person.ID, o.ID, b.other.ID, "member")
	if err != nil {
		t.Fatal("native organization invite", err)
	}
	f.delivered(t, agentnotification.KindOrganizationInvitation, m.ID, b.other.ID, agentnotification.Normal, 1)
	if _, err = b.store.AcceptInvitation(b.ctx, b.other.ID, m.ID); err != nil {
		t.Fatal("native organization accept", err)
	}
	f.delivered(t, agentnotification.KindOrganizationMembershipChange, m.ID, b.other.ID, agentnotification.Normal, 1)
	if _, err = b.store.ChangeMemberRole(b.ctx, b.person.ID, o.ID, m.ID, "moderator"); err != nil {
		t.Fatal("native organization role change", err)
	}
	var changes int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE kind='organization_membership_change' AND source_id=$1 AND recipient_id=$2`, m.ID, b.other.ID).Scan(&changes); err != nil || changes != 2 {
		t.Fatal("role change did not create a distinct native event", err)
	}
	if err = b.store.RevokeMember(b.ctx, b.person.ID, o.ID, m.ID); err != nil {
		t.Fatal("native organization revoke", err)
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE kind='organization_membership_change' AND source_id=$1 AND recipient_id=$2`, m.ID, b.other.ID).Scan(&changes); err != nil || changes != 3 {
		t.Fatal("revoke did not create native membership event", err)
	}
	items, err := b.store.ListInbox(b.ctx, b.other.ID)
	if err != nil {
		t.Fatal(err)
	}
	current := 0
	for _, item := range items {
		if item.SemanticCategory == "ORGANIZATION" {
			current++
			assertNotificationDeliveryNoPayload(t, item)
		}
	}
	if current != 1 {
		t.Fatalf("old organization states reused in Inbox count=%d", current)
	}
	if _, _, err = b.store.ResolveWorkspace(b.ctx, b.other.ID, o.ID); !errors.Is(err, organization.ErrForbidden) {
		t.Fatal("notification granted removed organization membership", err)
	}
	var actor string
	if err = b.pool.QueryRow(b.ctx, `SELECT actor_id FROM native_notification_decisions WHERE source_id=$1 AND kind='organization_membership_change' ORDER BY created_at DESC,id DESC LIMIT 1`, m.ID).Scan(&actor); err != nil || actor != o.AccountID {
		t.Fatal("organization resource origin was misreported", err)
	}
	var humanAudits int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM admin_audit_events WHERE organization_id=$1 AND resource_type='membership' AND actor_account_id=ANY($2::uuid[])`, o.ID, []string{b.person.ID, b.other.ID}).Scan(&humanAudits); err != nil || humanAudits != 4 {
		t.Fatal("real human operation audit missing", err)
	}
}

func TestNativeNotificationDeliveryAgentTaskMetadataIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	for _, test := range []struct {
		status string
		kind   agentnotification.Kind
	}{{agentworkspace.TaskCompleted, agentnotification.KindAgentTaskCompleted}, {agentworkspace.TaskFailed, agentnotification.KindAgentTaskFailed}} {
		t.Run(test.status, func(t *testing.T) {
			task, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "NOTIFICATION_PRIVATE_QUERY", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
			if err != nil {
				t.Fatal("save actual personal task", err)
			}
			task.Status = test.status
			if task, err = b.store.UpdateTask(b.ctx, task); err != nil {
				t.Fatal("actual terminal task update", err)
			}
			proof := f.delivered(t, test.kind, task.ID, b.person.ID, agentnotification.Normal, 1)
			if proof.EventVersion != test.status {
				t.Fatal("terminal event identity was not the native status")
			}
			var before, after string
			if err = b.pool.QueryRow(b.ctx, `SELECT jsonb_build_array(to_jsonb(t),t.xmin::text)::text FROM agent_tasks t WHERE id=$1`, task.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if _, err = b.store.UpdateTask(b.ctx, task); err != nil {
					t.Fatal("native terminal exact retry", err)
				}
			}
			pool, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = New(pool, false).UpdateTask(b.ctx, task)
			pool.Close()
			if err != nil {
				t.Fatal("native terminal retry after reconnect", err)
			}
			if err = b.pool.QueryRow(b.ctx, `SELECT jsonb_build_array(to_jsonb(t),t.xmin::text)::text FROM agent_tasks t WHERE id=$1`, task.ID).Scan(&after); err != nil || after != before {
				t.Fatal("exact terminal retry rewrote native source", err)
			}
			f.delivered(t, test.kind, task.ID, b.person.ID, agentnotification.Normal, 1)
			task.Conversation = []agentworkspace.Message{{Role: "user", Text: "NOTIFICATION_PRIVATE_QUERY ancillary conversation edit"}}
			if _, err = b.store.UpdateTask(b.ctx, task); err != nil {
				t.Fatal("native ancillary task edit", err)
			}
			var decisions, notices int
			if err = b.pool.QueryRow(b.ctx, `SELECT count(*),count(i.id) FROM native_notification_decisions d LEFT JOIN inbox_items i ON i.routing_decision_id=d.id WHERE d.kind=$1 AND d.source_id=$2`, string(test.kind), task.ID).Scan(&decisions, &notices); err != nil || decisions != 1 || notices != 1 {
				t.Fatal("ancillary edit re-pinged terminal event", err)
			}
			if item, err := b.store.ReadInboxItem(b.ctx, b.person.ID, proof.InboxID); !errors.Is(err, inbox.ErrNotFound) || item.ID != "" {
				t.Fatal("old task source fingerprint remained visible", err)
			}
		})
	}
}

func (f *notificationDeliveryFixture) activity(t *testing.T, organizer activitypublish.Organizer) (activitypublish.Activity, activitypublish.Input) {
	t.Helper()
	b := f.private.base
	input := activitypublish.Input{Organizer: organizer, CityID: "aberdeen-gb", Title: "NOTIFICATION_PRIVATE_TITLE", Summary: "合成通知活动", StartsAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second), EndsAt: time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second), TimeZone: "Europe/London", Modality: "online", PhysicalPlaceStatus: "not_applicable", Visibility: "public"}
	a, err := b.store.CreateSocialDraft(b.ctx, b.person.ID, input)
	if err != nil {
		t.Fatal("actual native activity draft", err)
	}
	f.activities = append(f.activities, a.ID)
	if a, err = b.store.PublishSocialActivity(b.ctx, b.person.ID, a.ID); err != nil {
		t.Fatal("actual native activity publish", err)
	}
	if _, changed, err := b.store.JoinActivity(b.ctx, b.other.ID, a.ID); err != nil || !changed {
		t.Fatal("actual RSVP", err)
	}
	return a, input
}

func TestNativeNotificationDeliveryActivityLifecycleAndOwnerChatIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	a, input := f.activity(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
	t.Run("starts_soon_current_native_router_only", func(t *testing.T) {
		// A real published/RSVP source exercises the production router scoped
		// to our own ID. Do not run the global scheduler against other fixtures.
		tx, err := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(b.ctx)
		inserted, err := routeNativeNotification(b.ctx, tx, agentnotification.KindActivityReminder, a.ID, b.other.ID)
		if err != nil || !inserted {
			t.Fatal("native starts-soon routing", err)
		}
		if err = tx.Commit(b.ctx); err != nil {
			t.Fatal(err)
		}
		f.delivered(t, agentnotification.KindActivityReminder, a.ID, b.other.ID, agentnotification.Normal, 1)
		t.Log("Reminder evidence is current-native source routing; global scheduler/deployment not run")
	})
	t.Run("owner_without_RSVP_sends_and_receives_activity_chat", func(t *testing.T) {
		var rsvps int
		if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2`, a.ID, b.person.ID).Scan(&rsvps); err != nil || rsvps != 0 {
			t.Fatal("fixture owner unexpectedly RSVP'd", err)
		}
		if _, err := b.store.JoinActivityChat(b.ctx, b.person.ID, a.ID); err != nil {
			t.Fatal("owner native chat join", err)
		}
		if _, err := b.store.JoinActivityChat(b.ctx, b.other.ID, a.ID); err != nil {
			t.Fatal("participant native chat join", err)
		}
		var clientID string
		if err := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&clientID); err != nil {
			t.Fatal(err)
		}
		m, err := b.store.SendActivityChatMessage(b.ctx, b.person.ID, a.ID, clientID, "NOTIFICATION_PRIVATE_BODY activity owner")
		if err != nil {
			t.Fatal("owner native activity message", err)
		}
		f.delivered(t, agentnotification.KindActivityMessage, m.ID, b.other.ID, agentnotification.Normal, 1)
		if err = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&clientID); err != nil {
			t.Fatal(err)
		}
		reply, err := b.store.SendActivityChatMessage(b.ctx, b.other.ID, a.ID, clientID, "NOTIFICATION_PRIVATE_BODY owner recipient")
		if err != nil {
			t.Fatal("participant native activity reply", err)
		}
		proof := f.delivered(t, agentnotification.KindActivityMessage, reply.ID, b.person.ID, agentnotification.Normal, 1)
		if err = b.store.RemoveActivityChatMessage(b.ctx, b.other.ID, a.ID, reply.ID); err != nil {
			t.Fatal("native remove own activity message", err)
		}
		if item, err := b.store.ReadInboxItem(b.ctx, b.person.ID, proof.InboxID); !errors.Is(err, inbox.ErrNotFound) || item.ID != "" {
			t.Fatal("removed activity message notification was readable", err)
		}
	})
	t.Run("reschedule_and_cancel_native_hooks", func(t *testing.T) {
		input.StartsAt = input.StartsAt.Add(15 * time.Minute)
		input.EndsAt = input.EndsAt.Add(15 * time.Minute)
		if _, err := b.store.UpdateSocialActivity(b.ctx, b.person.ID, a.ID, input); err != nil {
			t.Fatal("native reschedule", err)
		}
		f.delivered(t, agentnotification.KindActivityChange, a.ID, b.other.ID, agentnotification.Normal, 1)
		if _, err := b.store.CancelSocialActivity(b.ctx, b.person.ID, a.ID); err != nil {
			t.Fatal("native cancellation", err)
		}
		f.delivered(t, agentnotification.KindActivityCancelled, a.ID, b.other.ID, agentnotification.Normal, 1)
		items, err := b.store.ListInbox(b.ctx, b.other.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ResourceType == "activity_reminder" && item.TargetActivityID != nil && *item.TargetActivityID == a.ID {
				t.Fatal("cancelled activity retained starts-soon Inbox")
			}
		}
	})
}

func TestNativeNotificationDeliveryCommunityChatIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	c, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "合成通知社区", Summary: "仅隔离验收", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb"})
	if err != nil {
		t.Fatal(err)
	}
	f.communities = append(f.communities, c.ID)
	if _, err = b.store.JoinSocialCommunity(b.ctx, b.other.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	for _, person := range []string{b.person.ID, b.other.ID} {
		if _, err = b.store.JoinCommunityChat(b.ctx, person, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	var clientID string
	if err = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	m, err := b.store.SendCommunityChatMessage(b.ctx, b.person.ID, c.ID, clientID, "NOTIFICATION_PRIVATE_BODY community")
	if err != nil {
		t.Fatal("native community send", err)
	}
	proof := f.delivered(t, agentnotification.KindCommunityMessage, m.ID, b.other.ID, agentnotification.Normal, 1)
	if err = b.store.LeaveCommunityChat(b.ctx, b.other.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if item, err := b.store.ReadInboxItem(b.ctx, b.other.ID, proof.InboxID); !errors.Is(err, inbox.ErrNotFound) || item.ID != "" {
		t.Fatal("left community chat retained notification target", err)
	}
}

func TestNativeNotificationDeliveryCityReviewHooksIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES('aberdeen-gb',$1,'contributor'),('aberdeen-gb',$2,'reviewer')`, b.person.ID, b.other.ID)
	t.Run("place_review", func(t *testing.T) {
		c, err := b.store.Submit(b.ctx, b.person.ID, "aberdeen-gb", cityseed.SubmitInput{Name: "合成通知地点", CategoryCode: "social", Summary: "隔离建议", LocationPrecision: "none", SourceLabel: "合成来源", SourceURL: "https://example.invalid/notification-place", RightsNote: "仅本地开发验收", ExpiresAt: time.Now().UTC().Add(48 * time.Hour)})
		if err != nil {
			t.Fatal("actual place candidate submit", err)
		}
		f.placeCandidates = append(f.placeCandidates, c.ID)
		if _, err = b.store.Review(b.ctx, b.other.ID, c.ID, cityseed.ReviewInput{Decision: "reject", Note: "合成审核"}); err != nil {
			t.Fatal("actual place review", err)
		}
		f.delivered(t, agentnotification.KindPlaceReview, c.ID, b.person.ID, agentnotification.Normal, 1)
	})
	t.Run("activity_review", func(t *testing.T) {
		organizer := actorref.ActorRef{Type: actorref.Person, ID: b.person.ID}
		c, err := b.store.SubmitActivity(b.ctx, b.person.ID, "aberdeen-gb", cityseed.ActivityInput{Organizer: &organizer, Title: "NOTIFICATION_PRIVATE_TITLE", Summary: "合成建议", HostLabel: "独立合成公开标签", StartsAt: time.Now().UTC().Add(48 * time.Hour), EndsAt: time.Now().UTC().Add(49 * time.Hour), TimeZone: "Europe/London", SourceLabel: "合成来源", SourceURL: "https://example.invalid/notification-activity", RightsNote: "仅本地开发验收", ExpiresAt: time.Now().UTC().Add(72 * time.Hour)})
		if err != nil {
			t.Fatal("actual activity candidate submit", err)
		}
		f.activityCandidates = append(f.activityCandidates, c.ID)
		if _, err = b.store.ReviewActivity(b.ctx, b.other.ID, c.ID, cityseed.ActivityReviewInput{Decision: "reject", Note: "合成审核"}); err != nil {
			t.Fatal("actual activity review", err)
		}
		f.delivered(t, agentnotification.KindActivityReview, c.ID, b.person.ID, agentnotification.Normal, 1)
	})
}
func TestNativeNotificationDeliveryWritersWithRepeatableReadPoolIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(b.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var isolation string
	if err = pool.QueryRow(b.ctx, `SHOW default_transaction_isolation`).Scan(&isolation); err != nil || isolation != "repeatable read" {
		t.Fatal("real pool default isolation was not repeatable read", err)
	}
	originalStore := b.store
	b.store = New(pool, false)
	defer func() { b.store = originalStore }()

	// These are genuine native operations under a pool whose default is RR.
	// Each notification-producing writer must select READ COMMITTED itself.
	cv, _ := f.friend(t)
	m := f.message(t, cv)
	f.delivered(t, agentnotification.KindDirectMessage, m.ID, b.other.ID, agentnotification.Normal, 1)
	o := f.organization(t)
	member, err := b.store.InviteMember(b.ctx, b.person.ID, o.ID, b.other.ID, "member")
	if err != nil {
		t.Fatal("native organization invite under RR pool default", err)
	}
	f.delivered(t, agentnotification.KindOrganizationInvitation, member.ID, b.other.ID, agentnotification.Normal, 1)
	task, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "NOTIFICATION_PRIVATE_QUERY RR pool", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal("native task save under RR pool default", err)
	}
	task.Status = agentworkspace.TaskCompleted
	if task, err = b.store.UpdateTask(b.ctx, task); err != nil {
		t.Fatal("native terminal update under RR pool default", err)
	}
	f.delivered(t, agentnotification.KindAgentTaskCompleted, task.ID, b.person.ID, agentnotification.Normal, 1)

	// The direct primitive refuses an explicit RR transaction before any
	// ledger/Inbox mutation. This tests its guard, not an injected stale ACL.
	snapshot := func() string {
		var raw string
		if err := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
		 'decisions',(SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.id),'[]'::jsonb) FROM native_notification_decisions d WHERE recipient_id=ANY($1::uuid[])),
		 'inbox',(SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.id),'[]'::jsonb) FROM inbox_items i WHERE recipient_account_id=ANY($1::uuid[])))::text`, b.accounts).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	tx, err := pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(b.ctx)
	inserted, err := routeNativeNotification(b.ctx, tx, agentnotification.KindDirectMessage, m.ID, b.other.ID)
	if inserted || !errors.Is(err, agentnotification.ErrUnavailable) {
		t.Fatal("explicit RR routing primitive was not denied", err)
	}
	if err = tx.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("explicit RR routing changed ledger or Inbox")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY native request/decision/message/organization invitation/terminal task pass with actual RR pool default; direct RR route refused with zero ledger/Inbox writes; no stale-permission race injected")
}
