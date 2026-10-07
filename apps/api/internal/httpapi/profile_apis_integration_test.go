package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These barriers use actual registered routes, native sessions and PostgreSQL
// locks. The first Authenticate succeeds before the transaction waits; there
// is no spy authorizing the second operation.
func TestProfileAPIsPublicCurrentSession(t *testing.T) {
	for _, mode := range []string{"expiry", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			f := privateProfileHTTPDBNew(t)
			token := f.tokens[0]
			digest, _ := identity.ParseBearer("Bearer " + token)
			if mode == "expiry" {
				f.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',
					idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
			}
			before := f.effects(t)
			blocker, err := f.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal("cannot establish owned current-session barrier")
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if err = blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.accountIDs[0]).Scan(&pid); err != nil {
				t.Fatal("cannot lock owned Account")
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				r := privateProfileHTTPRequest(http.MethodPut, "/v1/me/profile", `{"displayName":"等待后的新资料","bio":"不得提交","visibility":"private"}`, token, "application/json")
				r.Header.Set("X-Request-ID", "public_current_session_"+mode)
				rw := httptest.NewRecorder()
				f.handler.ServeHTTP(rw, r)
				done <- rw
			}()
			profileAPIsWaitBlocked(t, f, pid)
			if mode == "revoke" {
				if err = f.store.RevokeSession(f.ctx, digest); err != nil {
					t.Fatal("native session revoke failed")
				}
			} else {
				var expired bool
				deadline := time.Now().Add(4 * time.Second)
				for !expired && time.Now().Before(deadline) {
					if err = f.pool.QueryRow(f.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); err != nil {
						t.Fatal("cannot read owned session clock")
					}
					if !expired {
						time.Sleep(20 * time.Millisecond)
					}
				}
				if !expired {
					t.Fatal("session expiry barrier not reached")
				}
			}
			if err = blocker.Commit(f.ctx); err != nil {
				t.Fatal("cannot release owned Account barrier")
			}
			select {
			case rw := <-done:
				after := f.effects(t)
				t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY mode=%s actual_status=%d profile_unchanged=%t request_id=%s", mode, rw.Code, before == after, rw.Header().Get("X-Request-ID"))
				if rw.Code != http.StatusUnauthorized || before != after {
					t.Errorf("current-session boundary: status=%d want401, owned source unchanged=%t", rw.Code, before == after)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("current-session request did not complete")
			}
		})
	}
}

func profileAPIsWaitBlocked(t *testing.T, f *privateProfileHTTPDBFixture, pid int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM accounts%')`, pid).Scan(&blocked); err != nil {
			t.Fatal("cannot observe real Account wait")
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP did not reach actual Account wait after initial Authenticate")
}

func profileAPIsSnapshot(t *testing.T, f *privateProfileHTTPDBFixture, cognitive bool) string {
	t.Helper()
	var snapshot string
	query := `SELECT jsonb_build_object(
		'public',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.account_id),'[]'::jsonb) FROM user_profiles p WHERE p.account_id=ANY($1::uuid[])),
		'intents',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.id),'[]'::jsonb) FROM intents i WHERE i.owner_account_id=ANY($1::uuid[])),
		'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE a.actor_account_id=ANY($1::uuid[])))::text`
	if cognitive {
		query = `SELECT jsonb_build_object(
		'metadata',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]'::jsonb) FROM agent_profiles p WHERE p.owner_id=ANY($1::uuid[])),
		'private',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]'::jsonb) FROM agent_private_profiles p WHERE p.owner_id=ANY($1::uuid[])),
		'policy',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]'::jsonb) FROM agent_profile_field_visibility p WHERE p.owner_id=ANY($1::uuid[])),
		'memory',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.id),'[]'::jsonb) FROM agent_memories p WHERE p.owner_id=ANY($1::uuid[])),
		'grants',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.id),'[]'::jsonb) FROM consent_grants p WHERE p.owner_account_id=ANY($1::uuid[])))::text`
	}
	if err := f.pool.QueryRow(f.ctx, query, f.accountIDs).Scan(&snapshot); err != nil {
		t.Fatal("owned Profile source snapshot failed")
	}
	return snapshot
}

// Only a successful native mutation may append exactly its minimal audit.
// Public sources and all pre-existing audit bytes must remain unchanged.
type profileAPIsAuditSpec struct{ action, kind, resource, purpose, requestID string }

