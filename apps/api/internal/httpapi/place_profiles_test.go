package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
)

type placeSemanticTransportSpy struct {
	foundation.PublicCatalog
	out    pp.Public
	c      pp.Candidate
	calls  int
	access pp.Access
	err    error
}

func (x *placeSemanticTransportSpy) GetPublicPlaceSemanticProfile(context.Context, string) (pp.Public, error) {
	x.calls++
	return x.out, x.err
}
func (x *placeSemanticTransportSpy) SubmitPlaceSemanticCandidate(_ context.Context, a pp.Access, city, id string, in pp.SubmitInput) (pp.Candidate, bool, error) {
	x.calls++
	x.access = a
	return x.c, true, x.err
}
func (x *placeSemanticTransportSpy) ListPlaceSemanticCandidates(_ context.Context, a pp.Access, _ string) ([]pp.Candidate, error) {
	x.calls++
	x.access = a
	return []pp.Candidate{x.c}, x.err
}
func (x *placeSemanticTransportSpy) ReviewPlaceSemanticCandidate(_ context.Context, a pp.Access, _ string, _ pp.ReviewInput) (pp.Candidate, error) {
	x.calls++
	x.access = a
	return x.c, x.err
}
func (x *placeSemanticTransportSpy) WithdrawPlaceSemanticProfile(_ context.Context, a pp.Access, _ string, in pp.WithdrawInput) (pp.Revision, error) {
	x.calls++
	x.access = a
	return pp.Revision{PlaceID: x.c.PlaceID, State: "withdrawn", Version: in.ExpectedVersion + 1}, x.err
}
func TestPlaceSemanticHTTPTransportPublicBindingAndUnavailable(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000001"
	at := time.Now().UTC()
	p := pp.Public{SchemaVersion: pp.SchemaVersion, PlaceID: id, CityID: "aberdeen-gb", Version: 1, Facts: pp.Facts{GoodFor: []string{"chat"}}, Confidence: pp.Assessment{Kind: "EDITOR_ASSESSMENT_UNCALIBRATED", Level: "LOW"}, CheckedAt: at, Source: pp.Source{Label: "本地合成资料", URL: "https://example.invalid", ObservedAt: at.Add(-time.Minute), ReviewedAt: at, ExpiresAt: at.Add(time.Hour)}}
	for _, mode := range []string{"valid", "wrong_subject", "invalid_confidence", "unavailable", "query_selector", "missing_port"} {
		t.Run(mode, func(t *testing.T) {
			spy := &placeSemanticTransportSpy{out: p}
			s := &server{catalog: spy}
			path := "/v1/places/" + id + "/profile"
			expected := 200
			switch mode {
			case "wrong_subject":
				spy.out.PlaceID = "00000000-0000-4000-8000-000000000002"
				expected = 503
			case "invalid_confidence":
				spy.out.Confidence.Kind = "PROBABILITY"
				expected = 503
			case "unavailable":
				spy.err = pp.ErrUnavailable
				expected = 503
			case "query_selector":
				path += "?privateMemory=true"
				expected = 400
			case "missing_port":
				s.catalog = nil
				expected = 503
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/places/{placeID}/profile", s.getPublicPlaceProfile)
			r := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != expected || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
			if mode == "valid" && !strings.Contains(w.Body.String(), `"state":"UNKNOWN"`) {
				t.Fatal("missing unknown", w.Body.String())
			}
			if mode == "query_selector" && spy.calls != 0 {
				t.Fatal("read on invalid selector")
			}
			if expected != 200 && strings.Contains(w.Body.String(), p.Source.URL) {
				t.Fatal("failed bound response leaked source")
			}
		})
	}
}
func TestPlaceSemanticHTTPTransportStrictWriteNoDispatch(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	spy := &placeSemanticTransportSpy{}
	s := &server{catalog: spy, access: access}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/cities/{cityID}/places/{placeID}/profile-candidates", s.submitPlaceProfileCandidate)
	path := "/v1/cities/aberdeen-gb/places/00000000-0000-4000-8000-000000000001/profile-candidates"
	for _, raw := range []string{`null`, `{"ownerId":"someone"}`, `{"operationId":"a","operationId":"b"}`, `{"OperationId":"a"}`} {
		t.Run(raw, func(t *testing.T) {
			r := privateProfileHTTPRequest("POST", path, raw, token, "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 400 || spy.calls != 0 {
				t.Fatal("malformed input dispatch", w.Code, spy.calls)
			}
		})
	}
	r := privateProfileHTTPRequest("POST", path, `{}`, token, "application/json")
	r.Header.Set("X-Birdtie-Organization-Workspace", "organization:foreign")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 || spy.calls != 0 {
		t.Fatal("workspace inherited", w.Code)
	}
	// This spy proves transport validation only, not a native authorization.
	_, _ = json.Marshal(spy)
}
