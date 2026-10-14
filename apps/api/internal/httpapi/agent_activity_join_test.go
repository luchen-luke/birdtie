package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type joinHTTPSpy struct {
	preview       aa.ActivityJoinPreview
	receipt       aa.ActivityJoinReceipt
	calls, checks int
	late          error
	which         string
}

func (g *joinHTTPSpy) PreviewOwn(context.Context, agentprofile.PrivateAccess, aa.ActivityJoinInput) (aa.ActivityJoinPreview, error) {
	g.calls++
	g.which = "preview"
	return g.preview, nil
}
func (g *joinHTTPSpy) ConfirmOwn(context.Context, agentprofile.PrivateAccess, string, string) (aa.ActivityJoinPreview, error) {
	g.calls++
	g.which = "confirm"
	return g.preview, nil
}
func (g *joinHTTPSpy) ExecuteOwn(context.Context, agentprofile.PrivateAccess, string, string) (aa.ActivityJoinReceipt, error) {
	g.calls++
	g.which = "execute"
	return g.receipt, nil
}
func (g *joinHTTPSpy) ReconcileOwn(context.Context, agentprofile.PrivateAccess, string) (aa.ActivityJoinReceipt, error) {
	g.calls++
	g.which = "reconcile"
	return g.receipt, nil
}
func (g *joinHTTPSpy) RevokeOwn(context.Context, agentprofile.PrivateAccess, string) (aa.ActivityJoinPreview, error) {
	g.calls++
	g.which = "revoke"
	return g.preview, nil
}
func (g *joinHTTPSpy) ValidatePreviewOwn(_ context.Context, _ agentprofile.PrivateAccess, v aa.ActivityJoinPreview) error {
	g.checks++
	if v.Proof != "native-proof" {
		return aa.ErrDenied
	}
	return g.late
}
func (g *joinHTTPSpy) ValidateReceiptOwn(_ context.Context, _ agentprofile.PrivateAccess, v aa.ActivityJoinReceipt) error {
	g.checks++
	if v.Proof != "native-proof" {
		return aa.ErrDenied
	}
	return g.late
}
func TestAgentActivityJoinHTTPRegisteredClosedBodiesAndLateFence(t *testing.T) {
	_, access, token := recoveryHTTPFixture(t)
	id := recoveryHTTPApproval
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	g := &joinHTTPSpy{preview: aa.ActivityJoinPreview{SchemaVersion: aa.ActivityJoinSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: recoveryHTTPOwner}, AgentID: id, TaskID: id, ApprovalID: id, LogicalOperationID: id, ActionID: id, ActivityID: id, BindingDigest: strings.Repeat("a", 64), State: aa.Pending, ObservedAt: at, ExpiresAt: at.Add(30 * time.Second), Proof: "native-proof"}, receipt: aa.ActivityJoinReceipt{SchemaVersion: aa.ActivityJoinSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: recoveryHTTPOwner}, AgentID: id, ApprovalID: id, DispatchID: id, Status: aa.Unknown, CommittedAt: at, Proof: "native-proof"}}
	physical := "not_applicable"
	g.preview.Review = &aa.ActivityJoinReview{SchemaVersion: aa.ActivityJoinReviewSchema, ActivityID: id, TargetVersion: strings.Repeat("b", 64), Title: "原生公开活动", Summary: "公开摘要", Description: "须核实的完整报名条件", Organizer: aa.ActivityJoinOrganizer{Type: "PERSON", ID: id}, StartsAt: at.Add(time.Hour), EndsAt: at.Add(2 * time.Hour), TimeZone: "Europe/London", Modality: "online", PhysicalPlaceStatus: &physical, Visibility: "public"}
	h := New(nil, access, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, WithAgentActivityJoin(g))
	base := "/v1/me/agent-activity-join/approvals/" + id + "/"
	for _, tc := range []struct {
		path, body, which string
		status            int
	}{
		{"/v1/me/agent-activity-join/previews", `{"taskId":"` + id + `","logicalOperationId":"` + id + `","actionId":"` + id + `","activityId":"` + id + `","expectedTaskUpdatedAt":"2026-10-13T00:00:00Z","replyTurnDigest":"` + strings.Repeat("a", 64) + `"}`, "preview", 201},
		{base + "confirm", `{"bindingDigest":"` + strings.Repeat("a", 64) + `"}`, "confirm", 200}, {base + "execute", `{"bindingDigest":"` + strings.Repeat("a", 64) + `"}`, "execute", 200}, {base + "reconcile", "", "reconcile", 200}, {base + "revoke", "", "revoke", 200},
	} {
		if tc.which == "revoke" {
			g.preview.State = aa.Cancelled
			g.preview.Review = nil
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(w, r)
		if w.Code != tc.status || g.which != tc.which || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc.which, w.Code, w.Body.String())
		}
		for _, secret := range []string{"native-proof", "effectKey", "session", "grant", "source_generation"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("internal value leaked")
			}
		}
	}
	before := g.calls
	for _, tc := range []struct {
		path, body string
		org        bool
		status     int
	}{{base + "reconcile", " ", false, 400}, {base + "reconcile?owner=x", "", false, 400}, {base + "confirm", `{"bindingDigest":"` + strings.Repeat("a", 64) + `","confirmed":true}`, false, 400}, {base + "reconcile", "", true, 403}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+token)
		if tc.org {
			r.Header.Set("X-Birdtie-Organization-Workspace", id)
		}
		h.ServeHTTP(w, r)
		if w.Code != tc.status || g.calls != before {
			t.Fatal(w.Code, g.calls, before)
		}
	}
	g.late = aa.ErrDenied
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", base+"reconcile", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(w, r)
	if w.Code != 403 || strings.Contains(w.Body.String(), "dispatchId") {
		t.Fatal("late revoked response escaped", w.Code)
	}
}
func TestAgentActivityJoinHTTPMissingGatewayFailsClosed(t *testing.T) {
	var g *joinHTTPSpy
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, WithAgentActivityJoin(g))
	for _, path := range []string{"/v1/me/agent-activity-join/previews", "/v1/me/agent-activity-join/approvals/" + recoveryHTTPApproval + "/reconcile"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
}
