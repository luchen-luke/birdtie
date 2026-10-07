package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Spies verify gateway shape only; PostgreSQL tests independently prove ACLs.
type visibilityHTTPStore struct {
	record                     agentprofile.VisibilityRecord
	projected                  agentprofile.ProjectedRecord
	err                        error
	reads, writes, projections int
	access                     agentprofile.PrivateAccess
	input                      agentprofile.ReplaceVisibilityInput
	digest                     [32]byte
	target                     string
}

func (s *visibilityHTTPStore) ReadOwnAgentProfileVisibility(_ context.Context, a agentprofile.PrivateAccess) (agentprofile.VisibilityRecord, error) {
	s.reads++
	s.access = a
	return s.record, s.err
}
func (s *visibilityHTTPStore) ReplaceOwnAgentProfileVisibility(_ context.Context, a agentprofile.PrivateAccess, i agentprofile.ReplaceVisibilityInput) (agentprofile.VisibilityRecord, error) {
	s.writes++
	s.access = a
	s.input = i
	return s.record, s.err
}
func (s *visibilityHTTPStore) ReadAgentProfileFields(_ context.Context, d [32]byte, target string) (agentprofile.ProjectedRecord, error) {
	s.projections++
	s.digest = d
	s.target = target
	return s.projected, s.err
}

type visibilityHTTPCatalog struct {
	foundation.PublicCatalog
	*visibilityHTTPStore
}

func visibilityHTTPFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *visibilityHTTPStore, string, string) {
	t.Helper()
	old, access, private, token := privateProfileHTTPFixture(t)
	record, err := agentprofile.NewVisibilityRecord(private.record.Profile, agentprofile.DefaultFieldRules(), false)
	if err != nil {
		t.Fatal(err)
	}
	store := &visibilityHTTPStore{record: record, projected: agentprofile.ProjectedRecord{AccountID: privateProfileHTTPOwner, Fields: map[agentprofile.FieldKey]json.RawMessage{agentprofile.FieldDisplayName: json.RawMessage(`"中文测试成员"`)}}}
	old.profileVisibility = store
	raw, err := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: agentprofile.DefaultFieldRules()})
	if err != nil {
		t.Fatal(err)
	}
	return old, access, store, token, string(raw)
}
func visibilityHTTPServe(s *server, kind string, w *httptest.ResponseRecorder, r *http.Request) {
	switch kind {
	case "get":
		s.ownProfileVisibility(w, r)
	case "put":
		s.replaceProfileVisibility(w, r)
	case "projection":
		r.SetPathValue("accountID", privateProfileHTTPOwner)
		s.profileFields(w, r)
	}
}
func TestAgentProfileVisibilityHTTPValidAndCurrentActor(t *testing.T) {
	for _, kind := range []string{"get", "put", "projection"} {
		t.Run(kind, func(t *testing.T) {
			s, a, store, token, body := visibilityHTTPFixture(t)
			method := http.MethodGet
			if kind == "put" {
				method = http.MethodPut
			} else {
				body = ""
			}
			r := privateProfileHTTPRequest(method, "/v1/me/agent-profile-visibility", body, token, "application/json")
			w := httptest.NewRecorder()
			visibilityHTTPServe(s, kind, w, r)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d", w.Code)
			}
			if kind == "projection" {
				if store.digest != a.digest || store.target != privateProfileHTTPOwner || strings.Contains(w.Body.String(), "communityIds") || strings.Contains(w.Body.String(), "profileVersion") {
					t.Fatal("projection exposes authority")
				}
			} else if store.access.SessionDigest != a.digest || store.access.WorkspacePrincipal.ID != privateProfileHTTPOwner {
				t.Fatal("current actor lost")
			}
			if kind == "put" && store.input.ExpectedVersion != 1 {
				t.Fatal("CAS input lost")
			}
		})
	}
	t.Run("anonymous public projection passes zero digest only", func(t *testing.T) {
		s, a, store, _, _ := visibilityHTTPFixture(t)
		r := privateProfileHTTPRequest("GET", "/v1/accounts/target/agent-profile-fields", "", "", "")
		w := httptest.NewRecorder()
		visibilityHTTPServe(s, "projection", w, r)
		if w.Code != 200 || a.calls != 0 || store.digest != ([32]byte{}) {
			t.Fatal("anonymous path incorrect")
		}
	})
}
func TestAgentProfileVisibilityHTTPAuthenticationAndSelectors(t *testing.T) {
	for _, kind := range []string{"get", "put", "projection"} {
		for _, name := range []string{"malformed", "duplicate bearer", "expired", "revoked", "workspace", "empty workspace", "duplicate workspace", "query", "get body", "missing store", "missing auth"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				s, a, store, token, body := visibilityHTTPFixture(t)
				method := "GET"
				if kind == "put" {
					method = "PUT"
				} else {
					body = ""
				}
				r := privateProfileHTTPRequest(method, "/v1/me/agent-profile-visibility", body, token, "application/json")
				want := 401
				switch name {
				case "malformed":
					r.Header.Set("Authorization", "Bearer invalid")
				case "duplicate bearer":
					r.Header.Add("Authorization", r.Header.Get("Authorization"))
				case "expired", "revoked":
					a.err = identity.ErrUnauthorized
				case "workspace", "empty workspace", "duplicate workspace":
					r.Header.Set("X-Birdtie-Organization-Workspace", "")
					if name == "workspace" {
						r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
					}
					if name == "duplicate workspace" {
						r.Header.Add("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
					}
					want = 403
				case "query":
					r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
					want = 400
				case "get body":
					if kind == "put" {
						r.Body = nil
					} else {
						r.Body = httptest.NewRequest("POST", "/", strings.NewReader("{}")).Body
					}
					want = 400
				case "missing store":
					s.profileVisibility = nil
					want = 503
				case "missing auth":
					s.access = nil
					want = 503
				}
				w := httptest.NewRecorder()
				visibilityHTTPServe(s, kind, w, r)
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || store.reads+store.writes+store.projections != 0 {
					t.Fatalf("status=%d want=%d calls=%d", w.Code, want, store.reads+store.writes+store.projections)
				}
				if want == 401 && w.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("missing challenge")
				}
			})
		}
	}
	for _, kind := range []string{"get", "put"} {
		for _, actor := range []string{"anonymous", "organization", "business", "community", "invalid", "zero"} {
			t.Run(kind+"/"+actor, func(t *testing.T) {
				s, a, store, token, body := visibilityHTTPFixture(t)
				want := 403
				if actor == "anonymous" {
					token = ""
					want = 401
				} else if actor == "zero" {
					a.actor.ID = "00000000-0000-0000-0000-000000000000"
				} else {
					a.actor.AccountType = actor
				}
				method := "GET"
				if kind == "put" {
					method = "PUT"
				} else {
					body = ""
				}
				r := privateProfileHTTPRequest(method, "/", body, token, "application/json")
				w := httptest.NewRecorder()
				visibilityHTTPServe(s, kind, w, r)
				if w.Code != want || store.reads+store.writes != 0 {
					t.Fatalf("status=%d", w.Code)
				}
			})
		}
	}
}
func TestAgentProfileVisibilityHTTPInputAndErrorRedaction(t *testing.T) {
	for _, body := range []string{"", `null`, `{}`, `{"expectedVersion":1,"rules":null}`, `{"expectedVersion":1,"expectedVersion":2,"rules":{}}`, `{"expectedVersion":1,"confirmed":true,"rules":{}}`, `{"ownerId":"injected","rules":{}}`, strings.Repeat("x", agentprofile.MaxVisibilityBodyBytes+1)} {
		t.Run(fmt.Sprintf("invalid-%d", len(body)), func(t *testing.T) {
			s, _, store, token, _ := visibilityHTTPFixture(t)
			r := privateProfileHTTPRequest("PUT", "/", body, token, "application/json")
			w := httptest.NewRecorder()
			s.replaceProfileVisibility(w, r)
			want := 400
			if len(body) > agentprofile.MaxVisibilityBodyBytes {
				want = 413
			}
			if w.Code != want || store.writes != 0 {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
	for _, ct := range []string{"", "text/plain", "application/json-invalid"} {
		t.Run("mime-"+ct, func(t *testing.T) {
			s, _, store, token, body := visibilityHTTPFixture(t)
			w := httptest.NewRecorder()
			s.replaceProfileVisibility(w, privateProfileHTTPRequest("PUT", "/", body, token, ct))
			if w.Code != 415 || store.writes != 0 {
				t.Fatal("invalid mime accepted")
			}
		})
	}
	for _, kind := range []string{"get", "put", "projection"} {
		for _, tc := range []struct {
			err    error
			status int
		}{{agentprofile.ErrInvalid, 400}, {agentprofile.ErrConflict, 409}, {agentprofile.ErrForbidden, 403}, {agentprofile.ErrNotFound, 404}, {errors.New(privateProfileHTTPMarker), 503}} {
			t.Run(kind+"/"+fmt.Sprint(tc.status), func(t *testing.T) {
				s, _, store, token, body := visibilityHTTPFixture(t)
				store.err = tc.err
				method := "GET"
				if kind == "put" {
					method = "PUT"
				} else {
					body = ""
				}
				w := httptest.NewRecorder()
				visibilityHTTPServe(s, kind, w, privateProfileHTTPRequest(method, "/", body, token, "application/json"))
				if w.Code != tc.status || strings.Contains(w.Body.String(), privateProfileHTTPMarker) || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d", w.Code)
				}
			})
		}
	}
}
func TestAgentProfileVisibilityHTTPForeignInvalidOutputAndRegisteredRoutes(t *testing.T) {
	for _, kind := range []string{"get", "put", "projection"} {
		for _, invalid := range []string{"foreign", "invalid"} {
			t.Run(kind+"/"+invalid, func(t *testing.T) {
				s, _, store, token, body := visibilityHTTPFixture(t)
				if kind == "projection" {
					if invalid == "foreign" {
						store.projected.AccountID = privateProfileHTTPForeign
					} else {
						store.projected.Fields = map[agentprofile.FieldKey]json.RawMessage{"memory": json.RawMessage(`"private"`)}
					}
				} else {
					if invalid == "foreign" {
						store.record.Profile.OwnerID = privateProfileHTTPForeign
					} else {
						store.record.SchemaVersion = "invalid"
					}
				}
				method := "GET"
				if kind == "put" {
					method = "PUT"
				} else {
					body = ""
				}
				w := httptest.NewRecorder()
				visibilityHTTPServe(s, kind, w, privateProfileHTTPRequest(method, "/", body, token, "application/json"))
				if w.Code != 503 || strings.Contains(w.Body.String(), "private\"") {
					t.Fatalf("status=%d", w.Code)
				}
			})
		}
	}
	t.Run("target IDs", func(t *testing.T) {
		for _, id := range []string{"invalid", "00000000-0000-0000-0000-000000000000"} {
			s, _, store, token, _ := visibilityHTTPFixture(t)
			r := privateProfileHTTPRequest("GET", "/", "", token, "")
			r.SetPathValue("accountID", id)
			w := httptest.NewRecorder()
			s.profileFields(w, r)
			if w.Code != 400 || store.projections != 0 {
				t.Fatalf("status=%d", w.Code)
			}
		}
	})
	for _, kind := range []string{"get", "put", "projection"} {
		t.Run("registered/"+kind, func(t *testing.T) {
			_, a, store, token, body := visibilityHTTPFixture(t)
			handler := New(&visibilityHTTPCatalog{visibilityHTTPStore: store}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			method := "GET"
			path := "/v1/me/agent-profile-visibility"
			if kind == "put" {
				method = "PUT"
			} else {
				body = ""
			}
			if kind == "projection" {
				path = "/v1/accounts/" + privateProfileHTTPOwner + "/agent-profile-fields"
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, privateProfileHTTPRequest(method, path, body, token, "application/json"))
			if w.Code != 200 || w.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}
