package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func memoryDBHTTPBody(t *testing.T, expected int64, key, summary string) string {
	t.Helper()
	body, err := json.Marshal(agentmemory.PutInput{ExpectedVersion: expected, MemoryType: agentmemory.TypePreference,
		MemoryKey: key, Summary: summary, StructuredValue: json.RawMessage(`{"sport":"羽毛球"}`), Visibility: agentmemory.VisibilityAgentOnly,
		ValidUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func memoryDBHTTPRecord(t *testing.T, body []byte) agentmemory.Record {
	t.Helper()
	var envelope struct {
		Data agentmemory.Record `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || agentmemory.ValidateRecord(envelope.Data) != nil {
		t.Fatal("real Memory response violated schema")
	}
	return envelope.Data
}
func TestAgentMemoryHTTPRealPersistenceAndIsolation(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var installed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.agent_memories') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("Memory HTTP requires migration056")
	}
	var id string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := memoryHTTPList + "/" + id
	body := memoryDBHTTPBody(t, 0, "badminton", memoryHTTPCanary)
	var profileBefore, profileAfter string
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, f.accountIDs[0]).Scan(&profileBefore); err != nil {
		t.Fatal(err)
	}
	first := f.request(t, f.handler, "GET", memoryHTTPList, "", f.tokens[0], 200, nil)
	var list struct {
		Data []agentmemory.Record `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &list); err != nil || len(list.Data) != 0 {
		t.Fatal("new owner's native Memory list not empty")
	}
	createdResponse := f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil)
	created := memoryDBHTTPRecord(t, createdResponse.Body.Bytes())
	if created.Version != 1 || created.OwnerID != f.accountIDs[0] || created.AgentID != f.agentIDs[0] || created.SourceType != agentmemory.SourceExplicit || created.Visibility != agentmemory.VisibilityAgentOnly {
		t.Fatal("real Memory binding/source invalid")
	}
	retried := memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil).Body.Bytes())
	if !reflect.DeepEqual(created, retried) {
		t.Fatal("identical create retry altered Memory")
	}
	// A newly composed API/store instance loads committed persistent data.
	reopened := postgres.New(f.pool, false)
	handler := privateProfileHTTPNew(reopened, reopened)
	rw := f.request(t, handler, "GET", memoryHTTPList, "", f.tokens[0], 200, nil)
	if err := json.Unmarshal(rw.Body.Bytes(), &list); err != nil || len(list.Data) != 1 || !reflect.DeepEqual(created, list.Data[0]) {
		t.Fatal("new API instance lost committed Memory")
	}
	peerList := f.request(t, handler, "GET", memoryHTTPList, "", f.tokens[1], 200, nil)
	if err := json.Unmarshal(peerList.Body.Bytes(), &list); err != nil || len(list.Data) != 0 {
		t.Fatal("foreign owner saw Memory")
	}
	f.request(t, handler, "PUT", path, memoryDBHTTPBody(t, 1, "badminton", "foreign overwrite"), f.tokens[1], 404, nil)
	f.request(t, handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[1], 404, nil)
	updateBody := memoryDBHTTPBody(t, 1, "badminton", "周末羽毛球，本人明确声明")
	updated := memoryDBHTTPRecord(t, f.request(t, handler, "PUT", path, updateBody, f.tokens[0], 200, nil).Body.Bytes())
	if updated.Version != 2 || updated.CreatedAt != created.CreatedAt || updated.Summary == created.Summary {
		t.Fatal("real explicit CAS replacement invalid")
	}
	f.request(t, handler, "PUT", path, body, f.tokens[0], 409, nil)
	retryUpdate := memoryDBHTTPRecord(t, f.request(t, handler, "PUT", path, updateBody, f.tokens[0], 200, nil).Body.Bytes())
	if !reflect.DeepEqual(updated, retryUpdate) {
		t.Fatal("identical edit retry altered Memory")
	}
	f.request(t, handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[0], 409, nil)
	deleted := memoryDBHTTPRecord(t, f.request(t, handler, "DELETE", path, `{"expectedVersion":2}`, f.tokens[0], 200, nil).Body.Bytes())
	if deleted.Version != 3 || deleted.Status != agentmemory.StatusDeleted || deleted.Summary != "" || string(deleted.StructuredValue) != "{}" {
		t.Fatal("real Memory delete did not scrub/tombstone")
	}
	deleteRetry := memoryDBHTTPRecord(t, f.request(t, handler, "DELETE", path, `{"expectedVersion":2}`, f.tokens[0], 200, nil).Body.Bytes())
	if !reflect.DeepEqual(deleted, deleteRetry) {
		t.Fatal("delete retry changed tombstone")
	}
	f.request(t, handler, "PUT", path, memoryDBHTTPBody(t, 3, "badminton", memoryHTTPCanary), f.tokens[0], 409, nil)
	final := f.request(t, handler, "GET", memoryHTTPList, "", f.tokens[0], 200, nil)
	if err := json.Unmarshal(final.Body.Bytes(), &list); err != nil || len(list.Data) != 0 {
		t.Fatal("deleted payload retained in owner list")
	}
	var metadataVersion int64
	if err := f.pool.QueryRow(f.ctx, `SELECT profile_version FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0]).Scan(&metadataVersion); err != nil || metadataVersion != 1 {
		t.Fatal("Memory modified native Profile version")
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, f.accountIDs[0]).Scan(&profileAfter); err != nil || profileAfter != profileBefore {
		t.Fatal("Memory changed ordinary user Profile")
	}
	var privateCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_private_profiles WHERE agent_id=$1`, f.agentIDs[0]).Scan(&privateCount); err != nil || privateCount != 0 {
		t.Fatal("Memory fabricated private Profile")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY real native HTTP create/retry/CAS/recomposition/scrub; original Profile unchanged")
}

func TestAgentMemoryHTTPRealIdentityAndStrictInput(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var id string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := memoryHTTPList + "/" + id
	body := memoryDBHTTPBody(t, 0, "strict", memoryHTTPCanary)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, c := range []struct {
			name, token string
			status      int
		}{
			{"anonymous", "", 401}, {"expired", f.newSession(f.accountIDs[0], true, false), 401},
			{"revoked", f.newSession(f.accountIDs[0], false, true), 401}, {"organization", f.tokens[2], 403}, {"business", f.tokens[3], 403},
		} {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				target := path
				input := body
				if method == "GET" {
					target = memoryHTTPList
					input = ""
				}
				if method == "DELETE" {
					input = `{"expectedVersion":1}`
				}
				f.request(t, f.handler, method, target, input, c.token, c.status, nil)
			})
		}
		workspace := ""
		target := path
		input := body
		if method == "GET" {
			target = memoryHTTPList
			input = ""
		}
		if method == "DELETE" {
			input = `{"expectedVersion":1}`
		}
		f.request(t, f.handler, method, target, input, f.tokens[0], 403, &workspace)
	}
	for _, invalid := range []string{`{"expectedVersion":0,"sourceType":"INFERRED"}`, `{"expectedVersion":0,"ownerId":"` + f.accountIDs[1] + `"}`, `{"expectedVersion":0,"confirmed":true}`, `{"expectedVersion":0,"expectedVersion":1}`} {
		f.request(t, f.handler, "PUT", path, invalid, f.tokens[0], 400, nil)
	}
	for _, number := range []string{"1e64", "1e100", "1e308", "1e-100"} {
		t.Run("stored_numeric_bound_"+number, func(t *testing.T) {
			input := strings.Replace(body, `{"sport":"羽毛球"}`, `{"quantity":`+number+`}`, 1)
			if input == body {
				t.Fatal("numeric HTTP fixture failed to replace structured value")
			}
			f.request(t, f.handler, "PUT", path, input, f.tokens[0], 400, nil)
		})
	}
	f.request(t, f.handler, "GET", memoryHTTPList+"?agentId="+f.agentIDs[1], "", f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", memoryHTTPList, body, f.tokens[0], 400, nil)
	f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		t.Run(method+"/inactive_agent", func(t *testing.T) {
			target := path
			input := body
			if method == http.MethodGet {
				target = memoryHTTPList
				input = ""
			}
			if method == http.MethodDelete {
				input = `{"expectedVersion":1}`
			}
			f.request(t, f.handler, method, target, input, f.tokens[0], 403, nil)
		})
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, f.accountIDs[0]).Scan(&count); err != nil || count != 0 {
		t.Fatal("denied requests produced Memory")
	}
}
