package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"net/http"
	"strings"
	"testing"
	"time"
)

const enrichmentHTTPBase = "/v1/me/agent-enrichment-purpose"

type enrichmentHTTPFixture struct {
	*contextBuilderHTTPFixture
	selection aep.Selection
	moment    content.Moment
}

func enrichmentHTTPNative(t *testing.T) *enrichmentHTTPFixture {
	t.Helper()
	f := &enrichmentHTTPFixture{contextBuilderHTTPFixture: contextBuilderHTTPNative(t)}
	task, e := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.accountIDs[0], ActingUserID: f.accountIDs[0], CityID: f.city, Query: "本轮分析本人选定正文", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}})
	if e != nil {
		t.Fatal(e)
	}
	f.moment, e = f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: f.city, Title: "UNSELECTED_HTTP_TITLE", Body: "实际本人所选正文_HTTP_ENRICHMENT", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	f.selection = aep.Selection{TaskID: task.ID, MomentID: f.moment.ID, MomentRevision: f.moment.Revision, Fields: []string{"body"}, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond).Add(10 * time.Minute)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM agent_enrichment_purpose_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_enrichment_purpose_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	return f
}
func enrichmentHTTPJSON(t *testing.T, v any) string {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func enrichmentHTTPData[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var value T
	if e := json.Unmarshal(raw, &value); e != nil {
		t.Fatal(e)
	}
	return value
}
func TestEnrichmentPurposeHTTPNativeRegisteredLifecycle(t *testing.T) {
	f := enrichmentHTTPNative(t)
	w := f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, f.selection), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[aep.Preview](t, w.Body.Bytes())
	if aep.ValidatePreview(p) != nil || p.State != "CURRENT_REVIEW" || len(p.Review.Content) != 1 || p.Review.Content["body"] != f.moment.Body || strings.Contains(w.Body.String(), f.moment.Title) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	t.Log("REGISTERED_PREVIEW_WIRE", w.Body.String())
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	w = f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[aep.Grant](t, w.Body.Bytes())
	if aep.ValidateGrant(g) != nil || g.ExpiresAt.After(p.ExpiresAt) || g.ModelAccess || g.CandidateRetentionAllowed {
		t.Fatal(w.Body.String())
	}
	t.Log("REGISTERED_GRANT_WIRE", w.Body.String())
	retry := enrichmentHTTPData[aep.Grant](t, f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil).Body.Bytes())
	if retry.ID != g.ID || !retry.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("retry renewed grant")
	}
	receipt := enrichmentHTTPData[aep.Preview](t, f.request(t, f.handler, "GET", enrichmentHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil).Body.Bytes())
	if receipt.State != "RECEIPT_ONLY" || receipt.ConsumedGrantID != g.ID || len(receipt.Review.Content) != 0 {
		t.Fatal(receipt)
	}
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants/"+g.ID, "", f.tokens[1], 403, nil)
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants/"+g.ID, "", f.tokens[0], 200, nil)
	revoked := enrichmentHTTPData[aep.Grant](t, f.request(t, f.handler, "DELETE", enrichmentHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil).Body.Bytes())
	if revoked.Revision != 2 || revoked.RevokedAt == nil {
		t.Fatal(revoked)
	}
	f.request(t, f.handler, "DELETE", enrichmentHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 409, nil)
}
func TestEnrichmentPurposeHTTPNativeClosedTransport(t *testing.T) {
	f := enrichmentHTTPNative(t)
	body := enrichmentHTTPJSON(t, f.selection)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", body, "", 401, nil)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews?purpose=TASK_CONTEXT_READ", body, f.tokens[0], 400, nil)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", body, f.tokens[0], 403, &f.accountIDs[1])
	for _, bad := range []string{`{"confirmed":true}`, strings.Replace(body, `"fields":["body"]`, `"fields":["body","body"]`, 1), strings.TrimSuffix(body, "}") + `,"purpose":"TASK_CONTEXT_READ"}`} {
		f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", bad, f.tokens[0], 400, nil)
	}
	other := f.selection
	other.MomentID = f.public
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, other), f.tokens[0], 403, nil)
	expired := f.selection
	expired.DeadlineAt = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, expired), f.tokens[0], 409, nil)
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+f.moment.ID+"/approve", `{"confirmed":true}`, f.tokens[0], 400, nil)
	var grants, previews int
	if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM consent_grants WHERE owner_account_id=$1),(SELECT count(*) FROM agent_enrichment_purpose_previews WHERE owner_id=$1)`, f.accountIDs[0]).Scan(&grants, &previews); e != nil || grants != 0 || previews != 0 {
		t.Fatal("rejected transport created permission", grants, previews, e)
	}
}
func TestEnrichmentPurposeHTTPNativeOriginalConsentCannotBypass(t *testing.T) {
	f := enrichmentHTTPNative(t)
	f.request(t, f.handler, "POST", "/v1/me/consents", enrichmentHTTPJSON(t, map[string]any{"recipientAccountId": f.accountIDs[1], "expiresAt": time.Now().UTC().Add(time.Hour), "purpose": aep.Purpose, "resourceId": f.moment.ID, "actions": []string{"analyze_local"}}), f.tokens[0], 400, nil)
	w := f.request(t, f.handler, "POST", "/v1/me/consents", enrichmentHTTPJSON(t, map[string]any{"recipientAccountId": f.accountIDs[1], "expiresAt": time.Now().UTC().Add(time.Hour)}), f.tokens[0], 201, nil)
	var v struct {
		Data struct {
			ID      string `json:"id"`
			Purpose string `json:"purpose"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.Purpose != "profile_view" {
		t.Fatal(w.Body.String(), e)
	}
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	r, e := f.store.ResolveOwnEnrichmentPurpose(f.ctx, a, v.Data.ID)
	if e == nil || len(r.Content) != 0 {
		t.Fatal("profile_view became analysis", r, e)
	}
}

