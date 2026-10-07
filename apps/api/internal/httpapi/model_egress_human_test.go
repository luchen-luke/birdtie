package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"net/http/httptest"
	"testing"
)

type modelEgressHumanSpy struct {
	modelEgressHTTPSpy
	reads int
}

func (s *modelEgressHumanSpy) ListOwnModelEgressOptions(context.Context, agentevent.Access) (modelegressbudget.HumanOptions, error) {
	s.reads++
	return modelegressbudget.HumanOptions{SchemaVersion: modelegressbudget.HumanSchemaVersion, Options: []modelegressbudget.HumanOption{}, ModelAccess: "UNAVAILABLE"}, nil
}
func (s *modelEgressHumanSpy) ListOwnModelEgressReceipts(context.Context, agentevent.Access) ([]modelegressbudget.HumanReceipt, error) {
	s.reads++
	return []modelegressbudget.HumanReceipt{}, nil
}
func (s *modelEgressHumanSpy) ReadOwnModelEgressReceipt(context.Context, agentevent.Access, string) (modelegressbudget.HumanReceipt, error) {
	s.reads++
	return modelegressbudget.HumanReceipt{}, modelegressbudget.ErrDenied
}
func TestModelEgressHumanHTTPStrictRegisteredReadBoundary(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	spy := &modelEgressHumanSpy{}
	h := privateProfileHTTPNew(spy, access)
	for _, path := range []string{modelEgressHTTPBase + "/options", modelEgressHTTPBase + "/previews", modelEgressHTTPBase + "/previews/" + privateProfileHTTPAgent} {
		for _, tail := range []string{"?", "?ownerId=x"} {
			before := spy.reads
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", path+tail, "", token, ""))
			if w.Code != 400 || spy.reads != before {
				t.Fatal("GET query control", w.Code)
			}
		}
		for _, body := range []string{"{}", "null"} {
			before := spy.reads
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", path, body, token, "application/json"))
			if w.Code != 400 || spy.reads != before {
				t.Fatal("GET body control", w.Code)
			}
		}
		before := spy.reads
		r := privateProfileHTTPRequest("GET", path, "", token, "")
		r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || spy.reads != before {
			t.Fatal("empty workspace bypass", w.Code)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("GET", path, "", token, ""))
		want := 200
		if path == modelEgressHTTPBase+"/previews/"+privateProfileHTTPAgent {
			want = 403
		}
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || spy.reads != before+1 {
			t.Fatal("registered reader", w.Code, w.Body.String())
		}
	}
}