func profileAPIsAssertAuditDelta(t *testing.T, f *privateProfileHTTPDBFixture, before string, expected ...profileAPIsAuditSpec) {
	t.Helper()
	var old, now map[string]json.RawMessage
	if json.Unmarshal([]byte(before), &old) != nil || json.Unmarshal([]byte(profileAPIsSnapshot(t, f, false)), &now) != nil {
		t.Fatal("invalid owned audit snapshot")
	}
	var previous, current []map[string]any
	if json.Unmarshal(old["audit"], &previous) != nil || json.Unmarshal(now["audit"], &current) != nil {
		t.Fatal("invalid owned audit array")
	}

	delete(old, "audit")
	delete(now, "audit")
	if !reflect.DeepEqual(old, now) {
		t.Fatal("ordinary public/Intent source changed")
	}
	added, err := profileAPIsAuditAdded(previous, current)
	if err != nil || len(added) != len(expected) {
		t.Fatal("pre-existing audit changed or audit delta not exact")
	}
	newRows := append([]map[string]any(nil), added...)

	for _, want := range expected {
		matched := -1
		for i, row := range added {
			if row["actor_account_id"] == f.accountIDs[0] && row["action"] == want.action && row["resource_type"] == want.kind && row["resource_id"] == want.resource && row["decision"] == "allowed" && row["purpose"] == want.purpose && row["request_id"] == want.requestID && row["target_resource_id"] == nil {
				matched = i
				break
			}
		}
		if matched < 0 {
			t.Fatal("successful action missing exact actor/Agent/purpose/request audit")
		}
		added = append(added[:matched], added[matched+1:]...)
	}
	for _, marker := range []string{privateProfileHTTPMarker, "本人新的直接输入", "本人自述城市历史"} {
		if strings.Contains(string(mustProfileAuditJSON(t, newRows)), marker) {
			t.Fatal("private content leaked to audit")
		}
	}
}

// Audit IDs are random UUIDs: additions may sort before or between old rows.
func profileAPIsAuditKey(row map[string]any) (string, bool) {
	value, ok := row["id"]
	if !ok || value == nil {
		return "", false
	}
	switch value.(type) {
	case string, float64, json.Number:
	default:
		return "", false
	}
	raw, err := json.Marshal(value)
	return string(raw), err == nil
}
func profileAPIsAuditAdded(previous, current []map[string]any) ([]map[string]any, error) {
	old := map[string]map[string]any{}
	for _, row := range previous {
		id, ok := profileAPIsAuditKey(row)
		if !ok || old[id] != nil {
			return nil, errors.New("invalid prior audit ID")
		}
		old[id] = row
	}
	seen := map[string]bool{}
	added := []map[string]any{}
	for _, row := range current {
		id, ok := profileAPIsAuditKey(row)
		if !ok || seen[id] {
			return nil, errors.New("invalid current audit ID")
		}
		seen[id] = true
		if prior, found := old[id]; found {
			if !reflect.DeepEqual(prior, row) {
				return nil, errors.New("prior audit modified")
			}
		} else {
			added = append(added, row)
		}
	}
	for id := range old {
		if !seen[id] {
			return nil, errors.New("prior audit removed")
		}
	}
	return added, nil
}

func TestProfileAPIsAuditDeltaUUIDOrdering(t *testing.T) {
	row := func(id, action string) map[string]any {
		return map[string]any{"id": id, "action": action, "actor_account_id": "owned"}
	}
	oldA := row("40000000-0000-4000-8000-000000000000", "replace")
	oldB := row("90000000-0000-4000-8000-000000000000", "replace")
	first := row("10000000-0000-4000-8000-000000000000", "replace")
	middle := row("60000000-0000-4000-8000-000000000000", "replace")
	previous := []map[string]any{oldA, oldB}
	current := []map[string]any{first, oldA, middle, oldB}
	got, err := profileAPIsAuditAdded(previous, current)
	if err != nil || !reflect.DeepEqual(got, []map[string]any{first, middle}) {
		t.Fatal("UUID additions before/between old rows not exact")
	}
	for _, tc := range []struct {
		name string
		rows []map[string]any
	}{
		{"changed", []map[string]any{first, row("40000000-0000-4000-8000-000000000000", "altered"), oldB}},
		{"removed", []map[string]any{first, oldA}},
		{"duplicate", []map[string]any{first, oldA, oldA, oldB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := profileAPIsAuditAdded(previous, tc.rows); err == nil {
				t.Fatal("invalid old audit mutation accepted")
			}
		})
	}
	if !reflect.DeepEqual(current, []map[string]any{first, oldA, middle, oldB}) {
		t.Fatal("audit difference mutated original rows")
	}
}

func mustProfileAuditJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func profileAPIsInsertIntents(t *testing.T, f *privateProfileHTTPDBFixture) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM intents WHERE owner_account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error("owned Profile Intent cleanup failed")
		}
	})
	f.exec(`INSERT INTO intents(owner_account_id,city_id,topic,available_from,available_until,time_zone,coarse_area_label,audience,state,expires_at,owner_confirmed_at)
		SELECT $1,id,'合成旧公开意图',now(),now()+interval '1 hour','Europe/London','合成区域',v.audience,v.state,now()+interval '2 hours',now()
		FROM (SELECT id FROM cities ORDER BY id LIMIT 1) c CROSS JOIN (VALUES('public','active'),('public','draft'),('public','fulfilled'),('private','active')) v(audience,state)`, f.accountIDs[0])
}

func TestProfileAPIsThreeNativeOperations(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	profileAPIsInsertIntents(t, f)
	var current agentprofile.PrivateRecord
	t.Run("GET current Agent metadata no write", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, true)
		ordinary := profileAPIsSnapshot(t, f, false)
		current = privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if current.Profile.AgentID != f.agentIDs[0] || current.Profile.OwnerID != f.accountIDs[0] || current.Profile.ProfileVersion != 1 || current.Configured || before != profileAPIsSnapshot(t, f, true) {
			t.Fatal("GET invented content or changed native identity")
		}
		profileAPIsAssertAuditDelta(t, f, ordinary)
	})
	t.Run("UPDATE private nine fields separate original public", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, false)
		fields := agentprofile.PrivateFields{PersonalPreferences: []string{"本人偏好"}, SocialPreferences: []string{"本人社交偏好"}, Availability: "本人可用时间", PreferredActivityTypes: []string{"羽毛球"}, TravelPreferences: []string{"步行"}, InteractionPreferences: []string{"中文"}, PrivateCityHistory: "本人自述城市历史", LanguagePreferences: []string{"中文"}, AgentNotes: privateProfileHTTPMarker}
		raw, _ := json.Marshal(agentprofile.ReplacePrivateInput{ExpectedVersion: 1, Fields: fields})
		w := f.request(t, f.handler, "PUT", privateProfileHTTPPath, string(raw), f.tokens[0], 200, nil)
		current = privateProfileHTTPDBRecord(t, w)
		if current.Profile.ProfileVersion != 2 || !reflect.DeepEqual(current.Fields, fields) {
			t.Fatal("private edit contaminated ordinary source")
		}
		profileAPIsAssertAuditDelta(t, f, before, profileAPIsAuditSpec{"replace", "agent_private_profile", f.agentIDs[0], "human_profile_edit", w.Header().Get("X-Request-ID")})
	})
	t.Run("UPDATE Public original client preserves private policy Memory permissions", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, true)
		want := identity.Profile{AccountID: f.accountIDs[0], DisplayName: "新中文姓名", Bio: "原客户端三字段", Visibility: "private"}
		w := f.request(t, f.handler, "PUT", "/v1/me/profile", `{"displayName":" 新中文姓名 ","bio":" 原客户端三字段 ","visibility":"private"}`, f.tokens[0], 200, nil)
		privateProfileHTTPDBAssertPublic(t, w, want)
		if before != profileAPIsSnapshot(t, f, true) {
			t.Fatal("public edit wrote private metadata, policy, Memory or permission")
		}
		var withdrawn, untouched int
		if f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE audience='public' AND state='withdrawn'),count(*) FILTER(WHERE (audience='public' AND state='fulfilled') OR (audience='private' AND state='active')) FROM intents WHERE owner_account_id=$1`, f.accountIDs[0]).Scan(&withdrawn, &untouched) != nil || withdrawn != 2 || untouched != 2 {
			t.Fatal("old Public Intent withdrawal changed")
		}
		f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", "", 404, nil)
		privateProfileHTTPDBAssertPublic(t, f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[0], 200, nil), want)
	})
	t.Run("public no op no source or audit write", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, false)
		f.request(t, f.handler, "PUT", "/v1/me/profile", `{"displayName":"新中文姓名","bio":"原客户端三字段","visibility":"private"}`, f.tokens[0], 200, nil)
		if before != profileAPIsSnapshot(t, f, false) {
			t.Fatal("idempotent identical public edit wrote source/audit")
		}
	})
	t.Run("restart native Store connection preserves both resources", func(t *testing.T) {
		ordinary := profileAPIsSnapshot(t, f, false)
		pool, err := pgxpool.New(f.ctx, f.pool.Config().ConnString())
		if err != nil {
			t.Fatal("new native connection failed")
		}
		defer pool.Close()
		store := postgres.New(pool, false)
		handler := privateProfileHTTPNew(store, store)
		restored := privateProfileHTTPDBRecord(t, f.request(t, handler, "GET", privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if !reflect.DeepEqual(restored, current) {
			t.Fatal("private content/version changed after Store reconnect")
		}
		privateProfileHTTPDBAssertPublic(t, f.request(t, handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[0], 200, nil), identity.Profile{AccountID: f.accountIDs[0], DisplayName: "新中文姓名", Bio: "原客户端三字段", Visibility: "private"})
		profileAPIsAssertAuditDelta(t, f, ordinary)
	})
	t.Run("stale Private CAS denies", func(t *testing.T) {
		ordinary := profileAPIsSnapshot(t, f, false)
		before := profileAPIsSnapshot(t, f, true)
		f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":1,"fields":{}}`, f.tokens[0], 409, nil)
		if before != profileAPIsSnapshot(t, f, true) {
			t.Fatal("stale private write changed current source")
		}
		profileAPIsAssertAuditDelta(t, f, ordinary)
	})
	t.Run("Private and policy simultaneous CAS only one winner", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, false)
		rules := agentprofile.DefaultFieldRules()
		policy, _ := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: 2, Rules: rules})
		requests := []struct{ path, body string }{{privateProfileHTTPPath, `{"expectedVersion":2,"fields":{"agentNotes":"本人新的直接输入"}}`}, {"/v1/me/agent-profile-visibility", string(policy)}}
		start := make(chan struct{})
		type casResult struct {
			code            int
			path, requestID string
		}
		done := make(chan casResult, 2)
		for _, request := range requests {
			go func(path, body string) {
				<-start
				rw := httptest.NewRecorder()
				r := privateProfileHTTPRequest("PUT", path, body, f.tokens[0], "application/json")
				id := "profile_cas_private_086"
				if path != privateProfileHTTPPath {
					id = "profile_cas_visibility_086"
				}
				r.Header.Set("X-Request-ID", id)
				f.handler.ServeHTTP(rw, r)
				done <- casResult{rw.Code, path, rw.Header().Get("X-Request-ID")}
			}(request.path, request.body)
		}
		close(start)
		a, b := <-done, <-done
		if !((a.code == 200 && b.code == 409) || (a.code == 409 && b.code == 200)) {
			t.Fatal("two domain writes did not share actual aggregate CAS")
		}
		winner := a
		if winner.code != 200 {
			winner = b
		}
		kind, purpose := "agent_private_profile", "human_profile_edit"
		if winner.path != privateProfileHTTPPath {
			kind, purpose = "agent_field_visibility", "human_field_audience_edit"
		}
		profileAPIsAssertAuditDelta(t, f, before, profileAPIsAuditSpec{"replace", kind, f.agentIDs[0], purpose, winner.requestID})
		after := profileAPIsSnapshot(t, f, false)
		current = privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		profileAPIsAssertAuditDelta(t, f, after)
		if current.Profile.ProfileVersion != 3 {
			t.Fatal("concurrent private/policy writes advanced twice")
		}
	})
	t.Run("explicit clear advances current version without public change", func(t *testing.T) {
		before := profileAPIsSnapshot(t, f, false)
		w := f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":3,"fields":{}}`, f.tokens[0], 200, nil)
		current = privateProfileHTTPDBRecord(t, w)
		if current.Configured || !agentprofile.PrivateFieldsEmpty(current.Fields) || current.Profile.ProfileVersion != 4 {
			t.Fatal("explicit private clear changed other domain")
		}
		profileAPIsAssertAuditDelta(t, f, before, profileAPIsAuditSpec{"replace", "agent_private_profile", f.agentIDs[0], "human_profile_edit", w.Header().Get("X-Request-ID")})
	})
}

