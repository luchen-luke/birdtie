package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const completionHTTPID = "80000000-0000-4000-8000-000000000004"

type completionHTTPStore struct {
	*privateProfileHTTPStore
	suggestions apc.Suggestions
	preview     apc.Preview
	receipt     apc.Receipt
	calls       int
	failure     error
	captured    agentprofile.PrivateAccess
}

type completionHTTPCatalog struct {
	foundation.PublicCatalog
	*completionHTTPStore
}

func TestProfileMemoryCompletionHTTPRegisteredNewRoutes(t *testing.T) {
	s, store, token := completionHTTPFixture(t)
	h := privateProfileHTTPNew(&completionHTTPCatalog{completionHTTPStore: store}, s.access)
	for _, tc := range []struct{ method, path, body string }{{"GET", profileCompletionPath + "/suggestions", ""}, {"POST", profileCompletionPath + "/previews", completionHTTPInput()}, {"GET", profileCompletionPath + "/previews/" + completionHTTPID, ""}, {"POST", profileCompletionPath + "/previews/" + completionHTTPID + "/accept", `{"planDigest":"` + strings.Repeat("a", 64) + `"}`}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest(tc.method, tc.path, tc.body, token, "application/json"))
		if w.Code != 200 {
			t.Fatal("actual New failed exact route", tc.path, w.Code)
		}
	}
}

func (f *completionHTTPStore) ReadOwnProfileCompletionSuggestions(_ context.Context, a agentprofile.PrivateAccess) (apc.Suggestions, error) {
	f.calls++
	f.captured = a
	return f.suggestions, f.failure
}
func (f *completionHTTPStore) PreviewOwnProfileCompletion(_ context.Context, a agentprofile.PrivateAccess, _ apc.PreviewInput) (apc.Preview, error) {
	f.calls++
	f.captured = a
	return f.preview, f.failure
}
func (f *completionHTTPStore) ReadOwnProfileCompletion(_ context.Context, a agentprofile.PrivateAccess, _ string) (apc.Receipt, error) {
	f.calls++
	f.captured = a
	return f.receipt, f.failure
}
func (f *completionHTTPStore) AcceptOwnProfileCompletion(_ context.Context, a agentprofile.PrivateAccess, _ string, _ apc.AcceptInput) (apc.Receipt, error) {
	f.calls++
	f.captured = a
	return f.receipt, f.failure
}

