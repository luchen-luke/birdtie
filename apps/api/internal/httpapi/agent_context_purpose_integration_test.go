package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

const contextPurposeHTTPBase = "/v1/me/agent-context"
const contextPurposeHTTPMemory = "本轮本人明确记录的羽毛球偏好"
const contextPurposeHTTPUnselected = "UNSELECTED_PRIVATE_CONTEXT_MUST_NOT_LEAK"

type contextPurposeHTTPFixture struct {
	*contextBuilderHTTPFixture
	task      agentworkspace.Task
	memory    agentmemory.Record
	tie       string
	selection acb.PurposeSelection
}

func contextPurposeHTTPNative(t *testing.T) *contextPurposeHTTPFixture {
	t.Helper()
	f := &contextPurposeHTTPFixture{contextBuilderHTTPFixture: contextBuilderHTTPNative(t)}
	var installed bool
	if e := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.agent_context_purpose_bindings') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("ContextPurpose HTTP requires actual migration076")
	}
	// This cleanup runs before the existing task/city and principal cleanup.
	// It removes only this fixture's native purpose bindings and owned domain rows.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{
			`DELETE FROM agent_context_purpose_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`,
			`DELETE FROM agent_context_purpose_previews WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		} {
			if _, e := f.pool.Exec(ctx, sql, f.accountIDs); e != nil {
				t.Error("owned ContextPurpose cleanup", e)
			}
		}
	})
	f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":1,"fields":{"availability":"周末下午","languagePreferences":["中文"],"agentNotes":"`+contextPurposeHTTPUnselected+`"}}`, f.tokens[0], 200, nil)
	var memoryID string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&memoryID); e != nil {
		t.Fatal(e)
	}
	f.memory = memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", memoryHTTPList+"/"+memoryID, contextPurposeHTTPMemoryBody(t, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)), f.tokens[0], 200, nil).Body.Bytes())
	access := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	if _, e := f.store.PutOwnPolicy(f.ctx, access, agentpolicysettings.Autonomy, agentpolicysettings.PutInput{ExpectedVersion: 0, Settings: json.RawMessage(`{"level":"LEVEL_0_OBSERVE"}`), ExpiresAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}); e != nil {
		t.Fatal("native explicit policy", e)
	}
	request, e := f.store.CreateFriendRequest(f.ctx, f.accountIDs[0], f.accountIDs[1], "本人确认的本地合成好友申请")
	if e != nil {
		t.Fatal("native friend request", e)
	}
	if _, e = f.store.DecideRequest(f.ctx, f.accountIDs[1], request.ID, "accept"); e != nil {
		t.Fatal("native friend accept", e)
	}
	ties, e := f.store.ListTies(f.ctx, f.accountIDs[0])
	if e != nil || len(ties) != 1 {
		t.Fatal("native exact tie", e)
	}
	f.tie = ties[0].ID
	if _, e = f.store.SetRelationshipConsent(f.ctx, f.accountIDs[0], relationshipcontext.Consent{Enabled: true}); e != nil {
		t.Fatal(e)
	}
	f.task, e = f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.accountIDs[0], ActingUserID: f.accountIDs[0], CityID: f.city, Query: "帮我按本轮明确资料整理羽毛球安排", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{{Role: "user", Text: "合成本轮输入"}}})
	if e != nil {
		t.Fatal("native current task", e)
	}
	f.selection = acb.PurposeSelection{AgentID: f.agentIDs[0], TaskID: f.task.ID, CityID: f.city, CurrentQuery: f.task.Query, TaskUpdatedAt: f.task.UpdatedAt, ProfileFields: []string{"availability", "languagePreferences"}, MemoryIDs: []string{f.memory.ID}, PlaceIDs: []string{f.place}, ActivityIDs: []string{f.public}, RelationshipTieIDs: []string{f.tie}, PolicyFamilies: []agentpolicysettings.Family{agentpolicysettings.Autonomy}, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond).Add(4 * time.Minute)}
	return f
}

func contextPurposeHTTPDigest(t *testing.T, token string) [32]byte {
	t.Helper()
	r := privateProfileHTTPRequest("GET", "/", "", token, "application/json")
	digest, e := identityDigestFromHeader(r)
	if e != nil {
		t.Fatal(e)
	}
	return digest
}