func TestProfileAPIsNativeSelfTargetsAndCompatibility(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	for _, index := range []int{2, 3} {
		f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'组织或业务自己的旧资料','合成资料','private')`, f.accountIDs[index])
	}
	t.Run("other Person writes only own public and private", func(t *testing.T) {
		var first string
		f.pool.QueryRow(f.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, f.accountIDs[0]).Scan(&first)
		w := f.request(t, f.handler, "PUT", "/v1/me/profile", publicProfileValid, f.tokens[1], 200, nil)
		privateProfileHTTPDBAssertPublic(t, w, identity.Profile{AccountID: f.accountIDs[1], DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"})
		other := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[1], 200, nil))
		if other.Profile.OwnerID != f.accountIDs[1] || other.Profile.AgentID != f.agentIDs[1] {
			t.Fatal("self selection inherited another Agent")
		}
		var after string
		f.pool.QueryRow(f.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, f.accountIDs[0]).Scan(&after)
		if first != after {
			t.Fatal("other Person edit contaminated owner")
		}
	})
	for _, index := range []int{2, 3} {
		t.Run("existing ordinary account type "+strconv.Itoa(index), func(t *testing.T) {
			before := profileAPIsSnapshot(t, f, true)
			w := f.request(t, f.handler, "PUT", "/v1/me/profile", publicProfileValid, f.tokens[index], 200, nil)
			privateProfileHTTPDBAssertPublic(t, w, identity.Profile{AccountID: f.accountIDs[index], DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"})
			f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[index], 403, nil)
			f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":1,"fields":{}}`, f.tokens[index], 403, nil)
			if before != profileAPIsSnapshot(t, f, true) {
				t.Fatal("ordinary Org/Biz edit created Agent content/role")
			}
		})
	}
	t.Run("missing ordinary row not upserted", func(t *testing.T) {
		f.exec(`DELETE FROM user_profiles WHERE account_id=$1`, f.accountIDs[3])
		before := profileAPIsSnapshot(t, f, false)
		f.request(t, f.handler, "PUT", "/v1/me/profile", publicProfileValid, f.tokens[3], 404, nil)
		if before != profileAPIsSnapshot(t, f, false) {
			t.Fatal("missing original source was rebuilt")
		}
	})
	for _, tc := range []struct {
		name, path, body string
		token            int
		status           int
		workspace        bool
	}{
		{"cross owner selector", "/v1/me/profile?ownerId=" + f.accountIDs[0], publicProfileValid, 1, 400, false},
		{"foreign agent selector", "/v1/me/profile?agentId=" + f.agentIDs[0], publicProfileValid, 1, 400, false},
		{"workspace", "/v1/me/profile", publicProfileValid, 0, 403, true},
		{"wire owner", "/v1/me/profile", strings.TrimSuffix(publicProfileValid, "}") + `,"ownerId":"` + f.accountIDs[1] + `"}`, 0, 400, false},
		{"wire duplicate", "/v1/me/profile", strings.TrimSuffix(publicProfileValid, "}") + `,"visibility":"private"}`, 0, 400, false},
		{"wire null", "/v1/me/profile", `{"displayName":"中文","bio":null,"visibility":"public"}`, 0, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := profileAPIsSnapshot(t, f, false)
			beforeC := profileAPIsSnapshot(t, f, true)
			var workspace *string
			if tc.workspace {
				workspace = &f.accountIDs[2]
			}
			f.request(t, f.handler, "PUT", tc.path, tc.body, f.tokens[tc.token], tc.status, workspace)
			if before != profileAPIsSnapshot(t, f, false) || beforeC != profileAPIsSnapshot(t, f, true) {
				t.Fatal("denied selector wrote native source")
			}
		})
	}
	t.Run("public human edit does not recreate removed private metadata", func(t *testing.T) {
		f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
		before := profileAPIsSnapshot(t, f, true)
		f.request(t, f.handler, "PUT", "/v1/me/profile", publicProfileValid, f.tokens[0], 200, nil)
		f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[0], 404, nil)
		if before != profileAPIsSnapshot(t, f, true) {
			t.Fatal("public human source rebuilt removed metadata")
		}
	})
}

