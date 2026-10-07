package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

type memoryAPIEncodedNativeStore struct {
	*postgres.Store
	beforeRevalidate func() error
	called           bool
}

func (s *memoryAPIEncodedNativeStore) RevalidateOwnMemoryDetail(ctx context.Context, a agentprofile.PrivateAccess, p agentmemory.DetailProjection) error {
	s.called = true
	if e := s.beforeRevalidate(); e != nil {
		return e
	}
	return s.Store.RevalidateOwnMemoryDetail(ctx, a, p)
}

func memoryAPIHTTPFullPairs(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var rows string
	q := `CREATE OR REPLACE FUNCTION pg_temp.memory_api_pairs() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE r record; v jsonb; o jsonb:='{}'; BEGIN FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(jsonb_build_object(''row'',to_jsonb(t),''xmin'',t.xmin::text) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',r.tablename) INTO v; o:=o||jsonb_build_object(r.tablename,v); END LOOP; RETURN o; END $$;`
	// A single acquired connection keeps the temporary helper local. All public
	// rows and epochs, including original correction ledgers, are compared.
	c, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	if _, e = c.Exec(f.ctx, q); e != nil {
		t.Fatal(e)
	}
	if e = c.QueryRow(f.ctx, `SELECT pg_temp.memory_api_pairs()::text`).Scan(&rows); e != nil {
		t.Fatal(e)
	}
	return rows
}

