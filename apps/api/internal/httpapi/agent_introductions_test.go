package httpapi

import (
	"context"
	ai "github.com/birdtie/birdtie/apps/api/internal/agentintroduction"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const introductionTestSource = "80000000-0000-4000-8000-000000000008"

type introductionHTTPStub struct {
	*privateProfileHTTPStore
	calls int
	got   agentprofile.PrivateAccess
	id    string
	out   ai.Response
	err   error
}

func (s *introductionHTTPStub) ReadOwnIntroductionSuggestions(_ context.Context, a agentprofile.PrivateAccess, id string) (ai.Response, error) {
	s.calls++
	s.got = a
	s.id = id
	return s.out, s.err
}
func TestIntroductionHTTPExactHumanBoundary(t *testing.T) {
	for _, name := range []string{"valid", "anonymous", "workspace", "foreign_actor", "extra_query", "duplicate_source", "missing_source", "body", "denied", "changed", "unavailable", "corrupt_output"} {
		t.Run(name, func(t *testing.T) {
			s, auth, base, token := privateProfileHTTPFixture(t)
			now := time.Now().UTC()
			stub := &introductionHTTPStub{privateProfileHTTPStore: base, out: ai.NewResponse(introductionTestSource, now, now.Add(time.Minute))}
			s.privateProfiles = stub
			r := privateProfileHTTPRequest("GET", "/v1/me/agent-introductions?sourceIntentId="+introductionTestSource, "", token, "")
			want := 200
			switch name {
			case "anonymous":
				r.Header.Del("Authorization")
				want = 401
			case "workspace":
				r.Header.Set("X-Birdtie-Organization-Workspace", "")
				want = 403
			case "foreign_actor":
				auth.actor.AccountType = "organization"
				want = 403
			case "extra_query":
				r.URL.RawQuery += "&ownerId=" + privateProfileHTTPForeign
				want = 400
			case "duplicate_source":
				r.URL.RawQuery += "&sourceIntentId=" + introductionTestSource
				want = 400
			case "missing_source":
				r.URL.RawQuery = ""
				want = 400
			case "body":
				r = privateProfileHTTPRequest("GET", r.URL.String(), "{}", token, "")
				want = 400
			case "denied":
				stub.err = ai.ErrDenied
				want = 403
			case "changed":
				stub.err = ai.ErrChanged
				want = 409
			case "unavailable":
				stub.err = ai.ErrUnavailable
				want = 503
			case "corrupt_output":
				stub.out.SendAllowed = true
				want = 503
			}
			w := httptest.NewRecorder()
			s.ownAgentIntroductionSuggestions(w, r)
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cached human suggestion")
			}
			if want == 200 && (stub.calls != 1 || stub.got.SessionDigest != auth.digest || stub.got.WorkspacePrincipal.ID != auth.actor.ID || stub.id != introductionTestSource) {
				t.Fatal("lost exact native access")
			}
			for _, v := range []string{"PRIVATE_HTTP_NO_DISCLOSURE_CANARY", "SHARED_ACTIVITY\":\"AVAILABLE", "sendAllowed\":true"} {
				if strings.Contains(w.Body.String(), v) {
					t.Fatal("leaked/activated", v)
				}
			}
		})
	}
}
