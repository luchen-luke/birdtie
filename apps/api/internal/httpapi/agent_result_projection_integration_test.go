package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/connection"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resultHTTPReply(t *testing.T, w *httptest.ResponseRecorder) agentworkspace.Results {
	t.Helper()
	var r struct {
		Data agentworkspace.Results `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Data.Task == nil {
		t.Fatal("native registered reply", w.Body.String())
	}
	return r.Data
}
func resultHTTPFind(t *testing.T, r agentworkspace.Results, kind, id string) arp.Item {
	t.Helper()
	for _, item := range r.ResultSet.Items {
		if item.Entity == (arp.Ref{Type: kind, ID: id}) {
			return item
		}
	}
	t.Fatalf("missing original ref %s:%s %s", kind, id, r.Message)
	return arp.Item{}
}

func TestAgentResultProjectionHTTPUnsupportedOpportunityFilterIsExplicit(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"我的社交机会今晚"}`, f.tokens[0], 200, nil)
	r := resultHTTPReply(t, w)
	if r.Task.Status != agentworkspace.TaskFailed || len(r.ResultSet.Items) != 0 || !strings.Contains(r.Message, "暂不支持额外筛选") {
		t.Fatal("unsupported filter was presented as applied", w.Body.String())
	}
}
func TestAgentResultProjectionHTTPRegisteredNativeOriginalRefsAndRestore(t *testing.T) {
	for _, kind := range []string{"person", "activity", "place", "community", "organization", "opportunity"} {
		t.Run(kind, func(t *testing.T) {
			f := contextBuilderHTTPNative(t)
			id := f.public
			query := "帮我找羽毛球活动"
			switch kind {
			case "place":
				id = f.place
				query = "找地点"
			case "person":
				id = f.accountIDs[1]
				query = "找公开成员"
				i, e := f.store.SubmitIntent(f.ctx, id, f.city, intent.Input{Confirmed: true, Topic: "合成公开交流", AvailableFrom: time.Now().Add(-time.Minute), AvailableUntil: time.Now().Add(time.Hour), TimeZone: "Europe/London", CoarseAreaLabel: "本人公开范围", ExpiresAt: time.Now().Add(time.Hour)})
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM intents WHERE id=$1`, i.ID) })
			case "community":
				query = "找社区"
				c, e := f.store.CreateSocialCommunity(f.ctx, f.accountIDs[1], community.SocialInput{Name: "合成原生社区", Summary: "本人公开", CityID: f.city, Visibility: "public", JoinPolicy: "open"})
				if e != nil {
					t.Fatal(e)
				}
				id = c.ID
				t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM communities WHERE id=$1`, id) })
			case "organization":
				query = "找组织"
				if e := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&id); e != nil {
					t.Fatal(e)
				}
				f.exec(`UPDATE organizations SET visibility='public',verification_status='verified' WHERE id=$1`, id)
				f.exec(`INSERT INTO organization_map_locations(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by,reviewed_by,reviewed_at) VALUES($1,$2,0,0,'public','approved',$3,$4,clock_timestamp())`, id, f.city, f.accountIDs[0], f.accountIDs[1])
				t.Cleanup(func() {
					f.pool.Exec(context.Background(), `DELETE FROM organization_map_locations WHERE organization_id=$1`, id)
				})
			case "opportunity":
				query = "我的社交机会"
				raw, _ := json.Marshal(socialintent.Constraints{Category: "badminton", PlaceID: f.place})
				i, e := f.store.CreateSocialIntentDraft(f.ctx, f.accountIDs[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "本人私密意图正文不得公开", Constraints: raw, Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(time.Hour)})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.ActivateSocialIntent(f.ctx, f.accountIDs[0], i.ID); e != nil {
					t.Fatal(e)
				}
				id = i.ID + ":" + f.private
				t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM social_intents WHERE id=$1`, i.ID) })
			}
			f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
			body, _ := json.Marshal(map[string]string{"query": query})
			w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", string(body), f.tokens[0], 200, nil)
			r := resultHTTPReply(t, w)
			item := resultHTTPFind(t, r, kind, id)
			if item.Detail == nil || item.Share == nil || *item.Detail != *item.Share || len(r.People)+len(r.Groups)+len(r.Organizations) != 0 {
				t.Fatal("one typed truth/refs", r)
			}
			for _, activity := range r.Activities {
				original := resultHTTPFind(t, r, "activity", activity.ID)
				if activity.Title != original.Title || activity.StartsAt.IsZero() || activity.EndsAt.IsZero() || activity.TimeZone == "" {
					t.Fatal("compatibility activity was fabricated", activity)
				}
			}
			for _, place := range r.Places {
				original := resultHTTPFind(t, r, "place", place.ID)
				if place.Name != original.Title {
					t.Fatal("compatibility place diverged", place)
				}
			}
			if kind == "opportunity" {
				if item.Scope != arp.SelfPrivate || item.Detail.ID != f.private || item.Detail.Type != "activity" {
					t.Fatal("private candidate must open original activity", item)
				}
			} else if item.Detail.ID != id || item.Detail.Type != kind {
				t.Fatal("typed detail replaced original id", item)
			}
			resultHTTPExplicitDomainActions(t, f.privateProfileHTTPDBFixture, item)
			restored := resultHTTPReply(t, f.request(t, f.handler, "GET", "/v1/me/agent-tasks/"+r.Task.ID, "", f.tokens[0], 200, nil))
			resultHTTPFind(t, restored, kind, id)
			f.request(t, f.handler, "GET", "/v1/me/agent-tasks/"+r.Task.ID, "", f.tokens[1], 404, nil)
			typedWire, _ := json.Marshal(r.ResultSet.Items)
			if strings.Contains(string(typedWire), "UNREQUESTED_CONTEXT_BODY_CANARY") {
				t.Fatal("typed summary projected unrequested activity body")
			}
			// The original authorized human Activity DTO retains its complete
			// description; this is not a machine-purpose public context read.
			for _, private := range []string{"本人私密意图正文不得公开", "targetdigest", "source_snapshot", "declaration_epoch"} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("private/internal source leaked", private)
				}
			}
		})
	}
}

func TestAgentResultProjectionHTTPRegisteredBusinessNoPermissionDoesNotPublishConsole(t *testing.T) {
	f, bid, _ := businessKnowledgeHTTPNative(t)
	city := "result-biz-" + strings.ReplaceAll(bid, "-", "")
	f.exec(`INSERT INTO cities(id,name,country_code,region,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'本地合成商家城','GB','LOCAL','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:NOW004','合成维护者')`, city)
	f.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, city)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM agent_tasks WHERE city_id=$1`, `DELETE FROM activities WHERE city_id=$1`, `DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := f.pool.Exec(context.Background(), q, city); e != nil {
				t.Error(e)
			}
		}
	})
	a, e := f.store.CreateSocialDraft(f.ctx, f.accountIDs[0], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: bid}, CityID: city, Title: "合成线上营业活动", Summary: "公开摘要", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.PublishSocialActivity(f.ctx, f.accountIDs[0], a.ID); e != nil {
		t.Fatal(e)
	}
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	w := f.request(t, f.handler, http.MethodPost, "/v1/cities/"+city+"/agent/tasks", `{"query":"找商家"}`, f.tokens[0], 200, nil)
	r := resultHTTPReply(t, w)
	item := resultHTTPFind(t, r, "business", bid)
	if item.Summary != "" || item.Title != "合成商家" || item.Anchor != nil {
		t.Fatal("070 private console became public absent072 permission", item)
	}
	if strings.Contains(w.Body.String(), "https://example.invalid/profile") || strings.Contains(w.Body.String(), "本人受控资料") {
		t.Fatal("private console source escaped")
	}
	resultHTTPExplicitDomainActions(t, f, item)
	t.Logf("LOCAL_SYNTHETIC_ONLY original_business=%s original_activity=%s request_id=%s", bid, a.ID, w.Header().Get("X-Request-ID"))
}

type resultHTTPFinalBarrier struct {
	*postgres.Store
	before func(arp.Access, arp.Query, arp.Receipt)
	calls  int
}

func (b *resultHTTPFinalBarrier) RevalidateAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query, r arp.Receipt) error {
	b.calls++
	if b.before != nil {
		b.before(a, q, r)
	}
	return b.Store.RevalidateAgentResultProjection(ctx, a, q, r)
}
func TestAgentResultProjectionHTTPFinalNativeAfterEncodedBuffer(t *testing.T) {
	for _, change := range []string{"sourceHide", "sourceABA", "invitationRevoke", "TaskABA", "AgentABA", "retiredAgent", "revokeSession", "normalIdleRefresh"} {
		t.Run(change, func(t *testing.T) {
			f := contextBuilderHTTPNative(t)
			bridge := &resultHTTPFinalBarrier{Store: f.store}
			want := 409
			bridge.before = func(a arp.Access, q arp.Query, receipt arp.Receipt) {
				if len(receipt.Items) != 2 {
					t.Fatal("public and granted private original result missing")
				}
				if len(receipt.PublicCommercialRefs) != 1 || receipt.PublicCommercialRefs[0] != (arp.Ref{Type: "activity", ID: f.public}) {
					t.Fatal("private Activity entered commercial port")
				}
				switch change {
				case "sourceHide":
					f.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.public)
				case "sourceABA":
					f.exec(`UPDATE activities SET title=title WHERE id=$1`, f.private)
				case "invitationRevoke":
					f.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, f.private, f.accountIDs[0])
				case "TaskABA":
					f.exec(`UPDATE agent_tasks SET updated_at=updated_at WHERE id=$1`, a.TaskID)
				case "AgentABA":
					f.exec(`UPDATE agents SET status=status WHERE id=$1`, f.agentIDs[0])
				case "retiredAgent":
					f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
				case "revokeSession":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
				case "normalIdleRefresh":
					f.exec(`UPDATE sessions SET idle_expires_at=least(expires_at,clock_timestamp()+interval '90 minutes') WHERE token_sha256=$1`, a.SessionDigest[:])
				}
			}
			if change == "revokeSession" {
				want = 401
			}
			if change == "retiredAgent" {
				want = 404
			}
			if change == "normalIdleRefresh" {
				want = 200
			}
			h := New(f.store, f.store, nil, f.store, bridge, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
			w := f.request(t, h, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"帮我找羽毛球活动"}`, f.tokens[0], want, nil)
			if bridge.calls != 1 {
				t.Fatal("registered response bypassed final native receipt", bridge.calls)
			}
			if want != 200 && (strings.Contains(w.Body.String(), f.public) || strings.Contains(w.Body.String(), f.private) || strings.Contains(w.Body.String(), "CANARY")) {
				t.Fatal("encoded stale authorized source escaped", w.Body.String())
			}
		})
	}
}