func TestProfileAPIsNativeSessionGatewayRejects(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
	valid := identity.ProfileInput{DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"}
	actor := identity.Actor{ID: f.accountIDs[0], AccountType: "person"}
	for _, name := range []string{"zero digest", "other owner", "other type", "case alias", "invalid UUID", "invalid input", "canceled", "expired", "revoked", "dev disabled", "inactive account"} {
		t.Run(name, func(t *testing.T) {
			before := profileAPIsSnapshot(t, f, false)
			beforeC := profileAPIsSnapshot(t, f, true)
			d, a, input, ctx := digest, actor, valid, f.ctx
			want := identity.ErrUnauthorized
			switch name {
			case "zero digest":
				d = [32]byte{}
			case "other owner":
				a.ID = f.accountIDs[1]
			case "other type":
				a.AccountType = "organization"
			case "case alias":
				a.AccountType = "PERSON"
			case "invalid UUID":
				a.ID = "untrusted-id"
			case "invalid input":
				input.Bio = "\x00"
				want = identity.ErrInvalidProfile
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = identity.ErrProfileUnavailable
			case "expired":
				token := f.newSession(f.accountIDs[0], true, false)
				d, _ = identity.ParseBearer("Bearer " + token)
			case "revoked":
				token := f.newSession(f.accountIDs[0], false, true)
				d, _ = identity.ParseBearer("Bearer " + token)
			case "dev disabled":
				f.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE token_sha256=$1`, d[:])
				defer f.exec(`UPDATE sessions SET authentication_method='test' WHERE token_sha256=$1`, d[:])
			case "inactive account":
				f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, a.ID)
				defer f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, a.ID)
			}
			p, err := f.store.UpdateHumanProfile(ctx, d, a, input)
			if !errors.Is(err, want) || p != (identity.Profile{}) || before != profileAPIsSnapshot(t, f, false) || beforeC != profileAPIsSnapshot(t, f, true) {
				t.Fatalf("native gate rejected=%t source unchanged=%t", errors.Is(err, want), before == profileAPIsSnapshot(t, f, false))
			}
		})
	}
}

func TestProfileAPIsFinalClockAfterProfileWait(t *testing.T) {
	for _, noOp := range []bool{false, true} {
		t.Run(strconv.FormatBool(noOp), func(t *testing.T) {
			f := privateProfileHTTPDBNew(t)
			profileAPIsInsertIntents(t, f)
			digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
			f.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
			before := profileAPIsSnapshot(t, f, false)
			blocker, err := f.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal("cannot establish owned Profile clock barrier")
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM user_profiles WHERE account_id=$1 FOR UPDATE`, f.accountIDs[0]).Scan(&pid) != nil {
				t.Fatal("cannot lock owned Profile")
			}
			body := `{"displayName":"晚到的本人资料","bio":"不应提交","visibility":"private"}`
			if noOp {
				body = `{"displayName":"合成 HTTP 公开资料","bio":"只有旧公开字段，非真实用户","visibility":"public"}`
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				rw := httptest.NewRecorder()
				f.handler.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", body, f.tokens[0], "application/json"))
				done <- rw
			}()
			deadline := time.Now().Add(4 * time.Second)
			blocked := false
			for !blocked && time.Now().Before(deadline) {
				if f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%user_profiles%')`, pid).Scan(&blocked) != nil {
					t.Fatal("cannot observe real Profile wait")
				}
				if !blocked {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if !blocked {
				t.Fatal("current-session gateway did not reach native Profile wait")
			}
			var expired bool
			for !expired && time.Now().Before(deadline) {
				if f.pool.QueryRow(f.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired) != nil {
					t.Fatal("clock query failed")
				}
				if !expired {
					time.Sleep(20 * time.Millisecond)
				}
			}
			if !expired {
				t.Fatal("final expiry not reached")
			}
			if blocker.Commit(f.ctx) != nil {
				t.Fatal("barrier release failed")
			}
			select {
			case rw := <-done:
				if rw.Code != 401 || before != profileAPIsSnapshot(t, f, false) {
					t.Fatalf("final clock no-op=%t status=%d source stable=%t", noOp, rw.Code, before == profileAPIsSnapshot(t, f, false))
				}
				t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY final_clock no_op=%t status=%d source/audit/intents rollback=true", noOp, rw.Code)
			case <-time.After(6 * time.Second):
				t.Fatal("final-clock request timed out")
			}
		})
	}
}

func TestProfileAPIsNonemptySourcesRemainSeparate(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":1,"fields":{"agentNotes":"`+privateProfileHTTPMarker+`"}}`, f.tokens[0], 200, nil)
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	rules, err := agentprofile.NormalizeFieldRules(rules)
	if err != nil {
		t.Fatal("synthetic policy shape invalid")
	}
	policy, _ := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: 2, Rules: rules})
	f.request(t, f.handler, "PUT", "/v1/me/agent-profile-visibility", string(policy), f.tokens[0], 200, nil)
	var memoryID string
	if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&memoryID) != nil {
		t.Fatal("cannot create owned synthetic Memory ID")
	}
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+memoryID, memoryDBHTTPBody(t, 0, "profile-api-isolation", privateProfileHTTPMarker), f.tokens[0], 200, nil)
	if _, err := f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour)); err != nil {
		t.Fatal("cannot create native explicit ordinary Profile grant")
	}
	before := profileAPIsSnapshot(t, f, true)
	w := f.request(t, f.handler, "PUT", "/v1/me/profile", publicProfileValid, f.tokens[0], 200, nil)
	privateProfileHTTPDBAssertPublic(t, w, identity.Profile{AccountID: f.accountIDs[0], DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"})
	if before != profileAPIsSnapshot(t, f, true) {
		t.Fatal("Public altered existing private/policy/Memory/grant complete rows")
	}
	for _, token := range []string{"", f.tokens[1]} {
		w = f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", token, 200, nil)
		privateProfileHTTPDBAssertPublic(t, w, identity.Profile{AccountID: f.accountIDs[0], DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"})
		if strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
			t.Fatal("ordinary Profile leaked nonempty cognition source")
		}
	}
	other := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[1], 200, nil))
	if other.Configured || other.Profile.OwnerID != f.accountIDs[1] {
		t.Fatal("profile_view grant exposed owner's private source")
	}
}