func identityDigestFromHeader(r *http.Request) ([32]byte, error) {
	// Reuse the actual canonical bearer parser; this is not an authority adapter.
	return identity.ParseBearer(r.Header.Get("Authorization"))
}

func contextPurposeHTTPMemoryBody(t *testing.T, version int64, until time.Time) string {
	t.Helper()
	raw, e := json.Marshal(agentmemory.PutInput{ExpectedVersion: version, MemoryType: agentmemory.TypePreference, MemoryKey: "native-http-context", Summary: contextPurposeHTTPMemory, StructuredValue: json.RawMessage(`{"sport":"羽毛球","unselectedRaw":"STRUCTURED_VALUE_NOT_IN_RUNTIME"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: until})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}

func contextPurposeHTTPSelectionBody(t *testing.T, s acb.PurposeSelection) string {
	t.Helper()
	// PurposeSelection contains server-derived queryDigest; wire has exactly 12 keys.
	obj := map[string]any{"agentId": s.AgentID, "taskId": s.TaskID, "cityId": s.CityID, "currentQuery": s.CurrentQuery, "taskUpdatedAt": s.TaskUpdatedAt, "profileFields": s.ProfileFields, "memoryIds": s.MemoryIDs, "placeIds": s.PlaceIDs, "activityIds": s.ActivityIDs, "relationshipTieIds": s.RelationshipTieIDs, "policyFamilies": s.PolicyFamilies, "deadlineAt": s.DeadlineAt}
	raw, e := json.Marshal(obj)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}

func (f *contextPurposeHTTPFixture) preview(t *testing.T) acb.PurposePreview {
	t.Helper()
	w := f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", contextPurposeHTTPSelectionBody(t, f.selection), f.tokens[0], 200, nil)
	var human struct {
		Data struct {
			Review json.RawMessage `json:"review"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &human); e != nil || len(human.Data.Review) == 0 || string(human.Data.Review) == "null" {
		t.Fatal("native preview omitted concrete selected human review", e)
	}
	for _, value := range []string{"周末下午", "中文", contextPurposeHTTPMemory, f.place, f.public, f.tie, f.task.Query, "Europe/London", "LEVEL_0_OBSERVE"} {
		if !strings.Contains(string(human.Data.Review), value) {
			t.Fatal("concrete native selected value absent from preview", value)
		}
	}
	for _, value := range []string{contextPurposeHTTPUnselected, "UNREQUESTED_CONTEXT_BODY_CANARY", "UNREQUESTED_CONTEXT_SUMMARY_CANARY", `"latitude"`, `"longitude"`, `"conversation"`, `"sentMessages"`, `"authority"`, `"rowToken"`} {
		if strings.Contains(string(human.Data.Review), value) {
			t.Fatal("unselected/private internal value in human preview", value)
		}
	}
	var v struct {
		Data acb.PurposePreview `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.ID == "" || v.Data.Purpose != acb.TaskContextRead {
		t.Fatal("native preview response", e)
	}
	return v.Data
}
func (f *contextPurposeHTTPFixture) approve(t *testing.T, p acb.PurposePreview) acb.PurposeGrant {
	t.Helper()
	w := f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/approvals", `{"previewId":"`+p.ID+`"}`, f.tokens[0], 200, nil)
	var v struct {
		Data acb.PurposeGrant `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.ID == "" || v.Data.Revision != 1 {
		t.Fatal("native grant response", e)
	}
	return v.Data
}
func (f *contextPurposeHTTPFixture) runtime(t *testing.T, g acb.PurposeGrant, want int) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, f.tokens[0], want, nil)
}
func (f *contextPurposeHTTPFixture) readonlyRows(t *testing.T) string {
	t.Helper()
	var raw string
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('task',(SELECT to_jsonb(t) FROM agent_tasks t WHERE id=$1),'memories',(SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM agent_memories m WHERE owner_id=$2),'privateProfile',(SELECT to_jsonb(p) FROM agent_private_profiles p WHERE owner_id=$2),'messages',(SELECT count(*) FROM conversation_messages WHERE sender_account_id=$2),'inbox',(SELECT count(*) FROM inbox_items WHERE recipient_account_id=$2))::text`, f.task.ID, f.accountIDs[0]).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func TestContextPurposeHTTPNativeSixSourcesRuntimeAndLifecycle(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	p := f.preview(t)
	if strings.Contains(contextPurposeHTTPSelectionBody(t, p.Selection), "queryDigest") {
		t.Fatal("server digest went into wire")
	}
	g := f.approve(t, p)
	again := f.approve(t, p)
	if again.ID != g.ID || again.Revision != g.Revision || !again.CreatedAt.Equal(g.CreatedAt) {
		t.Fatal("approval replay created new lifecycle")
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, f.accountIDs[0]).Scan(&count); e != nil || count != 1 {
		t.Fatal("not unique native grant", e, count)
	}
	// Every registered Authenticate refreshes idle expiry normally; that must not invalidate a healthy approval.
	f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants/"+g.ID, "", f.tokens[0], 200, nil)
	before := f.readonlyRows(t)
	w := f.runtime(t, g, 200)
	var out struct {
		Data runtimeContextResponse `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	r := out.Data
	if r.SchemaVersion != "agent-task-context-response-v1" || r.TaskID != f.task.ID || r.City.ID != f.city || r.City.TimeZone != "Europe/London" || !strings.Contains(r.Answer, f.task.Query) || r.ModelAccess != "UNAVAILABLE" || r.MemoryPromotionAllowed {
		t.Fatal("runtime did not consume native bounded task/city")
	}
	// AGE033 Preview still displays all six exact selected families. AGE034
	// consumption filters unrelated values: this Task requests badminton, not
	// availability, language, an unrelated place or the owner's friend state.
	if len(r.Facts) != 2 || len(r.Places) != 0 || len(r.Activities) != 1 || r.Activities[0].ID != f.public || len(r.Relationships) != 0 {
		t.Fatal("exact approved context was not consumed by relevant projection", r)
	}
	for _, want := range []string{contextPurposeHTTPMemory, "自主操作边界", "LEVEL_0_OBSERVE", "仅观察", "合成羽毛球 public"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal("native selected source not consumed", want)
		}
	}
	for _, forbidden := range []string{"周末下午", "languagePreferences", "合成 Context 地点", f.tie, contextPurposeHTTPUnselected, "STRUCTURED_VALUE_NOT_IN_RUNTIME", "UNREQUESTED_CONTEXT_BODY_CANARY", "UNREQUESTED_CONTEXT_SUMMARY_CANARY", `"rowToken"`, `"latitude"`, `"longitude"`, `"conversation"`, `"sentMessages"`, `"authority"`} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("unselected/internal source leaked", forbidden)
		}
	}
	if before != f.readonlyRows(t) {
		t.Fatal("read-only runtime mutated native source or sent messages")
	}
	if !r.ExpiresAt.After(time.Now()) || r.ExpiresAt.After(g.ExpiresAt) || r.ExpiresAt.After(g.Selection.DeadlineAt) {
		t.Fatal("runtime renewed source/grant deadline")
	}
	path := contextPurposeHTTPBase + "/grants/" + g.ID
	f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	f.runtime(t, g, 403)
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY concrete human Preview retains six exact native source families; registered Runtime consumes only related data and current declared policy; single consent_grants lifecycle and zero external/model/Memory effects")
}

