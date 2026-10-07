package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

type memoryAPIHTTPStore struct {
	*memoryHTTPStore
	p                           agentmemory.DetailProjection
	r                           mc.Receipt
	reads, revalidates, rejects int
	fail, late                  error
}

func (f *memoryAPIHTTPStore) ReadOwnMemoryDetail(_ context.Context, a agentprofile.PrivateAccess, _ string) (agentmemory.DetailProjection, error) {
	f.reads++
	f.access = a
	return f.p, f.fail
}
func (f *memoryAPIHTTPStore) RevalidateOwnMemoryDetail(_ context.Context, a agentprofile.PrivateAccess, p agentmemory.DetailProjection) error {
	f.revalidates++
	if _, e := agentmemory.DetailNativeProof(p, a); e != nil {
		return e
	}
	return f.late
}
func (f *memoryAPIHTTPStore) RejectOwnMemory(_ context.Context, a agentprofile.PrivateAccess, _ string, _ agentmemory.RejectInput) (mc.Receipt, error) {
	f.rejects++
	f.access = a
	return f.r, f.fail
}

type memoryAPIHTTPCatalog struct {
	foundation.PublicCatalog
	*memoryAPIHTTPStore
}

func memoryAPIHTTPFixture(t *testing.T) (http.Handler, *memoryAPIHTTPStore, string) {
	t.Helper()
	_, access, original, token, _ := memoryHTTPFixture(t)
	at := original.record.UpdatedAt
	a := agentprofile.PrivateAccess{SessionDigest: access.digest, WorkspacePrincipal: original.recordOwnerForAPI()}
	p, e := agentmemory.IssueDetail(agentmemory.DetailProjection{SchemaVersion: agentmemory.DetailSchema, Owner: a.WorkspacePrincipal, AgentID: original.record.AgentID, Target: agentmemory.DetailTarget{ID: memoryHTTPID, Version: 1, Status: agentmemory.StatusActive}, Memory: &original.record, ObservedAt: at, ExpiresAt: at.Add(time.Minute), Explanation: agentmemory.DetailExplanation}, a, strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	v, rv := memoryHTTPID, int64(2)
	r := mc.Receipt{SchemaVersion: mc.Schema, ID: completionHTTPID, Owner: a.WorkspacePrincipal, AgentID: original.record.AgentID, Target: mc.Target{Kind: "MEMORY", ID: memoryHTTPID, Version: 1}, Action: "REJECT", PlanDigest: strings.Repeat("b", 64), State: "COMMITTED", ObservedAt: at.Add(time.Second), ExpiresAt: at.Add(time.Minute), CommittedAt: timePointerForMemoryAPI(at.Add(time.Second)), ResultMemoryID: &v, ResultMemoryVersion: &rv, CurrentResultMatches: true}
	f := &memoryAPIHTTPStore{memoryHTTPStore: original, p: p, r: r}
	return privateProfileHTTPNew(&memoryAPIHTTPCatalog{memoryAPIHTTPStore: f}, access), f, token
}
func (s *memoryHTTPStore) recordOwnerForAPI() actorref.PrincipalRef {
	return actorref.PrincipalRef{Type: s.record.OwnerType, ID: s.record.OwnerID}
}
func timePointerForMemoryAPI(t time.Time) *time.Time { return &t }
func TestMemoryAPIHTTPActualRoutesAndClosedTransport(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		mutate                   func(*http.Request)
		want                     int
	}{
		{"detail", "GET", memoryHTTPList + "/" + memoryHTTPID, "", nil, 200},
		{"reject", "POST", memoryHTTPList + "/" + memoryHTTPID + "/reject", `{"operationId":"` + completionHTTPID + `","planDigest":"` + strings.Repeat("b", 64) + `"}`, nil, 200},
		{"getbody", "GET", memoryHTTPList + "/" + memoryHTTPID, "{}", nil, 400},
		{"query", "GET", memoryHTTPList + "/" + memoryHTTPID + "?ownerId=x", "", nil, 400},
		{"forcequery", "GET", memoryHTTPList + "/" + memoryHTTPID + "?", "", nil, 400},
		{"invalidpath", "GET", memoryHTTPList + "/bad", "", nil, 400},
		{"anonymous", "GET", memoryHTTPList + "/" + memoryHTTPID, "", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"org", "GET", memoryHTTPList + "/" + memoryHTTPID, "", func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }, 403},
		{"wrongmethod", "PATCH", memoryHTTPList + "/" + memoryHTTPID, "", nil, 405},
		{"ownerbody", "POST", memoryHTTPList + "/" + memoryHTTPID + "/reject", `{"ownerId":"x","confirmed":true}`, nil, 400},
		{"media", "POST", memoryHTTPList + "/" + memoryHTTPID + "/reject", `{}`, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f, token := memoryAPIHTTPFixture(t)
			r := privateProfileHTTPRequest(tc.method, tc.path, tc.body, token, "application/json")
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == 200 {
				if tc.name == "detail" {
					if f.reads != 1 || f.revalidates != 1 || !strings.Contains(w.Body.String(), `"data":`) || !strings.Contains(w.Body.String(), memoryHTTPCanary) {
						t.Fatal("detail not encoded/revalidated")
					}
				} else if f.rejects != 1 || strings.Contains(w.Body.String(), `"data":`) || strings.Contains(w.Body.String(), memoryHTTPCanary) {
					t.Fatal("native bare receipt changed")
				}
			}
			if tc.want != 200 && f.reads+f.revalidates+f.rejects != 0 {
				t.Fatal("invalid request reached native")
			}
		})
	}
}
func TestMemoryAPIHTTPZeroPrivateOutputOnCurrentFailure(t *testing.T) {
	for _, mode := range []string{"owner", "target", "model", "unsealed", "lateexpired", "lateforbidden", "rawerror", "recordvalue"} {
		t.Run(mode, func(t *testing.T) {
			h, f, token := memoryAPIHTTPFixture(t)
			want := 503
			switch mode {
			case "owner":
				f.p.Owner.ID = privateProfileHTTPForeign
			case "target":
				f.p.Target.ID = privateProfileHTTPForeign
			case "model":
				f.p.ModelAccess = true
			case "unsealed":
				f.p = agentmemory.DetailProjection{}
			case "lateexpired":
				f.late = agentmemory.ErrConflict
				want = 409
			case "lateforbidden":
				f.late = agentmemory.ErrForbidden
				want = 403
			case "rawerror":
				f.fail = errors.New(memoryHTTPCanary)
			case "recordvalue":
				f.p.Memory.StructuredValue = json.RawMessage(`{"changed":true}`)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", memoryHTTPList+"/"+memoryHTTPID, "", token, ""))
			if w.Code != want || strings.Contains(w.Body.String(), memoryHTTPCanary) {
				t.Fatal("private projection released on current failure", w.Code)
			}
		})
	}
}
func TestMemoryAPIHTTPRejectOriginalReceiptResponseBinding(t *testing.T) {
	for _, mode := range []string{"owner", "id", "kind", "target", "action", "digest", "pending", "model"} {
		t.Run(mode, func(t *testing.T) {
			h, f, token := memoryAPIHTTPFixture(t)
			switch mode {
			case "owner":
				f.r.Owner.ID = privateProfileHTTPForeign
			case "id":
				f.r.ID = privateProfileHTTPForeign
			case "kind":
				f.r.Target.Kind = "CANDIDATE"
			case "target":
				f.r.Target.ID = privateProfileHTTPForeign
			case "action":
				f.r.Action = "DELETE"
			case "digest":
				f.r.PlanDigest = strings.Repeat("c", 64)
			case "pending":
				f.r.State = "PENDING"
			case "model":
				f.r.ModelAccess = true
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("POST", memoryHTTPList+"/"+memoryHTTPID+"/reject", `{"operationId":"`+completionHTTPID+`","planDigest":"`+strings.Repeat("b", 64)+`"}`, token, "application/json"))
			if w.Code != 503 || strings.Contains(w.Body.String(), memoryHTTPCanary) {
				t.Fatal("wrong receipt released")
			}
		})
	}
}
func TestMemoryAPIHTTPOptionalPortMissingFailClosed(t *testing.T) {
	s, access, f, token, _ := memoryHTTPFixture(t)
	_ = s
	h := privateProfileHTTPNew(&memoryHTTPCatalog{memoryHTTPStore: f}, access)
	for _, path := range []string{memoryHTTPList + "/" + memoryHTTPID, memoryHTTPList + "/" + memoryHTTPID + "/reject"} {
		method := "GET"
		body := ""
		if strings.HasSuffix(path, "reject") {
			method = "POST"
			body = `{}`
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest(method, path, body, token, "application/json"))
		if w.Code != 503 || f.calls != 0 {
			t.Fatal("old Store claimed current port")
		}
	}
}