func resultHTTPExplicitDomainActions(t *testing.T, f *privateProfileHTTPDBFixture, item arp.Item) {
	t.Helper()
	ref := *item.Detail
	path := map[string]string{"person": "/v1/accounts/" + ref.ID + "/profile", "activity": "/v1/activities/" + ref.ID, "place": "/v1/places/" + ref.ID, "community": "/v1/communities/" + ref.ID, "organization": "/v1/organizations/" + ref.ID, "business": "/v1/businesses/" + ref.ID}[ref.Type]
	w := historicalHTTPCall(f, "GET", path, "", f.tokens[0])
	if w.Code != 200 || !strings.Contains(w.Body.String(), ref.ID) {
		t.Fatal("stable original detail cannot reopen", ref, w.Code, w.Body.String())
	}
	ids := f.accountIDs[:2]
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])`, `DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`, `DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`, `DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(context.Background(), q, ids); e != nil {
				t.Error("owned domain action cleanup", e)
			}
		}
	})
	request, e := f.store.CreateFriendRequest(f.ctx, ids[0], ids[1], "本地合成明确测试连接")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DecideRequest(f.ctx, ids[1], request.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	ties, e := f.store.ListTies(f.ctx, ids[0])
	if e != nil || len(ties) != 1 {
		t.Fatal(e, ties)
	}
	convo, e := f.store.StartFriendConversation(f.ctx, ids[0], ties[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	var operation string
	if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&operation); e != nil {
		t.Fatal(e)
	}
	sharePath := "/v1/me/conversations/" + convo.ID + "/entity-shares"
	body := fmt.Sprintf(`{"operationId":%q,"entity":{"type":%q,"id":%q}}`, operation, ref.Type, ref.ID)
	w = historicalHTTPCall(f, "POST", sharePath, body, f.tokens[0])
	if w.Code != 201 {
		t.Fatal("original authorized share", ref, w.Code, w.Body.String())
	}
	var receipt struct{ Data connection.EntityShareReceipt }
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Data.Message.Entity == nil || receipt.Data.Message.Entity.Type != ref.Type || receipt.Data.Message.Entity.ID != ref.ID {
		t.Fatal("original chat card diverged", w.Body.String())
	}
	reopened := historicalHTTPCall(f, "GET", sharePath+"/"+operation, "", f.tokens[0])
	var recovered struct{ Data connection.EntityShareReceipt }
	if reopened.Code != 200 || json.Unmarshal(reopened.Body.Bytes(), &recovered) != nil || recovered.Data.Message.ID != receipt.Data.Message.ID {
		t.Fatal("original share recovery diverged")
	}
	if ref.Type == "activity" || ref.Type == "place" {
		body = fmt.Sprintf(`{"kind":%q,"targetId":%q}`, ref.Type, ref.ID)
		w = historicalHTTPCall(f, "POST", "/v1/me/saved", body, f.tokens[0])
		if item.Entity.Type == "opportunity" { // This fixture is an invited private Activity. The original Save API stays public-only.
			if w.Code != 404 {
				t.Fatal("private opportunity expanded Save permission", w.Code, w.Body.String())
			}
			return
		}
		if w.Code != 201 {
			t.Fatal("explicit original Save", ref, w.Code, w.Body.String())
		}
		w = historicalHTTPCall(f, "GET", "/v1/me/saved", "", f.tokens[0])
		if w.Code != 200 || !strings.Contains(w.Body.String(), ref.ID) {
			t.Fatal("saved original ID not returned", w.Code, w.Body.String())
		}
	}
}