func TestContextPurposeHTTPNativeClosedWireAndIdentity(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	p := f.preview(t)
	g := f.approve(t, p)
	for _, token := range []string{"", "invalid", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, token, 401, nil)
	}
	for _, token := range f.tokens[1:] {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, token, 403, nil)
	}
	// The original approval is bound to its native Session, even for the same owner.
	f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, f.newSession(f.accountIDs[0], false, false), 403, nil)
	f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, f.tokens[0], 403, &f.accountIDs[2])
	for _, suffix := range []string{"?", "?approved=true"} {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime"+suffix, `{"grantId":"`+g.ID+`"}`, f.tokens[0], 400, nil)
		f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants/"+g.ID+suffix, "", f.tokens[0], 400, nil)
	}
	for _, raw := range []string{`null`, `[]`, `{"grantId":null}`, `{"GrantId":"` + g.ID + `"}`, `{"grantId":"` + g.ID + `","grantId":"` + g.ID + `"}`, `{"grantId":"` + g.ID + `","confirmed":true}`, `{"grantId":"` + g.ID + `"} {}`} {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", raw, f.tokens[0], 400, nil)
	}
	f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants/"+g.ID, `{}`, f.tokens[0], 400, nil)
	for _, field := range []string{"agentId", "taskId", "cityId", "currentQuery", "taskUpdatedAt", "profileFields", "memoryIds", "placeIds", "activityIds", "relationshipTieIds", "policyFamilies", "deadlineAt"} {
		t.Run("null_"+field, func(t *testing.T) {
			var obj map[string]any
			json.Unmarshal([]byte(contextPurposeHTTPSelectionBody(t, f.selection)), &obj)
			obj[field] = nil
			raw, _ := json.Marshal(obj)
			f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", string(raw), f.tokens[0], 400, nil)
		})
	}
	for _, field := range []string{"ownerId", "confirmed", "queryDigest", "source", "mode", "retention"} {
		var obj map[string]any
		json.Unmarshal([]byte(contextPurposeHTTPSelectionBody(t, f.selection)), &obj)
		obj[field] = "caller_is_not_authority"
		raw, _ := json.Marshal(obj)
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", string(raw), f.tokens[0], 400, nil)
	}
	for _, name := range []string{"unknown_profile", "duplicate_memory", "too_many_fields", "unknown_policy", "case_alias", "duplicate_key"} {
		t.Run(name, func(t *testing.T) {
			var obj map[string]any
			json.Unmarshal([]byte(contextPurposeHTTPSelectionBody(t, f.selection)), &obj)
			switch name {
			case "unknown_profile":
				obj["profileFields"] = []string{"PRIVATE_ALL"}
			case "duplicate_memory":
				obj["memoryIds"] = []string{f.memory.ID, f.memory.ID}
			case "too_many_fields":
				obj["profileFields"] = []string{"availability", "languagePreferences", "agentNotes", "socialPreferences"}
			case "unknown_policy":
				obj["policyFamilies"] = []string{"EVERYTHING"}
			case "case_alias":
				obj["AgentId"] = obj["agentId"]
				delete(obj, "agentId")
			}
			raw, _ := json.Marshal(obj)
			body := string(raw)
			if name == "duplicate_key" {
				body = `{"agentId":"` + f.agentIDs[0] + `",` + strings.TrimPrefix(body, "{")
			}
			f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", body, f.tokens[0], 400, nil)
		})
	}
	f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/approvals", `{"previewId":"`+p.ID+`"}`, f.tokens[1], 403, nil)
	// Ordinary profile_read cannot inherit the task source binding.
	old, e := f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	f.runtime(t, acb.PurposeGrant{ID: old.ID}, 403)
}