func TestProfileAPIsReadCommittedLateAccountBarrier(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	config := f.pool.Config().Copy()
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal("cannot create alternate isolation pool")
	}
	defer pool.Close()
	var setting string
	if pool.QueryRow(f.ctx, `SHOW default_transaction_isolation`).Scan(&setting) != nil || setting != "repeatable read" {
		t.Fatal("real default RR pool not established")
	}
	store := postgres.New(pool, false)
	handler := privateProfileHTTPNew(store, store)
	before := profileAPIsSnapshot(t, f, false)
	blocker, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("cannot establish owned Account barrier")
	}
	defer blocker.Rollback(context.Background())
	var pid int
	if blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.accountIDs[0]).Scan(&pid) != nil {
		t.Fatal("cannot lock native account")
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, f.tokens[0], "application/json"))
		done <- rw
	}()
	profileAPIsWaitBlocked(t, f, pid)
	if _, err = blocker.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0]); err != nil {
		t.Fatal("cannot apply real late suspension")
	}
	if blocker.Commit(f.ctx) != nil {
		t.Fatal("cannot release late Account change")
	}
	defer f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
	select {
	case rw := <-done:
		if rw.Code != 401 || before != profileAPIsSnapshot(t, f, false) {
			t.Fatalf("explicit RC did not deny late account state: status=%d", rw.Code)
		}
		t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual pool RR/current gateway RC/late Account suspended ->401/no source writes")
	case <-time.After(6 * time.Second):
		t.Fatal("late Account request did not complete")
	}
}

