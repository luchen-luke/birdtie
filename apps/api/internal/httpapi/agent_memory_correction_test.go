package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type correctionHTTPStore struct {
	*privateProfileHTTPStore
	p        mc.Preview
	r        mc.Receipt
	calls    int
	failure  error
	captured agentprofile.PrivateAccess
}
type correctionHTTPCatalog struct {
	foundation.PublicCatalog
	*correctionHTTPStore
}

func (f *correctionHTTPStore) PreviewOwnMemoryCorrection(_ context.Context, a agentprofile.PrivateAccess, _ mc.Input) (mc.Preview, error) {
	f.calls++
	f.captured = a
	return f.p, f.failure
}
func (f *correctionHTTPStore) ConfirmOwnMemoryCorrection(_ context.Context, a agentprofile.PrivateAccess, _ string, _ mc.ConfirmInput) (mc.Receipt, error) {
	f.calls++
	f.captured = a
	return f.r, f.failure
}
func (f *correctionHTTPStore) ReadOwnMemoryCorrection(_ context.Context, a agentprofile.PrivateAccess, _ string) (mc.Receipt, error) {
	f.calls++
	f.captured = a
	return f.r, f.failure
}
func correctionHTTPFixture(t *testing.T) (*server, *correctionHTTPStore, string) {
	s, _, original, token := privateProfileHTTPFixture(t)
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	in := mc.Input{ID: completionHTTPID, TargetKind: "MEMORY", TargetID: privateProfileHTTPForeign, ExpectedVersion: 1, Action: "REJECT"}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}
	memory, e := agentmemory.NewExplicit(privateProfileHTTPForeign, privateProfileHTTPAgent, owner, 1, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "correction.http", Summary: "合成本人原声明", StructuredValue: json.RawMessage(`{"native":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}, now, now)
	if e != nil {
		t.Fatal(e)
	}
	store := &correctionHTTPStore{privateProfileHTTPStore: original, p: mc.Preview{SchemaVersion: mc.Schema, ID: in.ID, Owner: owner, AgentID: privateProfileHTTPAgent, Input: in, Memories: []agentmemory.Record{memory}, Affected: []mc.Target{{Kind: "MEMORY", ID: in.TargetID, Version: 1}}, PlanDigest: strings.Repeat("a", 64), ObservedAt: now, ExpiresAt: now.Add(time.Minute), Explanation: mc.Explanation}, r: mc.Receipt{SchemaVersion: mc.Schema, ID: in.ID, Owner: owner, AgentID: privateProfileHTTPAgent, Target: mc.Target{Kind: "MEMORY", ID: in.TargetID, Version: 1}, Action: "REJECT", PlanDigest: strings.Repeat("a", 64), State: "PENDING", ObservedAt: now, ExpiresAt: now.Add(time.Minute)}}
	s.privateProfiles = store
	return s, store, token
}
func correctionHTTPInput() string {
	return `{"id":"` + completionHTTPID + `","targetKind":"MEMORY","targetId":"` + privateProfileHTTPForeign + `","expectedVersion":1,"action":"REJECT"}`
}
func TestMemoryCorrectionHTTPActualNewRoutesAndClosedTransport(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		mutate                   func(*http.Request)
		want                     int
	}{
		{"preview", "POST", memoryCorrectionPath + "/previews", correctionHTTPInput(), nil, 200}, {"receipt", "GET", memoryCorrectionPath + "/" + completionHTTPID, "", nil, 200}, {"confirm", "POST", memoryCorrectionPath + "/" + completionHTTPID + "/confirm", `{"planDigest":"` + strings.Repeat("a", 64) + `"}`, nil, 200},
		{"get_body", "GET", memoryCorrectionPath + "/" + completionHTTPID, "{}", nil, 400}, {"query_owner", "GET", memoryCorrectionPath + "/" + completionHTTPID + "?ownerId=" + privateProfileHTTPForeign, "", nil, 400}, {"unknown_authority", "POST", memoryCorrectionPath + "/previews", strings.Replace(correctionHTTPInput(), `{`, `{"confirmed":true,`, 1), nil, 400}, {"duplicate", "POST", memoryCorrectionPath + "/previews", strings.Replace(correctionHTTPInput(), `"action":"REJECT"`, `"action":"REJECT","action":"DELETE"`, 1), nil, 400}, {"bad_media", "POST", memoryCorrectionPath + "/previews", correctionHTTPInput(), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415}, {"oversize", "POST", memoryCorrectionPath + "/previews", strings.Repeat("x", mc.MaxBodyBytes+1), nil, 413}, {"wrong_method", "DELETE", memoryCorrectionPath + "/" + completionHTTPID, "", nil, 405}, {"anonymous", "GET", memoryCorrectionPath + "/" + completionHTTPID, "", func(r *http.Request) { r.Header.Del("Authorization") }, 401}, {"org", "GET", memoryCorrectionPath + "/" + completionHTTPID, "", func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, token := correctionHTTPFixture(t)
			h := privateProfileHTTPNew(&correctionHTTPCatalog{correctionHTTPStore: store}, s.access)
			req := privateProfileHTTPRequest(tc.method, tc.path, tc.body, token, "application/json")
			if tc.mutate != nil {
				tc.mutate(req)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == 200 {
				if store.calls != 1 || store.captured.WorkspacePrincipal.ID != privateProfileHTTPOwner || strings.Contains(w.Body.String(), `"data":`) {
					t.Fatal("current identity or actual bare response changed")
				}
			} else if store.calls != 0 {
				t.Fatal("invalid transport reached native port")
			}
		})
	}
}
func TestMemoryCorrectionHTTPInvalidNativeResponses(t *testing.T) {
	for _, mode := range []string{"owner", "target", "model", "digest", "raw_error", "missing_capability"} {
		t.Run(mode, func(t *testing.T) {
			s, store, token := correctionHTTPFixture(t)
			path := memoryCorrectionPath + "/previews"
			body := correctionHTTPInput()
			switch mode {
			case "owner":
				store.p.Owner.ID = privateProfileHTTPForeign
			case "target":
				store.p.Input.ExpectedVersion = 2
			case "model":
				store.p.ModelAccess = true
			case "digest":
				path = memoryCorrectionPath + "/" + completionHTTPID + "/confirm"
				body = `{"planDigest":"` + strings.Repeat("b", 64) + `"}`
			case "raw_error":
				store.failure = errors.New(privateProfileHTTPMarker)
			case "missing_capability":
				s.privateProfiles = store.privateProfileHTTPStore
			}
			w := httptest.NewRecorder()
			s.previewOwnMemoryCorrection(w, privateProfileHTTPRequest("POST", path, body, token, "application/json"))
			if mode == "digest" {
				w = httptest.NewRecorder()
				req := privateProfileHTTPRequest("POST", path, body, token, "application/json")
				req.SetPathValue("operationID", completionHTTPID)
				s.confirmOwnMemoryCorrection(w, req)
			}
			if w.Code != 503 || strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
				t.Fatal("invalid native returned data or raw private error", w.Code)
			}
		})
	}
}