func TestContextPurposeHTTPNativeChangedPreviewCannotApprove(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	p := f.preview(t)
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 1, time.Now().UTC().Truncate(time.Microsecond).Add(2*time.Hour)), f.tokens[0], 200, nil)
	f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/approvals", `{"previewId":"`+p.ID+`"}`, f.tokens[0], 403, nil)
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, f.accountIDs[0]).Scan(&count); e != nil || count != 0 {
		t.Fatal("stale preview minted consent", e, count)
	}
}

func TestContextPurposeHTTPNativeCurrentChangesInvalidateApproval(t *testing.T) {
	for _, name := range []string{"profile_replace", "profile_clear", "memory_replace", "memory_source_aba", "memory_delete", "task_update", "task_source_aba", "account_aba", "agent_aba", "tie_removed", "tie_source_aba", "relationship_consent_revoke", "peer_block", "city_withdraw", "place_withdraw", "activity_cancel", "memory_expiry"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeHTTPNative(t)
			if name == "memory_expiry" {
				f.memory = memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 1, time.Now().UTC().Truncate(time.Microsecond).Add(time.Second)), f.tokens[0], 200, nil).Body.Bytes())
			}
			g := f.approve(t, f.preview(t))
			switch name {
			case "profile_replace":
				f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":2,"fields":{"availability":"另一个未批准时间"}}`, f.tokens[0], 200, nil)
			case "profile_clear":
				f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":2,"fields":{}}`, f.tokens[0], 200, nil)
			case "memory_replace":
				f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 1, time.Now().UTC().Truncate(time.Microsecond).Add(2*time.Hour)), f.tokens[0], 200, nil)
			case "memory_source_aba":
				f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 1, time.Now().UTC().Truncate(time.Microsecond).Add(2*time.Hour)), f.tokens[0], 200, nil)
				f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 2, f.memory.ValidUntil), f.tokens[0], 200, nil)
			case "memory_delete":
				f.request(t, f.handler, "DELETE", memoryHTTPList+"/"+f.memory.ID, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
			case "task_update":
				f.task.Conversation = append(f.task.Conversation, agentworkspace.Message{Role: "user", Text: "新的未批准一轮"})
				if _, e := f.store.UpdateTask(f.ctx, f.task); e != nil {
					t.Fatal(e)
				}
			case "task_source_aba":
				original := f.task
				f.task.Conversation = append(f.task.Conversation, agentworkspace.Message{Role: "user", Text: "native changed version then restored content"})
				if _, e := f.store.UpdateTask(f.ctx, f.task); e != nil {
					t.Fatal(e)
				}
				if _, e := f.store.UpdateTask(f.ctx, original); e != nil {
					t.Fatal(e)
				}
			case "account_aba":
				f.exec(`UPDATE accounts SET status='suspended',updated_at=clock_timestamp() WHERE id=$1`, f.accountIDs[0])
				f.exec(`UPDATE accounts SET status='active',updated_at=clock_timestamp() WHERE id=$1`, f.accountIDs[0])
			case "agent_aba":
				f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			case "tie_removed":
				if e := f.store.RemoveTie(f.ctx, f.accountIDs[0], f.tie); e != nil {
					t.Fatal(e)
				}
			case "tie_source_aba":
				if e := f.store.RemoveTie(f.ctx, f.accountIDs[0], f.tie); e != nil {
					t.Fatal(e)
				}
				// Native fixture structural ABA, not a public restore capability.
				f.exec(`UPDATE person_ties SET status='active',updated_at=clock_timestamp() WHERE id=$1`, f.tie)
			case "relationship_consent_revoke":
				if _, e := f.store.SetRelationshipConsent(f.ctx, f.accountIDs[0], relationshipcontext.Consent{Enabled: false}); e != nil {
					t.Fatal(e)
				}
			case "peer_block":
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
			case "city_withdraw":
				f.exec(`UPDATE cities SET publication_status='draft',updated_at=clock_timestamp() WHERE id=$1`, f.city)
			case "place_withdraw":
				f.exec(`UPDATE places SET publication_status='draft',updated_at=clock_timestamp() WHERE id=$1`, f.place)
			case "activity_cancel":
				f.exec(`UPDATE activities SET cancelled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, f.public)
			case "memory_expiry":
				contextPurposeHTTPWaitUntil(t, f, g.ExpiresAt)
			}
			w := f.runtime(t, g, 403)
			if strings.Contains(w.Body.String(), contextPurposeHTTPMemory) || strings.Contains(w.Body.String(), "周末下午") {
				t.Fatal("invalidated payload escaped")
			}
		})
	}
}

func TestContextPurposeHTTPNativeExactSelectionAndForeignSources(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	minimal := f.selection
	minimal.ProfileFields = []string{"availability"}
	minimal.MemoryIDs = []string{}
	minimal.PlaceIDs = []string{}
	minimal.ActivityIDs = []string{}
	minimal.RelationshipTieIDs = []string{}
	minimal.PolicyFamilies = []agentpolicysettings.Family{}
	w := f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", contextPurposeHTTPSelectionBody(t, minimal), f.tokens[0], 200, nil)
	var p struct {
		Data acb.PurposePreview `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Data.Review.Profile) != 1 || string(p.Data.Review.Profile["availability"]) != `"周末下午"` || len(p.Data.Sources) != 3 {
		t.Fatal("exact selected human preview changed")
	}
	g := f.approve(t, p.Data)
	w = f.runtime(t, g, 200)
	var response struct {
		Data runtimeContextResponse `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if len(response.Data.Facts) != 0 || len(response.Data.Places)+len(response.Data.Activities)+len(response.Data.Relationships) != 0 || len(response.Data.Sources) != 2 {
		t.Fatal("unrelated selected field expanded into Runtime")
	}
	for _, forbidden := range []string{contextPurposeHTTPMemory, contextPurposeHTTPUnselected, "languagePreferences", f.place, f.public, f.tie} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("minimal selection expanded", forbidden)
		}
	}
	var foreignID string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&foreignID); e != nil {
		t.Fatal(e)
	}
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+foreignID, contextPurposeHTTPMemoryBody(t, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)), f.tokens[1], 200, nil)
	for _, name := range []string{"foreign_agent", "foreign_memory", "private_activity", "old_task_version", "expired_deadline"} {
		t.Run(name, func(t *testing.T) {
			s := f.selection
			want := 403
			switch name {
			case "foreign_agent":
				s.AgentID = f.agentIDs[1]
			case "foreign_memory":
				s.MemoryIDs = []string{foreignID}
			case "private_activity":
				s.ActivityIDs = []string{f.private}
			case "old_task_version":
				s.TaskUpdatedAt = s.TaskUpdatedAt.Add(-time.Microsecond)
			case "expired_deadline":
				s.DeadlineAt = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
				want = 409
			}
			f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", contextPurposeHTTPSelectionBody(t, s), f.tokens[0], want, nil)
		})
	}
}

func contextPurposeHTTPWaitUntil(t *testing.T, f *contextPurposeHTTPFixture, deadline time.Time) {
	t.Helper()
	for i := 0; i < 400; i++ {
		var now time.Time
		if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(deadline) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("native deadline did not elapse")
}

// Embedded real Store preserves all permission/source behavior. Only pool
// acquisition is held after the first native machine Build has committed.
type contextPurposeHTTPWait struct {
	*postgres.Store
	pool         *pgxpool.Pool
	held         *pgxpool.Conn
	calls        int
	finalStarted chan struct{}
}

func (s *contextPurposeHTTPWait) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	s.calls++
	if s.calls == 2 {
		close(s.finalStarted)
	}
	b, e := s.Store.BuildOwnAgentContext(ctx, r)
	if e == nil && s.calls == 1 {
		s.held, e = s.pool.Acquire(ctx)
	}
	return b, e
}

func TestContextPurposeHTTPNativeFinalPoolWait(t *testing.T) {
	for _, name := range []string{"session_revoke", "session_expiry", "grant_expiry", "source_change", "healthy"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeHTTPNative(t)
			if name == "grant_expiry" {
				f.selection.DeadlineAt = time.Now().UTC().Truncate(time.Microsecond).Add(2 * time.Second)
			}
			if name == "session_expiry" {
				f.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET expires_at=stamp.at+interval '2 seconds',idle_expires_at=stamp.at+interval '2 seconds' FROM stamp WHERE account_id=$1`, f.accountIDs[0])
			}
			g := f.approve(t, f.preview(t))
			cfg := f.pool.Config()
			cfg.MaxConns = 1
			cfg.MinConns = 0
			pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			wait := &contextPurposeHTTPWait{Store: postgres.New(pool, false), pool: pool, finalStarted: make(chan struct{})}
			defer func() {
				if wait.held != nil {
					wait.held.Release()
				}
			}()
			h := contextBuilderHTTPNew(wait, f.store, f.store)
			w := httptest.NewRecorder()
			req := privateProfileHTTPRequest("POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, f.tokens[0], "application/json")
			req.Header.Set("X-Request-ID", "context-purpose-final-wait-"+name)
			done := make(chan struct{})
			go func() { h.ServeHTTP(w, req); close(done) }()
			select {
			case <-wait.finalStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("registered runtime did not reach final native Builder")
			}
			if wait.held == nil || pool.Stat().AcquiredConns() != 1 {
				t.Fatal("not actual final native pool wait")
			}
			select {
			case <-done:
				t.Fatal("payload escaped held final pool")
			case <-time.After(40 * time.Millisecond):
			}
			switch name {
			case "session_revoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
			case "source_change":
				f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, contextPurposeHTTPMemoryBody(t, 1, time.Now().UTC().Truncate(time.Microsecond).Add(2*time.Hour)), f.tokens[0], 200, nil)
			case "session_expiry", "grant_expiry":
				contextPurposeHTTPWaitUntil(t, f, g.ExpiresAt)
			}
			wait.held.Release()
			wait.held = nil
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("final reader did not finish")
			}
			want := 403
			if name == "healthy" {
				want = 200
			}
			if name == "grant_expiry" {
				want = 409
			}
			if w.Code != want {
				t.Fatal("final pool boundary", name, w.Code, w.Body.String())
			}
			if want != 200 && strings.Contains(w.Body.String(), contextPurposeHTTPMemory) {
				t.Fatal("late revoked payload leak")
			}
			t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual final native pool wait %s status=%d", name, w.Code)
		})
	}
}
