package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemoryCandidateHTTPStrictHumanWire(t *testing.T) {
	for _, raw := range []string{`{"category":"sports","category":"culture"}`, `{"sources":[{"type":"MOMENT","id":"a","id":"b"}]}`, `{"confirmed":true}`, `{"assessment":{"value":1}}`, `null`, `{} {}`, `{"owner":"foreign"}`, `{"sources":[{"type":"MOMENT","id":"x","permissions":["all"]}]}`, `{"Category":"sports","sources":[],"validUntil":"2026-10-04T13:00:00Z"}`, `{"category":"sports","sources":[{"Type":"MOMENT","id":"80000000-0000-4000-8000-000000000011"}],"validUntil":"2026-10-04T13:00:00Z"}`} {
		t.Run(raw, func(t *testing.T) {
			var d agentmemorycandidate.HumanDraft
			if candidateJSON([]byte(raw), &d) {
				t.Fatal("unsafe wire decoded")
			}
		})
	}
	var d agentmemorycandidate.HumanDraft
	if !candidateJSON([]byte(`{"category":"sports","sources":[],"validUntil":"2026-10-04T13:00:00Z"}`), &d) {
		t.Fatal("valid wire denied")
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "assessment") {
		t.Fatal("probability")
	}
}
func TestMemoryCandidateHTTPNilGatewayFailClosed(t *testing.T) {
	s, _, _, token, _ := memoryHTTPFixture(t)
	w := httptest.NewRecorder()
	r := privateProfileHTTPRequest("GET", "/v1/me/agent-memory-candidates", "", token, "application/json")
	s.listOwnMemoryCandidates(w, r)
	if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
}
