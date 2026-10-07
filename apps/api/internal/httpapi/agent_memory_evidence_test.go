package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const evidenceHTTPID = "80000000-0000-4000-8000-000000000021"
const evidenceHTTPSource = "80000000-0000-4000-8000-000000000022"
const evidenceHTTPCanary = "PRIVATE_EVIDENCE_HTTP_CANARY"

// Only transport spies: no assertion here certifies a real session, source,
// active Agent, current ACL, inference permission or production deployment.
type evidenceHTTPStore struct {
	evidence          agentmemory.Evidence
	provenance        agentmemory.Provenance
	err               error
	read, put, remove int
	access            agentmemory.Access
	memoryID, id      string
	input             agentmemory.EvidenceReferenceInput
	expected          int64
}

func (s *evidenceHTTPStore) PutOwnMemoryEvidence(_ context.Context, a agentmemory.Access, mid, id string, in agentmemory.EvidenceReferenceInput) (agentmemory.Evidence, error) {
	s.put++
	s.access, s.memoryID, s.id, s.input = a, mid, id, in
	return s.evidence, s.err
}
func (s *evidenceHTTPStore) RemoveOwnMemoryEvidence(_ context.Context, a agentmemory.Access, mid, id string, expected int64) (agentmemory.Evidence, error) {
	s.remove++
	s.access, s.memoryID, s.id, s.expected = a, mid, id, expected
	return s.evidence, s.err
}
func (s *evidenceHTTPStore) ReadOwnMemoryProvenance(_ context.Context, a agentmemory.Access, mid string) (agentmemory.Provenance, error) {
	s.read++
	s.access, s.memoryID = a, mid
	return s.provenance, s.err
}
func (s *evidenceHTTPStore) calls() int { return s.read + s.put + s.remove }

type evidenceHTTPCatalog struct {
	foundation.PublicCatalog
	*memoryHTTPStore
	*evidenceHTTPStore
}

func evidenceHTTPFixture(t *testing.T, method string) (*server, *privateProfileHTTPAccess, *memoryHTTPStore, *evidenceHTTPStore, string, string) {
	t.Helper()
	s, a, m, token, _ := memoryHTTPFixture(t)
	event := m.record.CreatedAt
	e := agentmemory.Evidence{SchemaVersion: agentmemory.EvidenceSchemaV1, ID: evidenceHTTPID, MemoryID: memoryHTTPID, MemoryVersion: 1,
		AgentID: privateProfileHTTPAgent, OwnerType: actorref.Person, OwnerID: privateProfileHTTPOwner, Version: 1,
		Source: &agentevent.SourceReference{Type: agentevent.MomentSource, ID: evidenceHTTPSource,
			Owner: actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 1}},
		SignalType: agentmemory.SignalManualReference, Weight: 1, ObservedAt: event, EventTime: &event, CreatedAt: event, Status: agentmemory.EvidenceCurrent}
	p, err := agentmemory.BuildOwnProvenance(m.record, []agentmemory.Evidence{e}, event.Add(time.Minute))
	if err != nil {
		t.Fatal("cannot construct synthetic transport metadata")
	}
	store := &evidenceHTTPStore{evidence: e, provenance: p}
	s.memoryEvidence = store
	body := `{"expectedMemoryVersion":1,"sourceType":"MOMENT","sourceId":"` + evidenceHTTPSource + `"}`
	if method == http.MethodDelete {
		body = `{"expectedVersion":1}`
		store.evidence.Version = 2
		store.evidence.Status = agentmemory.EvidenceRemoved
		store.evidence.Source = nil
		store.evidence.EventTime = nil
		store.evidence.SignalType = ""
		store.evidence.Weight = 0
	}
	if method == http.MethodGet {
		body = ""
	}
	return s, a, m, store, token, body
}
func evidenceHTTPPath(method string) string {
	path := memoryHTTPList + "/" + memoryHTTPID
	if method == http.MethodGet {
		return path + "/provenance"
	}
	return path + "/evidence/" + evidenceHTTPID
}
func evidenceHTTPServe(s *server, rw *httptest.ResponseRecorder, r *http.Request) {
	if r.PathValue("memoryID") == "" {
		r.SetPathValue("memoryID", memoryHTTPID)
	}
	if r.PathValue("evidenceID") == "" {
		r.SetPathValue("evidenceID", evidenceHTTPID)
	}
	switch r.Method {
	case http.MethodGet:
		s.readOwnMemoryProvenance(rw, r)
	case http.MethodPut:
		s.putOwnMemoryEvidence(rw, r)
	case http.MethodDelete:
		s.removeOwnMemoryEvidence(rw, r)
	}
}
func evidenceHTTPError(t *testing.T, rw *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rw.Code != want {
		t.Fatalf("Evidence boundary status=%d want=%d", rw.Code, want)
	}
	if rw.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Evidence error is cacheable")
	}
	for _, secret := range []string{evidenceHTTPCanary, memoryHTTPCanary, evidenceHTTPSource} {
		if strings.Contains(rw.Body.String(), secret) {
			t.Fatal("Evidence error disclosed source/private payload")
		}
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(rw.Body.Bytes(), &envelope) != nil || envelope["error"] == nil || envelope["data"] != nil {
		t.Fatal("Evidence failure returned data/non-JSON")
	}
}