// This mux invokes the real leaf methods, but is deliberately not New's
// registered router. Root registers and verifies New after the shared safe point.
func completionLeafMux(s *server) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET "+profileCompletionPath+"/suggestions", s.getOwnProfileCompletionSuggestions)
	m.HandleFunc("POST "+profileCompletionPath+"/previews", s.previewOwnProfileCompletion)
	m.HandleFunc("GET "+profileCompletionPath+"/previews/{previewID}", s.getOwnProfileCompletion)
	m.HandleFunc("POST "+profileCompletionPath+"/previews/{previewID}/accept", s.acceptOwnProfileCompletion)
	return requestTrace(m)
}
func completionHTTPFixture(t *testing.T) (*server, *completionHTTPStore, string) {
	t.Helper()
	s, _, private, token := privateProfileHTTPFixture(t)
	at := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	value := agentmemorycandidate.Statement("hiking")
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}
	source := apc.Suggestion{MemoryID: privateProfileHTTPForeign, MemoryVersion: 1, Category: "hiking", Value: value, MemoryValidUntil: at.Add(time.Hour)}
	store := &completionHTTPStore{privateProfileHTTPStore: private, suggestions: apc.Suggestions{SchemaVersion: apc.Schema, Owner: owner, AgentID: privateProfileHTTPAgent, ProfileVersion: 1, TargetField: apc.Field, State: "FIELD_EMPTY", Sources: []apc.Suggestion{source}, ObservedAt: at}, preview: apc.Preview{SchemaVersion: apc.Schema, ID: completionHTTPID, Purpose: apc.Purpose, Owner: owner, AgentID: privateProfileHTTPAgent, Source: source, ExpectedProfileVersion: 1, TargetField: apc.Field, Before: []string{}, After: []string{value}, PlanDigest: strings.Repeat("a", 64), ObservedAt: at, ExpiresAt: at.Add(apc.PreviewTTL), Explanation: "新独立声明不随原Memory到期删除"}}
	version := int64(2)
	commitAt := at.Add(time.Second)
	store.receipt = apc.Receipt{SchemaVersion: apc.Schema, ID: completionHTTPID, Purpose: apc.Purpose, Owner: owner, AgentID: privateProfileHTTPAgent, MemoryID: source.MemoryID, MemoryVersion: 1, ExpectedProfileVersion: 1, PlanDigest: store.preview.PlanDigest, State: "COMMITTED", ResultProfileVersion: &version, CommittedAt: &commitAt, CurrentProfileMatches: true, ObservedAt: commitAt, ExpiresAt: store.preview.ExpiresAt}
	s.privateProfiles = store
	return s, store, token
}
func completionHTTPInput() string {
	return `{"previewId":"` + completionHTTPID + `","memoryId":"` + privateProfileHTTPForeign + `","memoryVersion":1,"expectedProfileVersion":1}`
}
func TestProfileMemoryCompletionHTTPLeafClosedTransportAndOwner(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		mutate                   func(*http.Request)
		want                     int
	}{
		{"suggestions", "GET", profileCompletionPath + "/suggestions", "", nil, 200},
		{"preview", "POST", profileCompletionPath + "/previews", completionHTTPInput(), nil, 200},
		{"receipt", "GET", profileCompletionPath + "/previews/" + completionHTTPID, "", nil, 200},
		{"accept", "POST", profileCompletionPath + "/previews/" + completionHTTPID + "/accept", `{"planDigest":"` + strings.Repeat("a", 64) + `"}`, nil, 200},
		{"get_body", "GET", profileCompletionPath + "/suggestions", "{}", nil, 400},
		{"metadata_get_body", "GET", profileCompletionPath + "/previews/" + completionHTTPID, "{}", nil, 400},
		{"confirmed_not_authority", "POST", profileCompletionPath + "/previews", strings.Replace(completionHTTPInput(), `{`, `{"confirmed":true,`, 1), nil, 400},
		{"duplicate", "POST", profileCompletionPath + "/previews", strings.Replace(completionHTTPInput(), `"memoryVersion":1`, `"memoryVersion":1,"memoryVersion":1`, 1), nil, 400},
		{"null", "POST", profileCompletionPath + "/previews", strings.Replace(completionHTTPInput(), `"memoryVersion":1`, `"memoryVersion":null`, 1), nil, 400},
		{"owner_selector", "GET", profileCompletionPath + "/suggestions?ownerId=" + privateProfileHTTPForeign, "", nil, 400},
		{"organization_workspace", "GET", profileCompletionPath + "/suggestions", "", func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }, 403},
		{"anonymous", "GET", profileCompletionPath + "/suggestions", "", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"bad_media", "POST", profileCompletionPath + "/previews", completionHTTPInput(), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"oversize", "POST", profileCompletionPath + "/previews", strings.Repeat("x", apc.MaxBodyBytes+1), nil, 413},
		{"wrong_method", "DELETE", profileCompletionPath + "/previews/" + completionHTTPID, "", nil, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, token := completionHTTPFixture(t)
			r := privateProfileHTTPRequest(tc.method, tc.path, tc.body, token, "application/json")
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			completionLeafMux(s).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == 200 {
				if store.calls != 1 || store.captured.WorkspacePrincipal.ID != privateProfileHTTPOwner || store.captured.SessionDigest == [32]byte{} {
					t.Fatal("server invocation context")
				}
			} else if store.calls != 0 {
				t.Fatal("invalid input reached native port")
			}
			if strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
				t.Fatal("unselected private field leaked")
			}
		})
	}
}
func TestProfileMemoryCompletionHTTPLeafInvalidNativeResponseFailsClosed(t *testing.T) {
	for _, mode := range []string{"wrong_owner", "model_access", "raw_error", "wrong_receipt_digest", "no_capability"} {
		t.Run(mode, func(t *testing.T) {
			s, store, token := completionHTTPFixture(t)
			path := profileCompletionPath + "/suggestions"
			method := "GET"
			body := ""
			switch mode {
			case "wrong_owner":
				store.suggestions.Owner.ID = privateProfileHTTPForeign
			case "model_access":
				store.suggestions.ModelAccess = true
			case "raw_error":
				store.failure = errors.New(privateProfileHTTPMarker)
			case "wrong_receipt_digest":
				store.receipt.PlanDigest = strings.Repeat("b", 64)
				method = "POST"
				path = profileCompletionPath + "/previews/" + completionHTTPID + "/accept"
				body = `{"planDigest":"` + strings.Repeat("a", 64) + `"}`
			case "no_capability":
				s.privateProfiles = store.privateProfileHTTPStore
			}
			w := httptest.NewRecorder()
			completionLeafMux(s).ServeHTTP(w, privateProfileHTTPRequest(method, path, body, token, "application/json"))
			if w.Code != 503 || strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
				t.Fatal("invalid native response/body escaped")
			}
		})
	}
}
