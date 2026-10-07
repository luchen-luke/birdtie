package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"strings"
	"testing"
	"time"
)

func TestMemoryCorrectionHTTPNativeRegisteredLifecycleAndClosedRecovery(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	f := privateProfileHTTPDBNew(t)
	var db string
	if f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&db) != nil || !strings.HasPrefix(db, "birdtie_saf001_http_") {
		t.Fatal("owned HTTP DB required")
	}
	t.Logf("MEMORY_CORRECTION owned registered HTTP database %s", db)
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	var id, operation string
	if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&id, &operation) != nil {
		t.Fatal("owned IDs")
	}
	m, e := f.store.PutOwnMemory(f.ctx, a, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "registered.correction", Summary: "合成本人原声明", StructuredValue: json.RawMessage(`{"native":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	in := mc.Input{ID: operation, TargetKind: "MEMORY", TargetID: id, ExpectedVersion: m.Version, Action: "REJECT"}
	w := f.request(t, f.handler, "POST", memoryCorrectionPath+"/previews", enrichmentHTTPJSON(t, in), f.tokens[0], 200, nil)
	var p mc.Preview
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || mc.ValidatePreview(p) != nil || len(p.Memories) != 1 || p.Memories[0].Summary != m.Summary {
		t.Fatal("actual registered bare preview")
	}
	f.request(t, f.handler, "POST", memoryCorrectionPath+"/"+p.ID+"/confirm", `{"confirmed":true}`, f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", memoryCorrectionPath+"/"+p.ID, "", f.tokens[1], 404, nil)
	f.request(t, f.handler, "POST", memoryCorrectionPath+"/"+p.ID+"/confirm", enrichmentHTTPJSON(t, mc.ConfirmInput{PlanDigest: p.PlanDigest}), f.tokens[1], 404, nil)
	w = f.request(t, f.handler, "POST", memoryCorrectionPath+"/"+p.ID+"/confirm", enrichmentHTTPJSON(t, mc.ConfirmInput{PlanDigest: p.PlanDigest}), f.tokens[0], 200, nil)
	var receipt mc.Receipt
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.State != "COMMITTED" || !receipt.CurrentResultMatches || receipt.Action != "REJECT" || strings.Contains(w.Body.String(), m.Summary) {
		t.Fatal("actual metadata receipt not bound")
	}
	next := f.newSession(f.accountIDs[0], false, false)
	f.request(t, f.handler, "POST", memoryCorrectionPath+"/"+p.ID+"/confirm", enrichmentHTTPJSON(t, mc.ConfirmInput{PlanDigest: p.PlanDigest}), next, 403, nil)
	w = f.request(t, f.handler, "GET", memoryCorrectionPath+"/"+p.ID, "", next, 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.State != "COMMITTED" || receipt.ResultMemoryVersion == nil || *receipt.ResultMemoryVersion != 2 {
		t.Fatal("new session only original metadata")
	}
	t.Log("REGISTERED_NEW_HANDLER_NATIVE=true EXACT_MEMORY_REJECT_DELETED=true NEW_SESSION_POST_FORBIDDEN=true BODYLESS_RECEIPT=true")
}
