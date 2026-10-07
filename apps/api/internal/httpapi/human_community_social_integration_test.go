package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type commUXFixture struct {
	base *privateProfileHTTPDBFixture
	id   string
}

func commUXNative(t *testing.T, visibility, policy string) *commUXFixture {
	t.Helper()
	b := privateProfileHTTPDBNew(t)
	b.handler = New(b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, nil, false, nil, b.pool, nil)
	c, e := b.store.CreateSocialCommunity(b.ctx, b.accountIDs[0], community.SocialInput{Name: "本地合成COMM002社群", Summary: "本人明确本地合成私人社群说明", Visibility: visibility, JoinPolicy: policy})
	if e != nil {
		t.Fatal(e)
	}
	f := &commUXFixture{base: b, id: c.ID}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, `DELETE FROM communities WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, q, f.id); e != nil {
				t.Error("owned Community cleanup", e)
			}
		}
	})
	return f
}
func (f *commUXFixture) call(method, path, body string, who int) *httptest.ResponseRecorder {
	return f.callHeaders(method, path, body, who, nil)
}
func (f *commUXFixture) callHeaders(method, path, body string, who int, headers map[string]string) *httptest.ResponseRecorder {
	token := ""
	if who >= 0 {
		token = f.base.tokens[who]
	}
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.base.handler.ServeHTTP(w, r)
	return w
}
func (f *commUXFixture) preview(t *testing.T, method, path, body string, who int) string {
	t.Helper()
	before := f.effects(t)
	w := f.callHeaders(method, path, body, who, map[string]string{"X-Birdtie-Community-Preview": "1"})
	if w.Code != 200 || before != f.effects(t) {
		t.Fatalf("native preview must be current and no durable mutation: %d %s", w.Code, w.Body.String())
	}
	var v struct {
		Data community.HumanApproval `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.ActorID != f.base.accountIDs[who] || v.Data.CommunityID != f.id || v.Data.Snapshot == "" {
		t.Fatal("native preview binding")
	}
	return v.Data.Snapshot
}
func (f *commUXFixture) effects(t *testing.T) string {
	t.Helper()
	var out string
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT jsonb_build_object('community',(SELECT to_jsonb(c) FROM communities c WHERE id=$1),'members',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]'::jsonb) FROM community_memberships m WHERE community_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE resource_type='community' AND resource_id=$1::text))::text`, f.id).Scan(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func commUXWaitCommunity(t *testing.T, f *commUXFixture, pid int) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		var blocked bool
		if e := f.base.pool.QueryRow(f.base.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%communities%')`, pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual registered operation did not wait on Community")
}
func TestCommunityUXHiddenKnownIDJoin(t *testing.T) {
	f := commUXNative(t, "hidden", "open")
	before := f.effects(t)
	if w := f.call("GET", "/v1/communities/"+f.id, "", 1); w.Code != 404 {
		t.Fatal("hidden detail", w.Code)
	}
	w := f.call("POST", "/v1/communities/"+f.id+"/join", "", 1)
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual_registered_hidden_open_join_status=%d body=%s", w.Code, w.Body.String())
	if w.Code != 404 || before != f.effects(t) {
		t.Errorf("known hidden ID granted membership: status=%d unchanged=%t", w.Code, before == f.effects(t))
	}
}
func TestCommunityUXLateSessionJoin(t *testing.T) {
	for _, mode := range []string{"revoke", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := commUXNative(t, "public", "open")
			b := f.base
			digest, e := identity.ParseBearer("Bearer " + b.tokens[1])
			if e != nil {
				t.Fatal(e)
			}
			if mode == "expiry" {
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
			}
			before := f.effects(t)
			blocker, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			query, target := `SELECT pg_backend_pid() FROM communities WHERE id=$1 FOR UPDATE`, f.id
			if mode == "revoke" {
				query, target = `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, b.accountIDs[1]
			}
			if e = blocker.QueryRow(b.ctx, query, target).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.call("POST", "/v1/communities/"+f.id+"/join", "", 1) }()
			if mode == "revoke" {
				commUXWaitAccount(t, f, pid)
			} else {
				commUXWaitCommunity(t, f, pid)
			}
			if mode == "revoke" {
				if e = b.store.RevokeSession(b.ctx, digest); e != nil {
					t.Fatal(e)
				}
			} else {
				var expired bool
				until := time.Now().Add(4 * time.Second)
				for !expired && time.Now().Before(until) {
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if !expired {
						time.Sleep(15 * time.Millisecond)
					}
				}
				if !expired {
					t.Fatal("actual expiry not reached")
				}
			}
			if e = blocker.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY mode=%s registered_status=%d full_owned_source_unchanged=%t", mode, w.Code, before == f.effects(t))
				if (w.Code != 401 && w.Code != 403) || before != f.effects(t) {
					t.Errorf("late revoked/expired native session committed: status=%d unchanged=%t", w.Code, before == f.effects(t))
				}
			case <-time.After(6 * time.Second):
				t.Fatal("registered late session request stalled")
			}
		})
	}
}
func TestCommunityUXLateRoleChange(t *testing.T) {
	f := commUXNative(t, "public", "open")
	b := f.base
	m, e := b.store.JoinSocialCommunity(b.ctx, b.accountIDs[1], f.id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ChangeSocialMemberRole(b.ctx, b.accountIDs[0], f.id, m.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(community.SocialInput{Name: "旧管理员迟到编辑", Summary: "不得提交", Visibility: "public", JoinPolicy: "open"})
	snapshot := f.preview(t, "PATCH", "/v1/communities/"+f.id, string(raw), 1)
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var pid int
	if e = blocker.QueryRow(b.ctx, `SELECT pg_backend_pid() FROM communities WHERE id=$1 FOR UPDATE`, f.id).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.callHeaders("PATCH", "/v1/communities/"+f.id, string(raw), 1, map[string]string{"X-Birdtie-Community-Snapshot": snapshot})
	}()
	commUXWaitCommunity(t, f, pid)
	if _, e = blocker.Exec(b.ctx, `UPDATE community_memberships SET role='member',updated_at=clock_timestamp() WHERE id=$1`, m.ID); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		var name string
		if e = b.pool.QueryRow(b.ctx, `SELECT name FROM communities WHERE id=$1`, f.id).Scan(&name); e != nil {
			t.Fatal(e)
		}
		t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual_late_demoted_admin_status=%d community_name_changed=%t", w.Code, strings.Contains(name, "迟到"))
		if w.Code != 403 || strings.Contains(name, "迟到") {
			t.Errorf("late demoted native admin changed Community: status=%d", w.Code)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("late permission operation stalled")
	}
}
func commUXWaitAccount(t *testing.T, f *commUXFixture, pid int) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		var blocked bool
		if e := f.base.pool.QueryRow(f.base.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM accounts%')`, pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual current native gateway did not wait on Account")
}

// Appended to the owned registered integration test only after the root frame.
func TestCommunityUXManagementLifecycle(t *testing.T) {
	f := commUXNative(t, "private", "request")
	b := f.base
	w := f.call("POST", "/v1/communities/"+f.id+"/join", "", 1)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var joined struct {
		Data community.Membership `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &joined); e != nil || joined.Data.Status != "pending" {
		t.Fatal("current pending record")
	}
	m := joined.Data
	path := "/v1/communities/" + f.id + "/requests/" + m.ID + "/approve"
	before := f.effects(t)
	if w = f.call("POST", path, "", 0); w.Code != 428 || before != f.effects(t) {
		t.Fatal("missing concrete current approval must have no effect", w.Code)
	}
	snapshot := f.preview(t, "POST", path, "", 0)
	w = f.callHeaders("POST", path, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot})
	if w.Code != 200 {
		t.Fatal("approve", w.Code, w.Body.String())
	}
	before = f.effects(t)
	if w = f.callHeaders("POST", path, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 409 || before != f.effects(t) {
		t.Fatal("duplicate approval effects", w.Code)
	}
	rolePath := "/v1/communities/" + f.id + "/members/" + m.ID + "/role"
	snapshot = f.preview(t, "PUT", rolePath, `{"role":"admin"}`, 0)
	before = f.effects(t)
	if w = f.callHeaders("PUT", rolePath, `{"role":"member"}`, 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 409 || before != f.effects(t) {
		t.Fatal("specific role binding", w.Code)
	}
	if w = f.callHeaders("PUT", rolePath, `{"role":"admin"}`, 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 200 {
		t.Fatal("set admin", w.Code, w.Body.String())
	}
	if w = f.call("GET", "/v1/communities/"+f.id+"/requests", "", 1); w.Code != 200 {
		t.Fatal("actual new admin", w.Code)
	}
	ownerMember := ""
	if e := b.pool.QueryRow(b.ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, f.id, b.accountIDs[0]).Scan(&ownerMember); e != nil {
		t.Fatal(e)
	}
	before = f.effects(t)
	w = f.callHeaders("DELETE", "/v1/communities/"+f.id+"/members/"+ownerMember, "", 1, map[string]string{"X-Birdtie-Community-Preview": "1"})
	if w.Code != 403 || before != f.effects(t) {
		t.Fatal("admin cannot remove owner", w.Code)
	}
	transfer := "/v1/communities/" + f.id + "/transfer-owner"
	body := `{"userAccountId":"` + b.accountIDs[1] + `"}`
	snapshot = f.preview(t, "POST", transfer, body, 0)
	if w = f.callHeaders("POST", transfer, body, 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 204 {
		t.Fatal("transfer", w.Code, w.Body.String())
	}
	var ownerRole, oldRole, creator string
	if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT role FROM community_memberships WHERE community_id=c.id AND user_account_id=$2),(SELECT role FROM community_memberships WHERE community_id=c.id AND user_account_id=$3),c.owner_account_id FROM communities c WHERE c.id=$1`, f.id, b.accountIDs[1], b.accountIDs[0]).Scan(&ownerRole, &oldRole, &creator); e != nil {
		t.Fatal(e)
	}
	if ownerRole != "owner" || oldRole != "admin" || creator != b.accountIDs[0] {
		t.Fatal("actual retained role/creator contract")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY current membership transfer; legacy owner_account_id retains original creator, downstream current-owner projection unresolved")
	before = f.effects(t)
	w = f.callHeaders("POST", "/v1/communities/"+f.id+"/archive", "", 0, map[string]string{"X-Birdtie-Community-Preview": "1"})
	if w.Code != 403 || before != f.effects(t) {
		t.Fatal("former owner archive denial", w.Code)
	}
	snapshot = f.preview(t, "POST", "/v1/communities/"+f.id+"/archive", "", 1)
	if w = f.callHeaders("POST", "/v1/communities/"+f.id+"/archive", "", 1, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 204 {
		t.Fatal("current owner archive", w.Code)
	}
	if w = f.call("POST", "/v1/communities/"+f.id+"/join", "", 0); w.Code != 404 {
		t.Fatal("archived join", w.Code)
	}
}
func TestCommunityUXInvitationPrivacyAndRemoval(t *testing.T) {
	f := commUXNative(t, "hidden", "open")
	b := f.base
	path := "/v1/communities/" + f.id + "/invitations"
	body := `{"userAccountId":"` + b.accountIDs[1] + `"}`
	snapshot := f.preview(t, "POST", path, body, 0)
	w := f.callHeaders("POST", path, body, 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot})
	if w.Code != 201 {
		t.Fatal("invite", w.Code, w.Body.String())
	}
	var v struct {
		Data community.Membership `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data.Status != "invited" {
		t.Fatal("invited state")
	}
	if w = f.call("GET", "/v1/communities/"+f.id, "", 1); w.Code != 200 || strings.Contains(w.Body.String(), "合成私人社群说明") {
		t.Fatal("invitee limited detail", w.Code, w.Body.String())
	}
	if w = f.call("GET", "/v1/communities/"+f.id+"/members", "", 1); w.Code != 404 {
		t.Fatal("invitee not yet member", w.Code)
	}
	w = f.call("GET", "/v1/communities/"+f.id+"/requests", "", 0)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "invited") {
		t.Fatal("current manager can revoke retained invitations", w.Code, w.Body.String())
	}
	remove := "/v1/communities/" + f.id + "/members/" + v.Data.ID
	snapshot = f.preview(t, "DELETE", remove, "", 0)
	if w = f.callHeaders("DELETE", remove, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 204 {
		t.Fatal("invite revoke", w.Code)
	}
	if w = f.call("POST", "/v1/communities/"+f.id+"/join", "", 1); w.Code != 404 {
		t.Fatal("withdrawn hidden invite cannot join", w.Code)
	}
	snapshot = f.preview(t, "POST", path, body, 0)
	if w = f.callHeaders("POST", path, body, 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 201 {
		t.Fatal("new invite", w.Code)
	}
	if w = f.call("POST", "/v1/communities/"+f.id+"/join", "", 1); w.Code != 200 {
		t.Fatal("explicit accept", w.Code)
	}
	b.exec(`UPDATE user_profiles SET display_name='COMM_PRIVATE_NAME_CANARY',visibility='private' WHERE account_id=$1`, b.accountIDs[1])
	// A Community roster already authorizes member identity; the ordinary
	// Profile audience is not its independent displayName field policy. Use
	// the real human privacy API rather than treating a raw source edit as it.
	w = f.call("GET", "/v1/me/agent-profile-visibility", "", 1)
	var policy struct {
		Data agentprofile.VisibilityRecord `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &policy) != nil {
		t.Fatal("real current field policy read", w.Code, w.Body.String())
	}
	policy.Data.Rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate, CommunityIDs: []string{}}
	payload, e := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: policy.Data.Profile.ProfileVersion, Rules: policy.Data.Rules})
	if e != nil {
		t.Fatal(e)
	}
	w = f.call("PUT", "/v1/me/agent-profile-visibility", string(payload), 1)
	if w.Code != 200 {
		t.Fatal("real human field privacy write", w.Code, w.Body.String())
	}
	if w = f.call("GET", "/v1/communities/"+f.id+"/members", "", 0); w.Code != 200 || strings.Contains(w.Body.String(), "COMM_PRIVATE_NAME_CANARY") {
		t.Fatal("retained visibility resolver", w.Code, w.Body.String())
	}
	snapshot = f.preview(t, "DELETE", remove, "", 0)
	if w = f.callHeaders("DELETE", remove, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}); w.Code != 204 {
		t.Fatal("remove", w.Code)
	}
	if w = f.call("GET", "/v1/communities/"+f.id, "", 1); w.Code != 404 {
		t.Fatal("removed hidden detail", w.Code)
	}
}
func TestCommunityUXApprovalABAAndSubjects(t *testing.T) {
	for _, mode := range []string{"target_aba", "community_aba", "account_aba", "different_session", "different_operation", "missing", "tamper"} {
		t.Run(mode, func(t *testing.T) {
			f := commUXNative(t, "private", "request")
			b := f.base
			w := f.call("POST", "/v1/communities/"+f.id+"/join", "", 1)
			var m struct {
				Data community.Membership `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m) != nil {
				t.Fatal("pending")
			}
			path := "/v1/communities/" + f.id + "/requests/" + m.Data.ID + "/approve"
			snapshot := f.preview(t, "POST", path, "", 0)
			switch mode {
			case "target_aba":
				b.exec(`UPDATE community_memberships SET status='rejected' WHERE id=$1`, m.Data.ID)
				b.exec(`UPDATE community_memberships SET status='pending',updated_at=$2 WHERE id=$1`, m.Data.ID, m.Data.UpdatedAt)
			case "community_aba":
				b.exec(`UPDATE communities SET name=name||'临时' WHERE id=$1`, f.id)
				b.exec(`UPDATE communities SET name='本地合成COMM002社群' WHERE id=$1`, f.id)
			case "account_aba":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.accountIDs[0])
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.accountIDs[0])
			case "different_session":
				b.tokens[0] = b.newSession(b.accountIDs[0], false, false)
			case "different_operation":
				path = strings.TrimSuffix(path, "approve") + "reject"
			case "missing":
				snapshot = ""
			case "tamper":
				snapshot += "0"
			}
			before := f.effects(t)
			w = f.callHeaders("POST", path, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot})
			want := 409
			if mode == "missing" {
				want = 400
			}
			if w.Code != want || before != f.effects(t) {
				t.Fatal("actual stale/current subject confirmation effects", mode, w.Code, w.Body.String())
			}
		})
	}
}
func TestCommunityUXIdentityBoundaries(t *testing.T) {
	f := commUXNative(t, "public", "open")
	for _, who := range []int{-1, 2, 3} {
		before := f.effects(t)
		w := f.call("POST", "/v1/communities/"+f.id+"/join", "", who)
		want := 403
		if who < 0 {
			want = 401
		}
		if w.Code != want || before != f.effects(t) {
			t.Fatal("actual person boundary", who, w.Code)
		}
	}
	before := f.effects(t)
	digest, _ := identity.ParseBearer("Bearer " + f.base.tokens[1])
	if _, e := f.base.store.MutateHumanCommunity(nil, digest, identity.Actor{ID: f.base.accountIDs[1], AccountType: "person"}, community.HumanCommand{Operation: "join", CommunityID: f.id}); !errors.Is(e, community.ErrUnavailable) || before != f.effects(t) {
		t.Fatal("live pool nil ctx guard", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := f.base.store.MutateHumanCommunity(ctx, digest, identity.Actor{ID: f.base.accountIDs[1], AccountType: "person"}, community.HumanCommand{Operation: "join", CommunityID: f.id}); !errors.Is(e, community.ErrUnavailable) || before != f.effects(t) {
		t.Fatal("cancelled ctx guard", e)
	}
}
func TestCommunityUXConcurrentConcreteApproval(t *testing.T) {
	f := commUXNative(t, "public", "request")
	w := f.call("POST", "/v1/communities/"+f.id+"/join", "", 1)
	var m struct {
		Data community.Membership `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m) != nil {
		t.Fatal("request")
	}
	path := "/v1/communities/" + f.id + "/requests/" + m.Data.ID + "/approve"
	snapshot := f.preview(t, "POST", path, "", 0)
	var before int
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT count(*) FROM audit_events WHERE resource_type='community' AND resource_id=$1 AND action='community_request_approve'`, f.id).Scan(&before); e != nil {
		t.Fatal(e)
	}
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- f.callHeaders("POST", path, "", 0, map[string]string{"X-Birdtie-Community-Snapshot": snapshot}).Code
		}()
	}
	a, b := <-done, <-done
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatal("one concrete current write", a, b)
	}
	var after int
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT count(*) FROM audit_events WHERE resource_type='community' AND resource_id=$1 AND action='community_request_approve'`, f.id).Scan(&after); e != nil || after-before != 1 {
		t.Fatal("exact audit once", after, before, e)
	}
}