func TestMemoryEvidenceHTTPRejectsCorruptedProvenanceDisplay(t *testing.T) {
	for _, item := range []struct {
		name string
		edit func(*agentmemory.Provenance)
	}{
		{"private_explanation", func(p *agentmemory.Provenance) { p.Explanation = evidenceHTTPCanary }},
		{"invented_experience", func(p *agentmemory.Provenance) { p.Explanation = "本人参加过该活动并喜欢该地点" }},
		{"incorrect_count", func(p *agentmemory.Provenance) { p.Explanation = "本人关联了2条私人记录" }},
		{"nil_evidence", func(p *agentmemory.Provenance) { p.Evidence = nil; p.Explanation = "本人明确填写" }},
		{"duplicate_evidence_id", func(p *agentmemory.Provenance) {
			p.Evidence = append(p.Evidence, p.Evidence[0])
			p.Explanation = "本人关联了2条私人记录"
		}},
		{"duplicate_source_address", func(p *agentmemory.Provenance) {
			e := p.Evidence[0]
			e.ID = privateProfileHTTPForeign
			p.Evidence = append(p.Evidence, e)
			p.Explanation = "本人关联了2条私人记录"
		}},
		{"over_100", func(p *agentmemory.Provenance) {
			for i := 1; i <= 100; i++ {
				e := p.Evidence[0]
				e.ID = fmt.Sprintf("81000000-0000-4000-8000-%012d", i)
				source := *e.Source
				source.ID = fmt.Sprintf("82000000-0000-4000-8000-%012d", i)
				e.Source = &source
				p.Evidence = append(p.Evidence, e)
			}
			p.Explanation = "本人关联了101条私人记录"
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			s, _, _, store, token, _ := evidenceHTTPFixture(t, http.MethodGet)
			item.edit(&store.provenance)
			rw := httptest.NewRecorder()
			evidenceHTTPServe(s, rw, privateProfileHTTPRequest(http.MethodGet, evidenceHTTPPath(http.MethodGet), "", token, ""))
			evidenceHTTPError(t, rw, 503)
		})
	}
}