func TestProfileAPIsActualLateCancellation(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	profileAPIsInsertIntents(t, f)
	before := profileAPIsSnapshot(t, f, false)
	beforeC := profileAPIsSnapshot(t, f, true)
	blocker, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("cannot establish cancellation barrier")
	}
	defer blocker.Rollback(context.Background())
	var pid int
	if blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.accountIDs[0]).Scan(&pid) != nil {
		t.Fatal("cannot lock owned Account")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rw := httptest.NewRecorder()
		r := privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, f.tokens[0], "application/json").WithContext(ctx)
		f.handler.ServeHTTP(rw, r)
		done <- rw
	}()
	profileAPIsWaitBlocked(t, f, pid)
	cancel()
	select {
	case rw := <-done:
		if rw.Code != 503 {
			t.Fatalf("late canceled native request status=%d", rw.Code)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("native query did not cancel")
	}
	if blocker.Commit(f.ctx) != nil {
		t.Fatal("cannot release native cancellation barrier")
	}
	if before != profileAPIsSnapshot(t, f, false) || beforeC != profileAPIsSnapshot(t, f, true) {
		t.Fatal("late canceled request wrote Profile/Intent/audit or cognition source")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY real Account lock wait ->ctx canceled->503/all owned sources unchanged")
}
