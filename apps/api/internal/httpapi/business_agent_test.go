package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// Transport-only fixture; it cannot validate actual source or session authority.
type businessKnowledgeWireSpy struct {
	foundation.PublicCatalog
	calls int
}

func (s *businessKnowledgeWireSpy) ReadOwnBusinessKnowledge(context.Context, businessconsole.Access) (agentbusiness.Snapshot, error) {
	s.calls++
	return agentbusiness.Snapshot{}, agentbusiness.ErrUnavailable
}
func (s *businessKnowledgeWireSpy) ValidateOwnBusinessKnowledge(context.Context, businessconsole.Access, string) error {
	s.calls++
	return agentbusiness.ErrUnavailable
}
func TestBusinessKnowledgeHTTPStrictWireNoAuthorityCalls(t *testing.T) {
	for _, body := range []string{`{"query":"营业时间"}`, `{"query":"营业时间","placeId":"","verified":true}`, `{"query":"营业时间","query":"商家介绍","placeId":""}`, `{"query":"营业时间","placeId":null}`, `{"query":"营业时间","placeId":""} true`} {
		t.Run(body, func(t *testing.T) {
			spy := &businessKnowledgeWireSpy{}
			s := &server{catalog: spy}
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			r.SetPathValue("businessID", "10000000-0000-4000-8000-000000000001")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.askOwnBusinessKnowledge(w, r)
			if w.Code != 400 || spy.calls != 0 {
				t.Fatal(w.Code, spy.calls, w.Body.String())
			}
		})
	}
	for _, header := range []string{"X-Birdtie-Organization-Workspace", "X-Birdtie-Business-Workspace"} {
		spy := &businessKnowledgeWireSpy{}
		s := &server{catalog: spy}
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"营业时间","placeId":""}`))
		r.SetPathValue("businessID", "10000000-0000-4000-8000-000000000001")
		r.Header.Set(header, "claimed")
		w := httptest.NewRecorder()
		s.askOwnBusinessKnowledge(w, r)
		if w.Code != 400 || spy.calls != 0 {
			t.Fatal(w.Code, spy.calls)
		}
	}
}
