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
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const memoryHTTPID = "80000000-0000-4000-8000-000000000011"
const memoryHTTPList = "/v1/me/agent-memories"
const memoryHTTPCanary = "PRIVATE_MEMORY_HTTP_CANARY"

// These spies exercise transport boundaries only. Actual authentication,
// current Agent joins, transactions and CAS are tested in the real DB suite.
type memoryHTTPStore struct {
	record   agentmemory.Record
	records  []agentmemory.Record
	err      error
	calls    int
	access   agentprofile.PrivateAccess
	input    agentmemory.PutInput
	expected int64
}

func (s *memoryHTTPStore) ReadOwnMemories(_ context.Context, a agentprofile.PrivateAccess) ([]agentmemory.Record, error) {
	s.calls++
	s.access = a
	return s.records, s.err
}
func (s *memoryHTTPStore) PutOwnMemory(_ context.Context, a agentprofile.PrivateAccess, _ string, in agentmemory.PutInput) (agentmemory.Record, error) {
	s.calls++
	s.access = a
	s.input = in
	return s.record, s.err
}
func (s *memoryHTTPStore) DeleteOwnMemory(_ context.Context, a agentprofile.PrivateAccess, _ string, v int64) (agentmemory.Record, error) {
	s.calls++
	s.access = a
	s.expected = v
	return s.record, s.err
}

type memoryHTTPCatalog struct {
	foundation.PublicCatalog
	*memoryHTTPStore
}

func memoryHTTPFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *memoryHTTPStore, string, string) {
	t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	in := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "sports", Summary: memoryHTTPCanary,
		StructuredValue: json.RawMessage(`{"sport":"羽毛球"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}
	record, err := agentmemory.NewExplicit(memoryHTTPID, privateProfileHTTPAgent,
		actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, 1, in, now, now)
	if err != nil {
		t.Fatal(err)
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: digest}
	store := &memoryHTTPStore{record: record, records: []agentmemory.Record{record}}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return &server{access: access, memories: store}, access, store, token, string(body)
}
func memoryHTTPServe(s *server, method string, rw *httptest.ResponseRecorder, r *http.Request) {
	r.SetPathValue("memoryID", strings.TrimPrefix(r.URL.Path, memoryHTTPList+"/"))
	switch method {
	case "GET":
		s.listOwnAgentMemories(rw, r)
	case "PUT":
		s.putOwnAgentMemory(rw, r)
	case "DELETE":
		s.deleteOwnAgentMemory(rw, r)
	}
}
func memoryHTTPError(t *testing.T, rw *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rw.Code != status {
		t.Fatalf("status=%d want=%d body=%s", rw.Code, status, rw.Body.String())
	}
	if rw.Header().Get("Cache-Control") != "no-store" || strings.Contains(rw.Body.String(), memoryHTTPCanary) {
		t.Fatal("memory error leaked or cacheable")
	}
}
func TestAgentMemoryHTTPAuthentication(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*server, *privateProfileHTTPAccess, *http.Request)
		status int
	}{
		{"anonymous", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"invalid_session", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.err = identity.ErrUnauthorized }, 401},
		{"foreign_actor", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "organization" }, 403},
		{"business", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "business" }, 403},
		{"malformed_actor", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.ID = "not-uuid" }, 403},
		{"org_header", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
		}, 403},
		{"actor_failure", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.err = errors.New(memoryHTTPCanary) }, 503},
		{"nil_auth", func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.access = nil }, 503},
		{"nil_memory", func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.memories = nil }, 503},
		{"owner_query", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
		}, 400},
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, c := range cases {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				s, a, store, token, body := memoryHTTPFixture(t)
				path := memoryHTTPList + "/" + memoryHTTPID
				if method == "GET" {
					path = memoryHTTPList
					body = ""
				}
				if method == "DELETE" {
					body = `{"expectedVersion":1}`
				}
				r := privateProfileHTTPRequest(method, path, body, token, "application/json")
				c.mutate(s, a, r)
				rw := httptest.NewRecorder()
				memoryHTTPServe(s, method, rw, r)
				memoryHTTPError(t, rw, c.status)
				if store.calls != 0 {
					t.Fatal("denied transport reached memory store")
				}
			})
		}
	}
}
func TestAgentMemoryHTTPStrictMutationInput(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		cases := []struct {
			name, body, media string
			status            int
		}{
			{"empty", "", "application/json", 400}, {"null", "null", "application/json", 400},
			{"selector", `{"expectedVersion":1,"ownerId":"` + privateProfileHTTPForeign + `"}`, "application/json", 400},
			{"duplicate", `{"expectedVersion":1,"expectedVersion":2}`, "application/json", 400},
			{"inferred", `{"expectedVersion":1,"sourceType":"INFERRED"}`, "application/json", 400},
			{"confirmed", `{"expectedVersion":1,"confirmed":true}`, "application/json", 400},
			{"trailing", `{"expectedVersion":1}{}`, "application/json", 400},
			{"wrong_media", `{}`, "text/plain", 415}, {"oversize", strings.Repeat(" ", agentmemory.MaxBodyBytes+1), "application/json", 413},
		}
		for _, c := range cases {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				s, _, store, token, _ := memoryHTTPFixture(t)
				rw := httptest.NewRecorder()
				memoryHTTPServe(s, method, rw, privateProfileHTTPRequest(method, memoryHTTPList+"/"+memoryHTTPID, c.body, token, c.media))
				memoryHTTPError(t, rw, c.status)
				if store.calls != 0 {
					t.Fatal("invalid memory body reached writer")
				}
			})
		}
		for _, id := range []string{"bad", "00000000-0000-0000-0000-000000000000"} {
			t.Run(method+"/bad_id_"+id, func(t *testing.T) {
				s, _, store, token, body := memoryHTTPFixture(t)
				rw := httptest.NewRecorder()
				memoryHTTPServe(s, method, rw, privateProfileHTTPRequest(method, memoryHTTPList+"/"+id, body, token, "application/json"))
				memoryHTTPError(t, rw, 400)
				if store.calls != 0 {
					t.Fatal("invalid address reached writer")
				}
			})
		}
	}
}
func TestAgentMemoryHTTPStoreFailuresAndNoPayloadLogs(t *testing.T) {
	errorsToTest := []struct {
		err    error
		status int
	}{{agentmemory.ErrInvalid, 400}, {agentmemory.ErrForbidden, 403},
		{agentmemory.ErrNotFound, 404}, {agentmemory.ErrConflict, 409}, {errors.New(memoryHTTPCanary), 503}}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, c := range errorsToTest {
			t.Run(fmt.Sprintf("%s/%d", method, c.status), func(t *testing.T) {
				s, _, store, token, body := memoryHTTPFixture(t)
				store.err = c.err
				path := memoryHTTPList + "/" + memoryHTTPID
				if method == "GET" {
					body = ""
					path = memoryHTTPList
				}
				if method == "DELETE" {
					body = `{"expectedVersion":1}`
				}
				var captured bytes.Buffer
				previous := log.Writer()
				log.SetOutput(&captured)
				t.Cleanup(func() { log.SetOutput(previous) })
				rw := httptest.NewRecorder()
				memoryHTTPServe(s, method, rw, privateProfileHTTPRequest(method, path, body, token, "application/json"))
				memoryHTTPError(t, rw, c.status)
				if strings.Contains(captured.String(), memoryHTTPCanary) || strings.Contains(captured.String(), token) {
					t.Fatal("memory/token leaked to error log")
				}
			})
		}
	}
}
func TestAgentMemoryHTTPResponseBoundaries(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, name := range []string{"foreign_owner", "invalid_record", "wrong_address", "wrong_state"} {
			t.Run(method+"/"+name, func(t *testing.T) {
				s, _, store, token, body := memoryHTTPFixture(t)
				path := memoryHTTPList + "/" + memoryHTTPID
				if method == "DELETE" {
					body = `{"expectedVersion":1}`
					store.record.Status = agentmemory.StatusDeleted
					store.record.Summary = ""
					store.record.StructuredValue = json.RawMessage(`{}`)
				}
				switch name {
				case "foreign_owner":
					store.record.OwnerID = privateProfileHTTPForeign
				case "invalid_record":
					store.record.Version = 0
				case "wrong_address":
					store.record.ID = privateProfileHTTPForeign
				case "wrong_state":
					store.record.Status = agentmemory.StatusPendingReview
				}
				if method == "GET" {
					path = memoryHTTPList
					body = ""
					store.records = []agentmemory.Record{store.record}
					if name == "wrong_address" {
						store.records = append(store.records, store.record)
						store.records[1].AgentID = privateProfileHTTPForeign
					}
				}
				rw := httptest.NewRecorder()
				memoryHTTPServe(s, method, rw, privateProfileHTTPRequest(method, path, body, token, "application/json"))
				memoryHTTPError(t, rw, 503)
			})
		}
	}
}
func TestAgentMemoryHTTPRegisteredPathsAndHumanGateway(t *testing.T) {
	_, a, store, token, body := memoryHTTPFixture(t)
	h := privateProfileHTTPNew(memoryHTTPCatalog{memoryHTTPStore: store}, a)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			path := memoryHTTPList + "/" + memoryHTTPID
			store.record.Status = agentmemory.StatusActive
			store.record.Summary = memoryHTTPCanary
			store.record.StructuredValue = json.RawMessage(`{"sport":"羽毛球"}`)
			requestBody := body
			if method == "GET" {
				path = memoryHTTPList
				requestBody = ""
				store.records = []agentmemory.Record{store.record}
			}
			if method == "DELETE" {
				requestBody = `{"expectedVersion":1}`
				store.record.Status = agentmemory.StatusDeleted
				store.record.Summary = ""
				store.record.StructuredValue = json.RawMessage(`{}`)
			}
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest(method, path, requestBody, token, "application/json"))
			if rw.Code != 200 || rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("registered Memory path status %d", rw.Code)
			}
			if store.access.SessionDigest != a.digest || store.access.WorkspacePrincipal.ID != privateProfileHTTPOwner {
				t.Fatal("Memory owner not resolved from authenticated native actor")
			}
		})
	}
	for _, path := range []string{"/v1/accounts/" + privateProfileHTTPOwner + "/agent-memories", "/v1/agents/" + privateProfileHTTPAgent + "/memories"} {
		t.Run("no_delegated_"+path, func(t *testing.T) {
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest("GET", path, "", token, ""))
			if rw.Code != 404 {
				t.Fatal("unexpected delegated/public memory route")
			}
		})
	}
}