func TestMemoryEvidenceHTTPDeniedActorAndSelectors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		edit   func(*server, *privateProfileHTTPAccess, *http.Request)
	}{
		{"anonymous", 401, func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.Header.Del("Authorization") }},
		{"wrong_scheme", 401, func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Set("Authorization", "Basic invalid")
		}},
		{"duplicate_auth", 401, func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Add("Authorization", r.Header.Get("Authorization"))
		}},
		{"expired_session", 401, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.err = identity.ErrUnauthorized }},
		{"organization", 403, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "organization" }},
		{"business", 403, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "business" }},
		{"community", 403, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "community" }},
		{"invalid_actor", 403, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.ID = "invalid" }},
		{"org_header_presence", 403, func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
		}},
		{"nil_auth", 503, func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.access = nil }},
		{"nil_memory", 503, func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.memories = nil }},
		{"nil_evidence", 503, func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.memoryEvidence = nil }},
		{"auth_failure", 503, func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.err = errors.New(evidenceHTTPCanary) }},
	}
	for _, query := range []string{"ownerId=" + privateProfileHTTPForeign, "agentId=" + privateProfileHTTPForeign, "sourceId=" + evidenceHTTPSource, "sourceType=MOMENT", "includeRemoved=true", "confirmed=true", "page=1", "unknown", "ownerId=x&ownerId=y"} {
		q := query
		cases = append(cases, struct {
			name   string
			status int
			edit   func(*server, *privateProfileHTTPAccess, *http.Request)
		}{"query_" + q, 400, func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.URL.RawQuery = q }})
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		for _, item := range cases {
			t.Run(method+"/"+item.name, func(t *testing.T) {
				s, a, m, store, token, body := evidenceHTTPFixture(t, method)
				r := privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json")
				item.edit(s, a, r)
				rw := httptest.NewRecorder()
				evidenceHTTPServe(s, rw, r)
				evidenceHTTPError(t, rw, item.status)
				if store.calls() != 0 || m.calls != 0 {
					t.Fatal("denied actor/selectors dispatched domain operation")
				}
			})
		}
	}
}