func TestMemoryAPIHTTPNativeRegisteredFiveOperationsAndRecovery(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	f := privateProfileHTTPDBNew(t)
	t.Logf("MEMORY_API owned registered HTTP database %s", f.pool.Config().ConnConfig.Database)
	var id, operation string
	if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&id, &operation) != nil {
		t.Fatal("owned ids")
	}
	in := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "registered.memory.api", Summary: "合成本人当前原文-069", StructuredValue: json.RawMessage(`{"human":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	// Retain original list/PUT/DELETE transport and stable identity.
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+id, enrichmentHTTPJSON(t, in), f.tokens[0], 200, nil)
	f.request(t, f.handler, "GET", memoryHTTPList, "", f.tokens[0], 200, nil)
	w := f.request(t, f.handler, "GET", memoryHTTPList+"/"+id, "", f.tokens[0], 200, nil)
	var detail struct {
		Data struct {
			SchemaVersion string                   `json:"schemaVersion"`
			Target        agentmemory.DetailTarget `json:"target"`
			Memory        *agentmemory.Record      `json:"memory"`
			ModelAccess   bool                     `json:"modelAccess"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Data.Target.ID != id || detail.Data.Target.Version != 1 || detail.Data.Memory == nil || detail.Data.Memory.Summary != in.Summary || detail.Data.ModelAccess || strings.Contains(w.Body.String(), "proof") {
		t.Fatal("actual native detail envelope")
	}
	f.request(t, f.handler, "GET", memoryHTTPList+"/"+id, "", f.tokens[1], 404, nil)
	f.request(t, f.handler, "GET", memoryHTTPList+"/"+id, "{}", f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", memoryHTTPList+"/"+id+"?", "", f.tokens[0], 400, nil)
	ws := ""
	f.request(t, f.handler, "GET", memoryHTTPList+"/"+id, "", f.tokens[0], 403, &ws)
	in.ExpectedVersion = 1
	in.Summary = "合成本人第二版-069"
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+id, enrichmentHTTPJSON(t, in), f.tokens[0], 200, nil)
	cin := mc.Input{ID: operation, TargetKind: "MEMORY", TargetID: id, ExpectedVersion: 2, Action: "REJECT"}
	w = f.request(t, f.handler, "POST", memoryCorrectionPath+"/previews", enrichmentHTTPJSON(t, cin), f.tokens[0], 200, nil)
	var p mc.Preview
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || mc.ValidatePreview(p) != nil {
		t.Fatal("original native specific preview")
	}
	raw := enrichmentHTTPJSON(t, agentmemory.RejectInput{OperationID: p.ID, PlanDigest: p.PlanDigest})
	f.request(t, f.handler, "POST", memoryHTTPList+"/"+privateProfileHTTPForeign+"/reject", raw, f.tokens[0], 409, nil)
	f.request(t, f.handler, "POST", memoryHTTPList+"/"+id+"/reject", `{"confirmed":true}`, f.tokens[0], 400, nil)
	w = f.request(t, f.handler, "POST", memoryHTTPList+"/"+id+"/reject", raw, f.tokens[0], 200, nil)
	var receipt mc.Receipt
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || mc.ValidateReceipt(receipt) != nil || receipt.State != "COMMITTED" || receipt.Action != "REJECT" || receipt.Target.Kind != "MEMORY" || receipt.Target.ID != id || receipt.ResultMemoryVersion == nil || *receipt.ResultMemoryVersion != 3 || strings.Contains(w.Body.String(), in.Summary) || strings.Contains(w.Body.String(), `"data":`) {
		t.Fatal("actual original bare receipt")
	}
	f.request(t, f.handler, "POST", memoryHTTPList+"/"+id+"/reject", raw, f.tokens[0], 200, nil)
	var count int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE resource_type='agent_memory_correction' AND resource_id=$1`, p.ID).Scan(&count) != nil || count != 1 {
		t.Fatal("path adapter audit repeated")
	}
	w = f.request(t, f.handler, "GET", memoryHTTPList+"/"+id, "", f.tokens[0], 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Data.Target.Status != agentmemory.StatusDeleted || detail.Data.Memory != nil || strings.Contains(w.Body.String(), "合成本人") {
		t.Fatal("deleted detail exposed old contents")
	}
	next := f.newSession(f.accountIDs[0], false, false)
	f.request(t, f.handler, "POST", memoryHTTPList+"/"+id+"/reject", raw, next, 403, nil)
	w = f.request(t, f.handler, "GET", memoryCorrectionPath+"/"+p.ID, "", next, 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.State != "COMMITTED" {
		t.Fatal("new Session original metadata unavailable")
	}
	// Independent original DELETE route remains present; a second object is not
	// aliasing the rejected Memory or its old concrete operation.
	var deleteID string
	if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&deleteID) != nil {
		t.Fatal("owned id")
	}
	in.ExpectedVersion = 0
	in.MemoryKey = "registered.original.delete"
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+deleteID, enrichmentHTTPJSON(t, in), f.tokens[0], 200, nil)
	f.request(t, f.handler, "DELETE", memoryHTTPList+"/"+deleteID, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "GET", memoryHTTPList+"/"+deleteID, "", f.tokens[0], 200, nil)
	t.Log("MEMORY_API ACTUAL_NEW_HANDLER_DETAIL_DATA=true ORIGINAL_PUT_LIST_DELETE=true PATH_REJECT_BARE_ORIGINAL_RECEIPT=true UNKNOWN_ORIGINAL_GET_ONLY=true")
}
func TestMemoryAPIHTTPNativeEncodedThenRealCurrentChangeZeroBody(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	f := privateProfileHTTPDBNew(t)
	t.Logf("MEMORY_API owned registered HTTP database %s", f.pool.Config().ConnConfig.Database)
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	for _, mode := range []string{"memory", "metadata", "revoke", "sealLeaseTamper"} {
		t.Run(mode, func(t *testing.T) {
			var id string
			if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id) != nil {
				t.Fatal("owned id")
			}
			in := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "encoded." + strings.ToLower(mode), Summary: "编码后绝不能释放的合成私密正文", StructuredValue: json.RawMessage(`{"closed":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
			m, e := f.store.PutOwnMemory(f.ctx, a, id, in)
			if e != nil {
				t.Fatal(e)
			}
			p, e := f.store.ReadOwnMemoryDetail(f.ctx, a, id)
			if e != nil {
				t.Fatal(e)
			}
			encoded, e := json.Marshal(p)
			if e != nil || !strings.Contains(string(encoded), in.Summary) {
				t.Fatal("expected encoded original")
			}
			before := ""
			store := &memoryAPIEncodedNativeStore{Store: f.store}
			if mode == "revoke" {
				defer f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			store.beforeRevalidate = func() error {
				switch mode {
				case "memory":
					if _, e = f.pool.Exec(f.ctx, `UPDATE agent_memories SET summary='新的本人合成内容',version=version+1 WHERE id=$1`, id); e != nil {
						t.Fatal(e)
					}
				case "metadata":
					if _, e = f.pool.Exec(f.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, m.AgentID); e != nil {
						t.Fatal(e)
					}
				case "revoke":
					if _, e = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
						t.Fatal(e)
					}
				case "sealLeaseTamper":
					// The native fingerprint is deliberately not replaceable with
					// a pure shape; tamper the actual issued argument in this hook.
					p.ExpiresAt = p.ObservedAt
				}
				before = memoryAPIHTTPFullPairs(t, f)
				if mode == "sealLeaseTamper" {
					return f.store.RevalidateOwnMemoryDetail(f.ctx, a, p)
				}
				return nil
			}
			h := privateProfileHTTPNew(store, f.store)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", memoryHTTPList+"/"+id, "", f.tokens[0], "application/json"))
			if !store.called || w.Code == 200 || strings.Contains(w.Body.String(), in.Summary) || before != memoryAPIHTTPFullPairs(t, f) {
				t.Fatal("registered handler released body or wrote rows after actual post-encode native change", w.Code)
			}
		})
	}
}
