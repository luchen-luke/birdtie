package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"strings"
	"testing"
	"time"
)

const candidateRetentionHTTPBase = "/v1/me/agent-candidate-retention"

type retentionHTTPFixture struct {
	f         *enrichmentHTTPFixture
	selection acr.Selection
}

func retentionHTTPNative(t *testing.T) *retentionHTTPFixture {
	t.Helper()
	f := enrichmentHTTPNative(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM agent_candidate_retention_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_candidate_retention_previews WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	// Native mutation captures its actual original outbox event. Do not fabricate Source JSON.
	m, e := f.store.UpdateMomentDraft(f.ctx, f.accountIDs[0], f.moment.ID, f.moment.Revision, content.MomentInput{CityID: f.city, Title: f.moment.Title, Body: "本人明确记录羽毛球关键词_HTTP_RETENTION", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	f.selection.MomentRevision = m.Revision
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	p, e := f.store.PreviewOwnEnrichmentPurpose(f.ctx, a, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.store.ApproveOwnEnrichmentPurpose(f.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	return &retentionHTTPFixture{f: f, selection: acr.Selection{AnalysisGrantID: g.ID, RetainUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}}
}
func TestCandidateRetentionHTTPNativeRegisteredLifecycle(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	w := f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, fixture.selection), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[acr.Preview](t, w.Body.Bytes())
	if acr.ValidatePreview(p) != nil || p.Review.Proposal.Category != "badminton" || p.Review.Clusters != 1 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "UNSELECTED_HTTP_TITLE") {
		t.Fatal(w.Body.String())
	}
	t.Log("REGISTERED_RETENTION_PREVIEW_WIRE", w.Body.String())
	f.request(t, f.handler, "GET", candidateRetentionHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	w = f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[acr.Grant](t, w.Body.Bytes())
	if acr.ValidateGrant(g) != nil || g.ExpiresAt.After(p.ExpiresAt) {
		t.Fatal(w.Body.String())
	}
	t.Log("REGISTERED_RETENTION_GRANT_WIRE", w.Body.String())
	w = f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	again := enrichmentHTTPData[acr.Grant](t, w.Body.Bytes())
	if again.ID != g.ID || !again.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("repeat changed grant")
	}
	w = f.request(t, f.handler, "GET", candidateRetentionHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	rec := enrichmentHTTPData[acr.Preview](t, w.Body.Bytes())
	if rec.State != "RECEIPT_ONLY" || rec.Review != nil || rec.ConsumedGrantID != g.ID {
		t.Fatal(rec)
	}
	f.request(t, f.handler, "GET", candidateRetentionHTTPBase+"/grants/"+g.ID, "", f.tokens[1], 403, nil)
	f.request(t, f.handler, "GET", candidateRetentionHTTPBase+"/grants/"+g.ID, "", f.tokens[0], 200, nil)
	w = f.request(t, f.handler, "DELETE", candidateRetentionHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	revoked := enrichmentHTTPData[acr.Grant](t, w.Body.Bytes())
	if revoked.Revision != 2 || revoked.RevokedAt == nil {
		t.Fatal(revoked)
	}
	f.request(t, f.handler, "DELETE", candidateRetentionHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 409, nil)
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memory_candidates WHERE owner_id=$1`, f.accountIDs[0]).Scan(&count); e != nil || count != 0 {
		t.Fatal("Phase A writes candidates", count, e)
	}
}
func TestCandidateRetentionHTTPNativeClosedTransportAndOriginalConsent(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	body := enrichmentHTTPJSON(t, fixture.selection)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", body, "", 401, nil)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews?confirmed=true", body, f.tokens[0], 400, nil)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", body, f.tokens[0], 403, &f.accountIDs[1])
	for _, bad := range []string{`{"confirmed":true}`, strings.TrimSuffix(body, "}") + `,"category":"culture"}`, strings.TrimSuffix(body, "}") + `,"purpose":"TASK_CONTEXT_READ"}`, strings.Replace(body, fixture.selection.AnalysisGrantID, "not-an-id", 1)} {
		f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", bad, f.tokens[0], 400, nil)
	}
	other := fixture.selection
	other.AnalysisGrantID = f.moment.ID
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, other), f.tokens[0], 403, nil)
	expired := fixture.selection
	expired.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, expired), f.tokens[0], 409, nil)
	f.request(t, f.handler, "POST", candidateRetentionHTTPBase+"/previews/"+f.moment.ID+"/approve", `{"confirmed":true}`, f.tokens[0], 400, nil)
	f.request(t, f.handler, "POST", "/v1/me/consents", enrichmentHTTPJSON(t, map[string]any{"recipientAccountId": f.accountIDs[0], "expiresAt": time.Now().UTC().Add(time.Hour), "purpose": acr.Purpose, "resourceId": f.moment.ID, "actions": []string{"stage_candidate"}}), f.tokens[0], 400, nil)
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='STAGE_MEMORY_CANDIDATE'`, f.accountIDs[0]).Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid request created retention grant", count, e)
	}
}

type retentionMalformedCatalog struct {
	*contextBuilderHTTPCatalog
	mutate func(*acr.Preview)
}

func (c *retentionMalformedCatalog) PreviewOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, s acr.Selection) (acr.Preview, error) {
	p, e := c.Store.PreviewOwnCandidateRetention(ctx, a, s)
	if e == nil {
		c.mutate(&p)
	}
	return p, e
}
func TestCandidateRetentionHTTPNativeMalformedAuthorityIsUnavailable(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	for _, mutate := range []func(*acr.Preview){func(p *acr.Preview) { p.ModelAccess = true }, func(p *acr.Preview) { p.CandidateWriteAvailable = true }, func(p *acr.Preview) { p.Owner.ID = f.accountIDs[1] }, func(p *acr.Preview) { p.Selection.RetainUntil = p.Selection.RetainUntil.Add(time.Second) }, func(p *acr.Preview) { p.Review.Source.Fingerprint = strings.Repeat("x", 64) }, func(p *acr.Preview) { p.Review.SelectedFields = []string{"unselected"} }} {
		handler := contextBuilderHTTPNew(&retentionMalformedCatalog{contextBuilderHTTPCatalog: f.catalog, mutate: mutate}, f.store, f.store)
		f.request(t, handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, fixture.selection), f.tokens[0], 503, nil)
	}
}