func TestMemoryEvidenceHTTPStrictBodiesAndAddresses(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, item := range []struct {
			name, body, media string
			status            int
		}{
			{"empty", "", "application/json", 400}, {"null", "null", "application/json", 400}, {"array", "[]", "application/json", 400},
			{"owner_claim", `{"expectedVersion":1,"ownerId":"` + privateProfileHTTPForeign + `"}`, "application/json", 400},
			{"duplicate", `{"expectedVersion":1,"expectedVersion":2}`, "application/json", 400},
			{"trailing", `{"expectedVersion":1}{}`, "application/json", 400},
			{"case_key", `{"ExpectedVersion":1}`, "application/json", 400},
			{"missing_media", `{}`, "", 415}, {"wrong_media", `{}`, "text/plain", 415},
			{"oversize", strings.Repeat(" ", agentmemory.MaxBodyBytes+1), "application/json", 413},
		} {
			t.Run(method+"/"+item.name, func(t *testing.T) {
				s, _, _, store, token, _ := evidenceHTTPFixture(t, method)
				rw := httptest.NewRecorder()
				evidenceHTTPServe(s, rw, privateProfileHTTPRequest(method, evidenceHTTPPath(method), item.body, token, item.media))
				evidenceHTTPError(t, rw, item.status)
				if store.calls() != 0 {
					t.Fatal("invalid input reached Evidence store")
				}
			})
		}
		for _, key := range []string{"owner", "agentId", "sourceVersion", "weight", "observedAt", "eventTime", "status", "consent", "verified", "confirmed", "modelUse", "body", "title"} {
			t.Run(method+"/unknown_"+key, func(t *testing.T) {
				s, _, _, store, token, body := evidenceHTTPFixture(t, method)
				body = strings.TrimSuffix(body, "}") + `,"` + key + `":"` + evidenceHTTPCanary + `"}`
				rw := httptest.NewRecorder()
				evidenceHTTPServe(s, rw, privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json"))
				evidenceHTTPError(t, rw, 400)
				if store.calls() != 0 {
					t.Fatal("caller permission/private field reached store")
				}
			})
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		for _, field := range []string{"memoryID", "evidenceID"} {
			if method == http.MethodGet && field == "evidenceID" {
				continue
			}
			for _, id := range []string{"bad", "00000000-0000-0000-0000-000000000000", " " + evidenceHTTPID} {
				t.Run(method+"/"+field+"/"+id, func(t *testing.T) {
					s, _, _, store, token, body := evidenceHTTPFixture(t, method)
					r := privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json")
					r.SetPathValue(field, id)
					rw := httptest.NewRecorder()
					evidenceHTTPServe(s, rw, r)
					evidenceHTTPError(t, rw, 400)
					if store.calls() != 0 {
						t.Fatal("invalid path reached store")
					}
				})
			}
		}
	}
	for _, body := range []string{"{}", " ", "x"} {
		t.Run("GETbody_"+body, func(t *testing.T) {
			s, _, _, store, token, _ := evidenceHTTPFixture(t, http.MethodGet)
			rw := httptest.NewRecorder()
			evidenceHTTPServe(s, rw, privateProfileHTTPRequest(http.MethodGet, evidenceHTTPPath(http.MethodGet), body, token, ""))
			evidenceHTTPError(t, rw, 400)
			if store.calls() != 0 {
				t.Fatal("GET body dispatched provenance")
			}
		})
	}
}

func TestMemoryEvidenceHTTPBackendErrorsNeverDisclosePayload(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		for _, item := range []struct {
			err    error
			status int
		}{
			{agentmemory.ErrInvalid, 400}, {agentmemory.ErrForbidden, 403}, {agentmemory.ErrNotFound, 404}, {agentmemory.ErrConflict, 409}, {agentmemory.ErrUnavailable, 503}, {errors.New(evidenceHTTPCanary), 503},
		} {
			t.Run(fmt.Sprintf("%s/%d/%s", method, item.status, item.err), func(t *testing.T) {
				s, _, _, store, token, body := evidenceHTTPFixture(t, method)
				store.err = item.err
				store.provenance.Explanation = evidenceHTTPCanary
				var captured bytes.Buffer
				previous := log.Writer()
				log.SetOutput(&captured)
				defer log.SetOutput(previous)
				rw := httptest.NewRecorder()
				evidenceHTTPServe(s, rw, privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json"))
				evidenceHTTPError(t, rw, item.status)
				if strings.Contains(captured.String(), evidenceHTTPCanary) || strings.Contains(captured.String(), token) || store.calls() != 1 {
					t.Fatal("store error log leaked or dispatched incorrectly")
				}
			})
		}
	}
}

func TestMemoryEvidenceHTTPRejectsForeignOrMalformedBackendRecords(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodGet} {
		for _, item := range []struct {
			name string
			edit func(*agentmemory.Evidence)
		}{
			{"schema", func(e *agentmemory.Evidence) { e.SchemaVersion = "unknown" }},
			{"owner", func(e *agentmemory.Evidence) {
				e.OwnerID = privateProfileHTTPForeign
				if e.Source != nil {
					e.Source.Owner.ID = privateProfileHTTPForeign
				}
			}},
			{"owner_type", func(e *agentmemory.Evidence) { e.OwnerType = actorref.Organization }},
			{"memory", func(e *agentmemory.Evidence) { e.MemoryID = privateProfileHTTPForeign }},
			{"invalid_agent", func(e *agentmemory.Evidence) { e.AgentID = "bad" }},
			{"version_zero", func(e *agentmemory.Evidence) { e.Version = 0 }},
			{"created_before_observed", func(e *agentmemory.Evidence) { e.CreatedAt = e.ObservedAt.Add(-time.Nanosecond) }},
		} {
			t.Run(method+"/"+item.name, func(t *testing.T) {
				s, _, _, store, token, body := evidenceHTTPFixture(t, method)
				if method == http.MethodGet {
					item.edit(&store.provenance.Evidence[0])
				} else {
					item.edit(&store.evidence)
				}
				rw := httptest.NewRecorder()
				evidenceHTTPServe(s, rw, privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json"))
				evidenceHTTPError(t, rw, 503)
			})
		}
	}
	for _, item := range []struct {
		name string
		edit func(*agentmemory.Evidence)
	}{
		{"wrong_address", func(e *agentmemory.Evidence) { e.ID = privateProfileHTTPForeign }},
		{"wrong_memory_version", func(e *agentmemory.Evidence) { e.MemoryVersion = 2 }},
		{"wrong_source_address", func(e *agentmemory.Evidence) { e.Source.ID = privateProfileHTTPForeign }},
		{"source_owner", func(e *agentmemory.Evidence) { e.Source.Owner.ID = privateProfileHTTPForeign }},
		{"signal", func(e *agentmemory.Evidence) { e.SignalType = "MODEL_INFERENCE" }},
		{"weight", func(e *agentmemory.Evidence) { e.Weight = 0.8 }},
		{"removed", func(e *agentmemory.Evidence) {
			e.Status = agentmemory.EvidenceRemoved
			e.Source = nil
			e.EventTime = nil
			e.SignalType = ""
			e.Weight = 0
		}},
	} {
		t.Run("PUT/"+item.name, func(t *testing.T) {
			s, _, _, store, token, body := evidenceHTTPFixture(t, http.MethodPut)
			item.edit(&store.evidence)
			rw := httptest.NewRecorder()
			evidenceHTTPServe(s, rw, privateProfileHTTPRequest(http.MethodPut, evidenceHTTPPath(http.MethodPut), body, token, "application/json"))
			evidenceHTTPError(t, rw, 503)
		})
	}
	for _, item := range []struct {
		name string
		edit func(*agentmemory.Evidence)
	}{
		{"payload_after_remove", func(e *agentmemory.Evidence) { e.SignalType = agentmemory.SignalManualReference }},
		{"wrong_address", func(e *agentmemory.Evidence) { e.ID = privateProfileHTTPForeign }},
		{"wrong_CAS_version", func(e *agentmemory.Evidence) { e.Version = 5 }},
	} {
		t.Run("DELETE/"+item.name, func(t *testing.T) {
			s, _, _, store, token, body := evidenceHTTPFixture(t, http.MethodDelete)
			item.edit(&store.evidence)
			rw := httptest.NewRecorder()
			evidenceHTTPServe(s, rw, privateProfileHTTPRequest(http.MethodDelete, evidenceHTTPPath(http.MethodDelete), body, token, "application/json"))
			evidenceHTTPError(t, rw, 503)
		})
	}
}

func TestMemoryEvidenceHTTPRegisteredNativeOwnerPaths(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			_, a, m, store, token, body := evidenceHTTPFixture(t, method)
			h := privateProfileHTTPNew(evidenceHTTPCatalog{memoryHTTPStore: m, evidenceHTTPStore: store}, a)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest(method, evidenceHTTPPath(method), body, token, "application/json; charset=utf-8"))
			if rw.Code != 200 || rw.Header().Get("Cache-Control") != "no-store" || rw.Header().Get("X-Request-ID") == "" {
				t.Fatalf("registered native Evidence path status=%d", rw.Code)
			}
			if store.calls() != 1 || m.calls != 0 || store.access.SessionDigest != a.digest || store.access.WorkspacePrincipal != (actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}) || store.memoryID != memoryHTTPID {
				t.Fatal("Evidence address/owner not derived from current actor or unexpectedly mutated Memory")
			}
			if method == http.MethodPut && (store.id != evidenceHTTPID || store.input.SourceID != evidenceHTTPSource || store.input.ExpectedMemoryVersion != 1) {
				t.Fatal("reference input not preserved")
			}
			if method == http.MethodDelete && (store.id != evidenceHTTPID || store.expected != 1) {
				t.Fatal("detach CAS not preserved")
			}
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(rw.Body.Bytes(), &envelope) != nil {
				t.Fatal("non-JSON response")
			}
			if method == http.MethodGet {
				var got agentmemory.Provenance
				if json.Unmarshal(envelope.Data, &got) != nil || !reflect.DeepEqual(got, store.provenance) {
					t.Fatal("provenance response differs")
				}
			}
			if method == http.MethodDelete {
				for _, value := range []string{evidenceHTTPSource, `"source"`, `"eventTime"`, `"signalType"`, `"weight"`} {
					if strings.Contains(string(envelope.Data), value) {
						t.Fatal("removed response returned source metadata")
					}
				}
			}
		})
	}
	for _, path := range []string{"/v1/accounts/" + privateProfileHTTPOwner + "/agent-memories/" + memoryHTTPID + "/provenance", "/v1/agents/" + privateProfileHTTPAgent + "/memories/" + memoryHTTPID + "/provenance"} {
		t.Run("no_delegated_"+path, func(t *testing.T) {
			_, a, m, store, token, _ := evidenceHTTPFixture(t, http.MethodGet)
			h := privateProfileHTTPNew(evidenceHTTPCatalog{memoryHTTPStore: m, evidenceHTTPStore: store}, a)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest(http.MethodGet, path, "", token, ""))
			if rw.Code != 404 || store.calls() != 0 {
				t.Fatal("delegated/public Evidence gateway unexpectedly exists")
			}
		})
	}
}
