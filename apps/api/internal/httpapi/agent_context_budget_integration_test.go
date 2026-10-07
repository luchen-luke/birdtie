package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
)

func TestContextBudgetHTTPRegisteredDefaultAndNoClientAuthority(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	var schema88 bool
	if e := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.model_request_runs') IS NOT NULL`).Scan(&schema88); e != nil || !schema88 {
		t.Fatal("registered HTTP requires actual schema088", e)
	}
	g := f.approve(t, adapterHTTPPreview(t, f))
	before := f.readonlyRows(t)
	w := f.runtime(t, g, 200)
	var reply struct {
		Data aca.View `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &reply); e != nil {
		t.Fatal(e)
	}
	v := reply.Data
	raw, _ := json.Marshal(v)
	if v.Budget.Limit != 16384 || v.Budget.Used != len(raw) || v.Budget.TokenCountStatus != "UNKNOWN_NOT_TOKENIZED" || v.ModelAccess != "UNAVAILABLE" || v.MemoryPromotionAllowed || v.ContentIsInstruction {
		t.Fatal("default byte accounting/authority", v)
	}
	known := false
	for _, fact := range v.Facts {
		if fact.Kind == "EXPLICIT_MEMORY" {
			known = fact.Confidence != nil && fact.Confidence.Semantics == agentconfidence.DirectDeclaration && fact.Confidence.Value != nil && *fact.Confidence.Value == f.memory.Confidence
		}
	}
	if !known {
		t.Fatal("registered default route lost actual native confidence")
	}
	for _, field := range []string{`"itemLimit":0`, `"priority":["memories"]`, `"confidenceThreshold":0.5`, `"recencyWeight":1`, `"ownerId":"` + f.accountIDs[1] + `"`, `"query":"未批准"`} {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`",`+field+`}`, f.tokens[0], 400, nil)
	}
	f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", `{"grantId":"`+g.ID+`"}`, f.tokens[1], 403, nil)
	if before != f.readonlyRows(t) {
		t.Fatal("default projection or rejected controls wrote native data")
	}
	service, built, access := adapterNativeBuild(t, f, g)
	zero, threshold := 0, 1.0
	projected, e := consumeBudgetedTaskContext(built.Bundle, aca.Budget{MaxEncodedBytes: 32768, ItemLimit: &zero, ConfidenceThreshold: &threshold, RecencyWeight: 1, Priority: []string{"memories", "activities", "places", "relationships", "profile"}})
	if e != nil || len(projected.Facts) != 1 || len(projected.Sources) != 3 {
		t.Fatal("server controls lost anchors", e, projected)
	}
	if _, e = service.RevalidateOwn(f.ctx, access, built); e != nil {
		t.Fatal(e)
	}
	f.request(t, f.handler, "DELETE", contextPurposeHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	if _, e = service.RevalidateOwn(f.ctx, access, built); e == nil {
		t.Fatal("omitted native grant revoke escaped seal")
	}
	w = f.runtime(t, g, 403)
	if strings.Contains(w.Body.String(), contextPurposeHTTPMemory) {
		t.Fatal("revoked payload leak")
	}
	t.Log("LOCAL_SYNTHETIC registered native schema088 Runtime preserves server-only controls and actual confidence; revoke yields no context")
}
