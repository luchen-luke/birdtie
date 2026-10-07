package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func creationNativeWire(t *testing.T, label, method, path, body string, w *httptest.ResponseRecorder) {
	t.Helper()
	v := map[string]any{"scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT", "method": method, "path": path, "requestBody": body, "status": w.Code, "responseBody": w.Body.String()}
	raw, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	t.Log("SOCIAL_INTENT_CREATION_NATIVE_WIRE " + label + " " + string(raw))
	if dir := os.Getenv("BIRDTIE_SOCIAL_INTENT_CREATION_WIRE_DIR"); dir != "" {
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		for i := 0; ; i++ {
			file, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%s-%s-%03d.json", t.Name(), label, i)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if os.IsExist(e) {
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			_, e = file.Write(raw)
			closeErr := file.Close()
			if e != nil || closeErr != nil {
				t.Fatal("native wire evidence write", e, closeErr)
			}
			break
		}
	}
}

// Actual registered API and actual PostgreSQL with disposable synthetic owners.
// This is not a real IdP, real social activity, deployment or pilot result.
func creationHTTPNative(t *testing.T) (*privateProfileHTTPDBFixture, http.Handler) {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	for i := 0; i < 2; i++ {
		if _, e := f.store.EnsureAgentProfile(f.ctx, f.agentIDs[i], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[i]}); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var receipts bool
		if e := f.pool.QueryRow(ctx, `SELECT to_regclass('public.social_intent_creation_receipts') IS NOT NULL`).Scan(&receipts); e != nil {
			t.Error(e)
			return
		}
		if receipts {
			if _, e := f.pool.Exec(ctx, `DELETE FROM social_intent_creation_receipts WHERE owner_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
		if _, e := f.pool.Exec(ctx, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
		if _, e := f.pool.Exec(ctx, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	h := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
	return f, h
}
func TestSocialIntentCreationHTTPNativeOperationKey(t *testing.T) {
	f, h := creationHTTPNative(t)
	var operation string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&operation); e != nil {
		t.Fatal(e)
	}
	input := map[string]any{"operationId": operation, "type": "FIND_ACTIVITY", "title": "合成私人意图保存回执", "constraints": map[string]any{}, "audience": "PRIVATE", "modality": "ONLINE", "expiresAt": time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)}
	body, _ := json.Marshal(input)
	call := func() *httptest.ResponseRecorder {
		q := httptest.NewRequest("POST", "/v1/me/social-intents", strings.NewReader(string(body)))
		q.Header.Set("Authorization", "Bearer "+f.tokens[0])
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w
	}
	first, second := call(), call()
	creationNativeWire(t, "create", "POST", "/v1/me/social-intents", string(body), first)
	creationNativeWire(t, "replay", "POST", "/v1/me/social-intents", string(body), second)
	if first.Code != 201 || second.Code != 200 {
		t.Fatalf("durable operation first=%d second=%d first_body=%s second_body=%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var a, b map[string]any
	if json.Unmarshal(first.Body.Bytes(), &a) != nil || json.Unmarshal(second.Body.Bytes(), &b) != nil {
		t.Fatal("invalid receipt envelope")
	}
	aa, aok := a["data"].(map[string]any)
	bb, bok := b["data"].(map[string]any)
	if !aok || !bok || aa["intentId"] == nil || aa["intentId"] != bb["intentId"] || aa["operationId"] != operation || bb["operationId"] != operation {
		t.Fatal("same operation did not return same actual ID", a, b)
	}
	var intentCount, auditCount int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM social_intents WHERE creator_account_id=$1`, f.accountIDs[0]).Scan(&intentCount); e != nil {
		t.Fatal(e)
	}
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='social_intent' AND action='create'`, f.accountIDs[0]).Scan(&auditCount); e != nil {
		t.Fatal(e)
	}
	if intentCount != 1 || auditCount != 1 {
		t.Fatalf("operation effect count intent=%d audit=%d", intentCount, auditCount)
	}
	// The immutable creation receipt must not masquerade as the entity's newer
	// state. These are actual native updates and actual registered API replays.
	f.exec(`UPDATE social_intents SET title='原实体已由本人后续修改',status='CANCELLED',updated_at=clock_timestamp() WHERE id=$1`, aa["intentId"])
	changed := call()
	creationNativeWire(t, "current-cancelled", "POST", "/v1/me/social-intents", string(body), changed)
	if changed.Code != 200 || !strings.Contains(changed.Body.String(), `"CANCELLED"`) || !strings.Contains(changed.Body.String(), "原实体已由本人后续修改") {
		t.Fatal("receipt hid current entity state", changed.Code, changed.Body.String())
	}
	f.exec(`UPDATE social_intents SET status='DRAFT',expires_at=created_at+interval '1 microsecond',updated_at=clock_timestamp() WHERE id=$1`, aa["intentId"])
	expired := call()
	creationNativeWire(t, "current-expired", "POST", "/v1/me/social-intents", string(body), expired)
	if expired.Code != 200 || !strings.Contains(expired.Body.String(), `"EXPIRED"`) {
		t.Fatal("committed expired operation retried new creation", expired.Code, expired.Body.String())
	}
	for _, w := range []*httptest.ResponseRecorder{changed, expired} {
		var decoded map[string]any
		if json.Unmarshal(w.Body.Bytes(), &decoded) != nil || (decoded["data"].(map[string]any))["recordedAt"] != aa["recordedAt"] {
			t.Fatal("immutable receipt timestamp changed")
		}
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY same operation: one native intent, one native audit")
}

func TestSocialIntentCreationHTTPNativeLegacySourceAndReadBoundaries(t *testing.T) {
	f, h := creationHTTPNative(t)
	task, e := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.accountIDs[0], ActingUserID: f.accountIDs[0], CityID: "aberdeen-gb", Query: "合成来源已完成查询", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if e != nil {
		t.Fatal(e)
	}
	old := map[string]any{"type": "FIND_ACTIVITY", "title": "原来源私人草稿", "constraints": map[string]any{}, "audience": "PRIVATE", "modality": "ONLINE", "expiresAt": time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)}
	sourcePath := "/v1/me/agent-tasks/" + task.ID + "/social-intent-drafts"
	call := func(method, path string, payload any, who int, org bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		q := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		q.Header.Set("Content-Type", "application/json")
		if who >= 0 {
			q.Header.Set("Authorization", "Bearer "+f.tokens[who])
		}
		if org {
			q.Header.Set("X-Birdtie-Organization-Workspace", f.accountIDs[2])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		creationNativeWire(t, strings.ToLower(method)+"-"+strings.ReplaceAll(strings.Trim(path, "/"), "/", "-")+"-"+strconv.Itoa(who)+"-"+map[bool]string{true: "org", false: "person"}[org], method, path, string(raw), w)
		return w
	}
	first := call("POST", sourcePath, map[string]any{"confirmed": true, "draft": old}, 0, false)
	if first.Code != 201 {
		t.Fatal("legacy task create", first.Code, first.Body.String())
	}
	var original struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(first.Body.Bytes(), &original) != nil || original.Data.ID == "" {
		t.Fatal("legacy original ID")
	}
	if w := call("POST", sourcePath, map[string]any{"confirmed": true, "draft": old}, 0, false); w.Code != 409 || !strings.Contains(w.Body.String(), "intent_already_created_from_task") {
		t.Fatal("legacy exact conflict", w.Code, w.Body.String())
	}
	readPath := "/v1/me/agent-tasks/" + task.ID + "/social-intent-draft"
	if w := call("GET", readPath, nil, 0, false); w.Code != 200 || !strings.Contains(w.Body.String(), original.Data.ID) || !strings.Contains(w.Body.String(), task.ID) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("source original owner receipt", w.Code, w.Body.String())
	}
	for _, x := range []struct {
		who  int
		org  bool
		want int
	}{{1, false, 404}, {2, false, 403}, {0, true, 403}, {-1, false, 401}} {
		w := call("GET", readPath, nil, x.who, x.org)
		if w.Code != x.want {
			t.Fatal("source owner/current workspace", x, w.Code, w.Body.String())
		}
	}
	var operation string
	if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&operation); e != nil {
		t.Fatal(e)
	}
	old["operationId"] = operation
	old["title"] = "这次修改并未再次保存"
	w := call("POST", sourcePath, map[string]any{"confirmed": true, "draft": old}, 0, false)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"NO_EFFECT"`) || !strings.Contains(w.Body.String(), original.Data.ID) || strings.Contains(w.Body.String(), "这次修改并未再次保存") {
		t.Fatal("source conflict authority falsified new content", w.Code, w.Body.String())
	}
	receiptPath := "/v1/me/social-intent-creations/" + operation
	if w = call("GET", receiptPath, nil, 0, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"SOURCE_ALREADY_EXISTS"`) {
		t.Fatal("persisted source refusal", w.Code, w.Body.String())
	}
	for _, x := range []struct {
		who  int
		org  bool
		want int
	}{{1, false, 404}, {2, false, 403}, {0, true, 403}, {-1, false, 401}} {
		w = call("GET", receiptPath, nil, x.who, x.org)
		if w.Code != x.want {
			t.Fatal("receipt owner/current workspace", x, w.Code)
		}
	}
	old["title"] = "同操作却换内容"
	if w = call("POST", sourcePath, map[string]any{"confirmed": true, "draft": old}, 0, false); w.Code != 409 || !strings.Contains(w.Body.String(), "social_intent_operation_changed") {
		t.Fatal("body/key mismatch", w.Code, w.Body.String())
	}
	delete(old, "operationId")
	a, b := call("POST", "/v1/me/social-intents", old, 0, false), call("POST", "/v1/me/social-intents", old, 0, false)
	if a.Code != 201 || b.Code != 201 {
		t.Fatal("old standalone creation must keep exact legacy response", a.Code, b.Code)
	}
}
func TestSocialIntentCreationHTTPNativeCommitDisconnectAndNewServerRecovery(t *testing.T) {
	f, h := creationHTTPNative(t)
	var operation string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&operation); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"operationId": operation, "type": "FIND_COMPANION", "title": "合成断线前已提交私人草稿", "constraints": map[string]any{}, "audience": "PRIVATE", "modality": "ONLINE", "expiresAt": time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)})
	committed, release := make(chan struct{}), make(chan struct{})
	wire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if r.Method == "POST" {
			close(committed)
			<-release
		}
	}))
	defer wire.Close()
	ctx, cancel := context.WithCancel(f.ctx)
	q, e := http.NewRequestWithContext(ctx, "POST", wire.URL+"/v1/me/social-intents", strings.NewReader(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	q.Header.Set("Authorization", "Bearer "+f.tokens[0])
	q.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		r, e := http.DefaultClient.Do(q)
		if r != nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		done <- e
	}()
	select {
	case <-committed:
	case <-time.After(5 * time.Second):
		t.Fatal("actual API commit not reached")
	}
	cancel()
	close(release)
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("native transport disconnect was not observed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transport cancellation did not finish")
	}
	// A new server/store instance reads the durable original operation. No title
	// match or in-process receipt cache participates in this recovery.
	fresh := postgres.New(f.pool, false)
	h = New(fresh, fresh, nil, fresh, fresh, fresh, fresh, fresh, fresh, fresh, fresh, nil, false, nil, nil, nil)
	q = httptest.NewRequest("GET", "/v1/me/social-intent-creations/"+operation, nil)
	q.Header.Set("Authorization", "Bearer "+f.tokens[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, q)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"COMMITTED"`) || !strings.Contains(w.Body.String(), operation) {
		t.Fatal("unknown transport original receipt recovery", w.Code, w.Body.String())
	}
	creationNativeWire(t, "recover-after-native-disconnect", "GET", "/v1/me/social-intent-creations/"+operation, "", w)
	q = httptest.NewRequest("POST", "/v1/me/social-intents", strings.NewReader(string(raw)))
	q.Header.Set("Authorization", "Bearer "+f.tokens[0])
	q.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, q)
	if w.Code != 200 {
		t.Fatal("explicit same operation retry after disconnect", w.Code, w.Body.String())
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM social_intents WHERE creator_account_id=$1`, f.accountIDs[0]).Scan(&count); e != nil || count != 1 {
		t.Fatal("disconnect duplicated entity", count, e)
	}
	t.Log("LOCAL_DISPOSPOSABLE_SYNTHETIC_ONLY real socket cancelled after actual native commit; new server exact operation receipt; one entity")
}