type enrichmentMalformedCatalog struct {
	*contextBuilderHTTPCatalog
	mutate func(*aep.Preview)
}

func (c *enrichmentMalformedCatalog) PreviewOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, s aep.Selection) (aep.Preview, error) {
	p, e := c.Store.PreviewOwnEnrichmentPurpose(ctx, a, s)
	if e == nil {
		c.mutate(&p)
	}
	return p, e
}
func TestEnrichmentPurposeHTTPNativeMalformedAuthorityIsUnavailable(t *testing.T) {
	f := enrichmentHTTPNative(t)
	for _, mutate := range []func(*aep.Preview){func(p *aep.Preview) { p.ModelAccess = true }, func(p *aep.Preview) { p.Owner.ID = f.accountIDs[1] }, func(p *aep.Preview) { p.State = "RECEIPT_ONLY"; p.ConsumedGrantID = p.ID; p.Review = aep.Review{} }, func(p *aep.Preview) { p.Selection.Fields = []string{"title", "body"} }} {
		handler := contextBuilderHTTPNew(&enrichmentMalformedCatalog{contextBuilderHTTPCatalog: f.catalog, mutate: mutate}, f.store, f.store)
		f.request(t, handler, http.MethodPost, enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, f.selection), f.tokens[0], 503, nil)
	}
}

func TestEnrichmentPreviewReceiptHTTPNativeRegisteredBodylessOwnerHistory(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	f := enrichmentHTTPNative(t)
	var database string
	if e := f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&database); e != nil {
		t.Fatal(e)
	}
	t.Log("ANALYSIS_RECEIPT owned registered HTTP database", database)
	w := f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, f.selection), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[aep.Preview](t, w.Body.Bytes())
	path := enrichmentHTTPBase + "/previews/" + p.ID + "/receipt"
	w = f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	r := enrichmentHTTPData[aep.PreviewReceipt](t, w.Body.Bytes())
	if aep.ValidatePreviewReceipt(r, aep.Purpose) != nil || r.State != "OPEN_UNCONSUMED" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	if _, e := f.pool.Exec(f.ctx, `UPDATE moments SET body='PRIVATE_CHANGED_HISTORY_HTTP_CANARY',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID); e != nil {
		t.Fatal(e)
	}
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	w = f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	r = enrichmentHTTPData[aep.PreviewReceipt](t, w.Body.Bytes())
	if r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID == "" {
		t.Fatal(w.Body.String())
	}
	for _, bad := range []string{f.moment.Body, "PRIVATE_CHANGED_HISTORY_HTTP_CANARY", `"selection"`, `"review"`, `"taskQuery"`, `"authority"`} {
		if strings.Contains(w.Body.String(), bad) {
			t.Fatal("bodyless history wire leaked")
		}
	}
	t.Log("HISTORICAL_ANALYSIS_PREVIEW_RECEIPT_RAW_WIRE", w.Body.String())
	f.request(t, f.handler, "GET", path, "", f.tokens[1], 403, nil)
	f.request(t, f.handler, "GET", path, "", "", 401, nil)
	f.request(t, f.handler, "GET", path+"?", "", f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", path+"?confirmed=true", "", f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", path, "{}", f.tokens[0], 400, nil)
}
