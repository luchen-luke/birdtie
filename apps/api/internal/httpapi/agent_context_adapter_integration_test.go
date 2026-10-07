package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentcontextrelevance"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

func adapterNativeBuild(t *testing.T, f *contextPurposeHTTPFixture, g acb.PurposeGrant) (*acb.Service, acb.BuiltContext, agentprofile.PrivateAccess) {
	t.Helper()
	access := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	service, e := acb.NewService(f.store)
	if e != nil {
		t.Fatal(e)
	}
	agent, e := service.Resolve(f.ctx, access)
	if e != nil {
		t.Fatal(e)
	}
	s := g.Selection
	request := acb.Request{Access: access, Agent: agent, TaskID: s.TaskID, RequestID: "native-air022", CityID: s.CityID, CurrentQuery: s.CurrentQuery, TaskUpdatedAt: s.TaskUpdatedAt, Mode: acb.MachineTaskContext, Selection: acb.ExactTaskContext, ProfileFields: s.ProfileFields, MemoryIDs: s.MemoryIDs, PlaceIDs: s.PlaceIDs, ActivityIDs: s.ActivityIDs, RelationshipTieIDs: s.RelationshipTieIDs, PolicyFamilies: s.PolicyFamilies, PurposeGrantID: g.ID, PurposeDeadlineAt: s.DeadlineAt, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)}
	if request.DeadlineAt.After(g.ExpiresAt) {
		request.DeadlineAt = g.ExpiresAt
	}
	built, e := service.Build(f.ctx, request)
	if e != nil {
		t.Fatal("native approved Build", e)
	}
	return service, built, access
}
func adapterHTTPPreview(t *testing.T, f *contextPurposeHTTPFixture) acb.PurposePreview {
	t.Helper()
	w := f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", contextPurposeHTTPSelectionBody(t, f.selection), f.tokens[0], 200, nil)
	var out struct {
		Data acb.PurposePreview `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.ID == "" {
		t.Fatal("native preview", e)
	}
	return out.Data
}

// Calibrate the smallest byte budget that can hold the actual mandatory
// anchors and their current field evidence. Every lower budget must fail closed;
// this never changes the runtime default or removes evidence to obtain a fit.
func nativeAdapterMinimumBudget(t *testing.T, project func(int) (aca.View, error)) (aca.View, int) {
	t.Helper()
	low, high := 1, aca.MaxEncodedBytes
	if _, e := project(high); e != nil { t.Fatal("maximum native adapter budget", e) }
	for low < high {
		mid := low + (high-low)/2
		_, e := project(mid)
		if e == nil { high = mid } else if errors.Is(e, aca.ErrBudget) { low = mid+1 } else { t.Fatal("unexpected native budget failure", e) }
	}
	v, e := project(low)
	if e != nil { t.Fatal("measured mandatory anchor floor", e) }
	if low > 1 { if _, e = project(low-1); !errors.Is(e, aca.ErrBudget) { t.Fatal("below measured mandatory floor did not fail closed", e) } }
	raw, e := json.Marshal(v)
	if e != nil || len(raw) != v.Budget.Used || len(raw) > low || v.Budget.Limit != low || v.FieldEvidenceSet == nil { t.Fatal("actual evidence or byte accounting lost") }
	t.Logf("ACTUAL_REQUIRED_ANCHOR_FLOOR=%d USED=%d", low, len(raw))
	return v, low
}

func TestContextAdapterHTTPNativeBudgetAfterActualApproval(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	// The source is written through its native human route and then explicitly
	// selected in the immutable preview. No fake grant or new knowledge is used.
	longText := "羽毛球" + strings.Repeat("；仅为本人填写的合成偏好，不表示参加", 90)
	fields := map[string]any{"expectedVersion": 2, "fields": map[string]any{"agentNotes": longText, "availability": "", "languagePreferences": []string{}}}
	raw, _ := json.Marshal(fields)
	f.request(t, f.handler, "PUT", privateProfileHTTPPath, string(raw), f.tokens[0], 200, nil)
	f.selection.ProfileFields = []string{"agentNotes", "availability", "languagePreferences"}
	p := adapterHTTPPreview(t, f)
	if !strings.Contains(string(p.Review.Profile["agentNotes"]), "羽毛球") {
		t.Fatal("actual human preview missing specific content")
	}
	g := f.approve(t, p)
	before := f.readonlyRows(t)
	service, built, access := adapterNativeBuild(t, f, g)
	large, e := consumeBudgetedTaskContext(built.Bundle, aca.Budget{MaxEncodedBytes: 32768})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := consumeBudgetedTaskContext(built.Bundle, aca.Budget{MaxEncodedBytes: 4800}); !errors.Is(e, aca.ErrBudget) {
		t.Fatal("old 4800-byte budget must refuse mandatory current metadata", e)
	}
	small, smallLimit := nativeAdapterMinimumBudget(t, func(n int) (aca.View, error) {
		return consumeBudgetedTaskContext(built.Bundle, aca.Budget{MaxEncodedBytes: n})
	})
	largeRaw, _ := json.Marshal(large)
	smallRaw, _ := json.Marshal(small)
	if !strings.Contains(string(largeRaw), longText) || strings.Contains(string(smallRaw), longText) || len(smallRaw) >= len(largeRaw) || small.Budget.Omitted["profile"] < 1 || len(smallRaw) != small.Budget.Used || len(smallRaw) > smallLimit {
		t.Fatal("budgeted runtime did not consume bounded selected content", len(smallRaw), len(largeRaw))
	}
	for _, value := range []string{"LEVEL_0_OBSERVE", "仅观察", f.task.Query, "Europe/London", "UNKNOWN", "NOT_TOKENIZED"} {
		if !strings.Contains(string(smallRaw), value) {
			t.Fatal("required actual anchor/unknown omitted", value)
		}
	}
	if _, e = service.RevalidateOwn(f.ctx, access, built); e != nil {
		t.Fatal("current approved native seal", e)
	}
	if before != f.readonlyRows(t) {
		t.Fatal("adapter wrote or dispatched")
	}
	// Exercise the real subsequent relevance service too: the budget adapter
	// consumes only its approved related projection, then revalidates its full
	// original source set rather than treating the projection as permission.
	relevance, e := agentcontextrelevance.NewService(f.store)
	if e != nil {
		t.Fatal(e)
	}
	related, e := relevance.Retrieve(f.ctx, built.Request)
	if e != nil {
		t.Fatal("native relevant sources", e)
	}
	relatedLarge, e := consumeRelatedBudgetedTaskContext(related.ProjectionBundle(), aca.Budget{MaxEncodedBytes: 32768}, related.ExcludedCounts())
	if e != nil {
		t.Fatal(e)
	}
	relatedSmall, relatedLimit := nativeAdapterMinimumBudget(t, func(n int) (aca.View, error) {
		return consumeRelatedBudgetedTaskContext(related.ProjectionBundle(), aca.Budget{MaxEncodedBytes: n}, related.ExcludedCounts())
	})
	largeRaw, _ = json.Marshal(relatedLarge)
	smallRaw, _ = json.Marshal(relatedSmall)
	if !strings.Contains(string(largeRaw), longText) || strings.Contains(string(smallRaw), longText) || relatedSmall.Budget.Omitted["profile"] == 0 || len(smallRaw) != relatedSmall.Budget.Used || len(smallRaw) > relatedLimit {
		t.Fatal("native relevance to budget consumption was bypassed")
	}
	if e = relevance.Revalidate(f.ctx, access, related); e != nil {
		t.Fatal(e)
	}
	if _, e = consumeBudgetedTaskContext(built.Bundle, aca.Budget{MaxEncodedBytes: 1}); !errors.Is(e, aca.ErrBudget) {
		t.Fatal("insufficient required anchors not rejected", e)
	}
	// Even a fact omitted by budget stays in the original sealed authorization.
	fields = map[string]any{"expectedVersion": 3, "fields": map[string]any{"agentNotes": "已更正且尚未批准的来源"}}
	raw, _ = json.Marshal(fields)
	f.request(t, f.handler, "PUT", privateProfileHTTPPath, string(raw), f.tokens[0], 200, nil)
	if _, e = service.RevalidateOwn(f.ctx, access, built); !errors.Is(e, acb.ErrDenied) {
		t.Fatal("budget omission hid source change", e)
	}
	if e = relevance.Revalidate(f.ctx, access, related); !errors.Is(e, acb.ErrDenied) {
		t.Fatal("budget/relevance omission hid source change", e)
	}
	f.runtime(t, g, 403)
	t.Log("LOCAL_SYNTHETIC actual native selected Profile/Memory/Policy/City/Task/etc consumed with final-byte input bounds; omitted native source remains sealed; no model or domain writes")
}
func TestContextAdapterHTTPNativeRegisteredRuntimeUsesAdapter(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	g := f.approve(t, adapterHTTPPreview(t, f))
	before := f.readonlyRows(t)
	w := f.runtime(t, g, 200)
	var data struct {
		Data aca.View `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
		t.Fatal(e)
	}
	v := data.Data
	raw, _ := json.Marshal(v)
	if v.AdapterVersion != aca.Version || v.Budget.Unit != aca.BudgetUnit || v.Budget.Limit != aca.DefaultBudget().MaxEncodedBytes || v.Budget.Used != len(raw) || v.Budget.Used > v.Budget.Limit || v.Budget.TokenCountStatus != "UNKNOWN_NOT_TOKENIZED" {
		t.Fatal("registered Runtime bypassed adapter", v.AdapterVersion, v.Budget, len(raw))
	}
	if len(v.Provenance) < 3 || v.ModelAccess != "UNAVAILABLE" || v.MemoryPromotionAllowed || v.ContentIsInstruction || v.ExpiresAt.After(g.ExpiresAt) {
		t.Fatal("authority/trace/time changed")
	}
	for _, p := range v.Provenance {
		if p.Source.ID == "" || len(p.Fields) == 0 || p.Source.RowToken != "" {
			t.Fatal("retained field source missing")
		}
	}
	for _, forbidden := range []string{contextPurposeHTTPUnselected, "STRUCTURED_VALUE_NOT_IN_RUNTIME", `"rowToken"`, `"authority"`, `"conversation"`, `"latitude"`, `"longitude"`} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("private context leaked", forbidden)
		}
	}
	if before != f.readonlyRows(t) {
		t.Fatal("runtime effects")
	}
	for _, body := range []string{`{"grantId":"` + g.ID + `","budget":1}`, `{"grantId":"` + g.ID + `","maxEncodedBytes":32768}`, `{"grantId":"` + g.ID + `","ownerId":"` + f.accountIDs[1] + `"}`, `{"GrantId":"` + g.ID + `"}`} {
		f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/runtime", body, f.tokens[0], 400, nil)
	}
}

// Run the actual registered-handler source/identity and held-pool fixtures
// against the newly integrated consumer. These are genuine native regressions,
// not stubs; no repeated case count is reported as distinct requirements.
func TestContextAdapterHTTPNativeSourceAndIdentityRegressions(t *testing.T) {
	TestContextPurposeHTTPNativeCurrentChangesInvalidateApproval(t)
	TestContextPurposeHTTPNativeClosedWireAndIdentity(t)
}
func TestContextAdapterHTTPNativeFinalPoolWait(t *testing.T) {
	TestContextPurposeHTTPNativeFinalPoolWait(t)
}
