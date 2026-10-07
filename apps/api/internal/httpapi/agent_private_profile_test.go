package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// These are HTTP boundary spies, not a session/DB/provider implementation or
// production verification. Nil embedded capabilities fail if private editing
// accidentally calls public Profile, grants, Memory or other domain methods.
const (
	privateProfileHTTPOwner      = "80000000-0000-4000-8000-000000000001"
	privateProfileHTTPAgent      = "80000000-0000-4000-8000-000000000002"
	privateProfileHTTPForeign    = "80000000-0000-4000-8000-000000000003"
	privateProfileHTTPPath       = "/v1/me/agent-private-profile"
	privateProfileHTTPMarker     = "PRIVATE_HTTP_NO_DISCLOSURE_CANARY"
	privateProfileHTTPValidInput = `{"expectedVersion":1,"fields":{"agentNotes":"仅本人填写的说明","languagePreferences":["中文"]}}`
)

type privateProfileHTTPAccess struct {
	identity.AccessStore
	actor  identity.Actor
	digest [32]byte
	err    error
	calls  int
}

func (s *privateProfileHTTPAccess) Authenticate(_ context.Context, digest [32]byte) (identity.Actor, error) {
	s.calls++
	if digest != s.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return s.actor, s.err
}

type privateProfileHTTPStore struct {
	record       agentprofile.PrivateRecord
	err          error
	readCalls    int
	replaceCalls int
	access       agentprofile.PrivateAccess
	input        agentprofile.ReplacePrivateInput
}

func (s *privateProfileHTTPStore) ReadOwnAgentPrivateProfile(_ context.Context, access agentprofile.PrivateAccess) (agentprofile.PrivateRecord, error) {
	s.readCalls++
	s.access = access
	return s.record, s.err
}

func (s *privateProfileHTTPStore) ReplaceOwnAgentPrivateProfile(_ context.Context, access agentprofile.PrivateAccess, input agentprofile.ReplacePrivateInput) (agentprofile.PrivateRecord, error) {
	s.replaceCalls++
	s.access, s.input = access, input
	return s.record, s.err
}

type privateProfileHTTPCatalog struct {
	foundation.PublicCatalog
	*privateProfileHTTPStore
}

func privateProfileHTTPFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *privateProfileHTTPStore, string) {
	t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal("could not create synthetic boundary credential")
	}
	meta, err := agentprofile.New(privateProfileHTTPAgent, actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal("invalid synthetic metadata fixture")
	}
	private, err := agentprofile.NewPrivateRecord(meta, agentprofile.PrivateFields{AgentNotes: privateProfileHTTPMarker}, true)
	if err != nil {
		t.Fatal("invalid synthetic private fixture")
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: digest}
	store := &privateProfileHTTPStore{record: private}
	return &server{access: access, privateProfiles: store}, access, store, token
}

func privateProfileHTTPRequest(method, path, body, token, contentType string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func privateProfileHTTPServe(s *server, method string, rw *httptest.ResponseRecorder, req *http.Request) {
	if method == http.MethodGet {
		s.getOwnAgentPrivateProfile(rw, req)
	} else {
		s.replaceOwnAgentPrivateProfile(rw, req)
	}
}

func privateProfileHTTPAssertError(t *testing.T, rw *httptest.ResponseRecorder, want int, code string) {
	t.Helper()
	if rw.Code != want {
		t.Fatalf("HTTP boundary status %d, want %d", rw.Code, want)
	}
	if rw.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response is cacheable")
	}
	if !strings.HasPrefix(rw.Header().Get("Content-Type"), "application/json") {
		t.Fatal("error response is not JSON")
	}
	if strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
		t.Fatal("private field/database detail disclosed in error response")
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != code {
		t.Fatal("error response changed fixed public code")
	}
}

func TestPrivateAgentProfileHTTPAuthenticationAndActorBoundary(t *testing.T) {
	cases := []struct {
		name          string
		edit          func(*server, *privateProfileHTTPAccess, *privateProfileHTTPStore, *http.Request)
		want          int
		code          string
		wantAuthCalls int
	}{
		{"anonymous", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Del("Authorization")
		}, 401, "unauthorized", 0},
		{"wrong credential scheme", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Set("Authorization", "Basic invalid")
		}, 401, "unauthorized", 0},
		{"malformed opaque credential", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Set("Authorization", "Bearer bts1_invalid")
		}, 401, "unauthorized", 0},
		{"duplicate authorization", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Add("Authorization", r.Header.Get("Authorization"))
		}, 401, "unauthorized", 0},
		{"expired session", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.err = identity.ErrUnauthorized
		}, 401, "unauthorized", 1},
		{"revoked session", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.err = fmt.Errorf("current session revoked: %w", identity.ErrUnauthorized)
		}, 401, "unauthorized", 1},
		{"session service unavailable", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.err = errors.New(privateProfileHTTPMarker)
		}, 503, "private_profile_unavailable", 1},
		{"organization actor", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.AccountType = "organization"
		}, 403, "personal_profile_required", 1},
		{"business actor", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.AccountType = "business"
		}, 403, "personal_profile_required", 1},
		{"community actor", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.AccountType = "community"
		}, 403, "personal_profile_required", 1},
		{"city actor unsupported", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.AccountType = "city"
		}, 403, "personal_profile_required", 1},
		{"missing actor", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor = identity.Actor{}
		}, 403, "personal_profile_required", 1},
		{"malformed actor ID", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.ID = privateProfileHTTPMarker
		}, 403, "personal_profile_required", 1},
		{"all-zero actor ID", func(_ *server, a *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			a.actor.ID = "00000000-0000-0000-0000-000000000000"
		}, 403, "private_profile_forbidden", 1},
		{"organization workspace header", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
		}, 403, "personal_profile_required", 0},
		{"empty workspace header presence", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Set("X-Birdtie-Organization-Workspace", "")
		}, 403, "personal_profile_required", 0},
		{"duplicate workspace headers", func(_ *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, r *http.Request) {
			r.Header.Add("X-Birdtie-Organization-Workspace", "")
			r.Header.Add("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
		}, 403, "personal_profile_required", 0},
		{"missing private store", func(s *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			s.privateProfiles = nil
		}, 503, "private_profile_unavailable", 0},
		{"missing identity access", func(s *server, _ *privateProfileHTTPAccess, _ *privateProfileHTTPStore, _ *http.Request) {
			s.access = nil
		}, 503, "private_profile_unavailable", 0},
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range cases {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				s, access, store, token := privateProfileHTTPFixture(t)
				body := ""
				if method == http.MethodPut {
					body = privateProfileHTTPValidInput
				}
				r := privateProfileHTTPRequest(method, privateProfileHTTPPath, body, token, "application/json")
				tc.edit(s, access, store, r)
				rw := httptest.NewRecorder()
				privateProfileHTTPServe(s, method, rw, r)
				privateProfileHTTPAssertError(t, rw, tc.want, tc.code)
				if store.readCalls+store.replaceCalls != 0 || access.calls != tc.wantAuthCalls {
					t.Fatal("unauthorized boundary reached private Store or wrong authentication path")
				}
				if tc.want == 401 && rw.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("unauthorized response lacks Bearer challenge")
				}
			})
		}
	}
}

func TestPrivateAgentProfileHTTPRejectsSelectorsAndGetBody(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, query := range []string{"ownerId=" + privateProfileHTTPForeign, "agentId=" + privateProfileHTTPAgent, "ownerType=ORGANIZATION", "workspace=organization", "owner=one&owner=two", "fields=public", "irrelevant=", "%zz=invalid"} {
			t.Run(method+" query "+strings.Split(query, "=")[0], func(t *testing.T) {
				s, _, store, token := privateProfileHTTPFixture(t)
				body := ""
				if method == http.MethodPut {
					body = privateProfileHTTPValidInput
				}
				rw := httptest.NewRecorder()
				privateProfileHTTPServe(s, method, rw, privateProfileHTTPRequest(method, privateProfileHTTPPath+"?"+query, body, token, "application/json"))
				privateProfileHTTPAssertError(t, rw, 400, "private_profile_query_not_supported")
				if store.readCalls+store.replaceCalls != 0 {
					t.Fatal("query override reached private Store")
				}
			})
		}
	}
	for _, body := range []string{" ", "{}", `{"ownerId":"` + privateProfileHTTPForeign + `"}`, privateProfileHTTPMarker} {
		t.Run("GET body "+fmt.Sprint(len(body)), func(t *testing.T) {
			s, _, store, token := privateProfileHTTPFixture(t)
			rw := httptest.NewRecorder()
			s.getOwnAgentPrivateProfile(rw, privateProfileHTTPRequest(http.MethodGet, privateProfileHTTPPath, body, token, "application/json"))
			privateProfileHTTPAssertError(t, rw, 400, "private_profile_get_body_not_supported")
			if store.readCalls != 0 {
				t.Fatal("GET body reached private Store")
			}
		})
	}
}

func TestPrivateAgentProfileHTTPStrictPUTInput(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ``}, {"null body", `null`}, {"missing fields", `{"expectedVersion":1}`}, {"missing version", `{"fields":{}}`},
		{"duplicate version", `{"expectedVersion":1,"expectedVersion":2,"fields":{}}`},
		{"escaped duplicate version", `{"expectedVersion":1,"\u0065xpectedVersion":2,"fields":{}}`},
		{"duplicate fields", `{"expectedVersion":1,"fields":{},"fields":{}}`},
		{"duplicate note", `{"expectedVersion":1,"fields":{"agentNotes":"one","agentNotes":"two"}}`},
		{"unknown field", `{"expectedVersion":1,"fields":{"inferredPreference":"` + privateProfileHTTPMarker + `"}}`},
		{"wrong field casing", `{"expectedVersion":1,"fields":{"AgentNotes":"` + privateProfileHTTPMarker + `"}}`},
		{"null fields", `{"expectedVersion":1,"fields":null}`}, {"null note", `{"expectedVersion":1,"fields":{"agentNotes":null}}`},
		{"null preference", `{"expectedVersion":1,"fields":{"personalPreferences":null}}`},
		{"null list item", `{"expectedVersion":1,"fields":{"personalPreferences":[null]}}`},
		{"number list item", `{"expectedVersion":1,"fields":{"personalPreferences":[1]}}`},
		{"object field", `{"expectedVersion":1,"fields":{"agentNotes":{"confirmed":true}}}`},
		{"owner override", `{"expectedVersion":1,"fields":{},"ownerId":"` + privateProfileHTTPForeign + `"}`},
		{"Agent override", `{"expectedVersion":1,"fields":{},"agentId":"` + privateProfileHTTPAgent + `"}`},
		{"organization role override", `{"expectedVersion":1,"fields":{},"ownerType":"ORGANIZATION","role":"owner"}`},
		{"confirmed override", `{"expectedVersion":1,"fields":{},"confirmed":true}`},
		{"grant override", `{"expectedVersion":1,"fields":{},"grant":{"public":true}}`},
		{"profile version override", `{"expectedVersion":1,"fields":{},"profileVersion":2}`},
		{"visibility override", `{"expectedVersion":1,"fields":{"visibility":"PUBLIC"}}`},
		{"zero version", `{"expectedVersion":0,"fields":{}}`}, {"fraction version", `{"expectedVersion":1.0,"fields":{}}`},
		{"exponent version", `{"expectedVersion":1e0,"fields":{}}`}, {"quoted version", `{"expectedVersion":"1","fields":{}}`},
		{"trailing object", `{"expectedVersion":1,"fields":{}} {}`}, {"trailing null", `{"expectedVersion":1,"fields":{}} null`},
		{"malformed syntax", `{"expectedVersion":1,"fields":{},}`}, {"invalid surrogate", `{"expectedVersion":1,"fields":{"agentNotes":"\ud800"}}`},
		{"invalid UTF8", "{\"expectedVersion\":1,\"fields\":{\"agentNotes\":\"\xff\"}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, store, token := privateProfileHTTPFixture(t)
			rw := httptest.NewRecorder()
			s.replaceOwnAgentPrivateProfile(rw, privateProfileHTTPRequest(http.MethodPut, privateProfileHTTPPath, tc.body, token, "application/json"))
			privateProfileHTTPAssertError(t, rw, 400, "invalid_private_profile")
			if store.replaceCalls != 0 || store.readCalls != 0 {
				t.Fatal("invalid body reached private Store")
			}
		})
	}
}

func TestPrivateAgentProfileHTTPPUTBoundsAndMIME(t *testing.T) {
	for _, tc := range []struct {
		name, body, mime string
		want             int
		code             string
	}{
		{"missing MIME", privateProfileHTTPValidInput, "", 415, "json_required"},
		{"text MIME", privateProfileHTTPValidInput, "text/plain", 415, "json_required"},
		{"form MIME", privateProfileHTTPValidInput, "application/x-www-form-urlencoded", 415, "json_required"},
		{"json suffix unsupported", privateProfileHTTPValidInput, "application/problem+json", 415, "json_required"},
		{"trailing empty MIME parameter", privateProfileHTTPValidInput, "application/json;", 200, ""},
		{"broken MIME parameter", privateProfileHTTPValidInput, "application/json; charset=", 415, "json_required"},
		{"case insensitive MIME", privateProfileHTTPValidInput, "Application/JSON; charset=utf-8", 200, ""},
		{"exact 16KiB", privateProfileHTTPValidInput + strings.Repeat(" ", agentprofile.MaxPrivateBodyBytes-len(privateProfileHTTPValidInput)), "application/json", 200, ""},
		{"over 16KiB", privateProfileHTTPValidInput + strings.Repeat(" ", agentprofile.MaxPrivateBodyBytes-len(privateProfileHTTPValidInput)+1), "application/json", 413, "private_profile_body_too_large"},
		{"over semantic field bytes", `{"expectedVersion":1,"fields":{"agentNotes":"` + strings.Repeat("字", 2000) + `","availability":"` + strings.Repeat("字", 2000) + `","privateCityHistory":"` + strings.Repeat("a", 1500) + `"}}`, "application/json", 400, "invalid_private_profile"},
		{"over item runes", `{"expectedVersion":1,"fields":{"languagePreferences":["` + strings.Repeat("字", 161) + `"]}}`, "application/json", 400, "invalid_private_profile"},
		{"over text runes", `{"expectedVersion":1,"fields":{"agentNotes":"` + strings.Repeat("a", 2001) + `"}}`, "application/json", 400, "invalid_private_profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, store, token := privateProfileHTTPFixture(t)
			rw := httptest.NewRecorder()
			s.replaceOwnAgentPrivateProfile(rw, privateProfileHTTPRequest(http.MethodPut, privateProfileHTTPPath, tc.body, token, tc.mime))
			if tc.want != 200 {
				privateProfileHTTPAssertError(t, rw, tc.want, tc.code)
				if store.replaceCalls != 0 {
					t.Fatal("invalid MIME/body limit reached Store")
				}
				return
			}
			if rw.Code != 200 || store.replaceCalls != 1 || rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("valid MIME/wire limit rejected or cacheable")
			}
		})
	}
}

func TestPrivateAgentProfileHTTPSuccessUsesOnlyResolvedOwnerAndVersion(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, configured := range []bool{false, true} {
			t.Run(method+fmt.Sprint(configured), func(t *testing.T) {
				s, access, store, token := privateProfileHTTPFixture(t)
				body := ""
				if method == http.MethodPut {
					body = `{"expectedVersion":1,"fields":{"languagePreferences":[" 中文 ","中文","English"],"agentNotes":" 自己的说明 "}}`
					store.record.Profile.ProfileVersion = 2
				}
				if !configured {
					if method == http.MethodPut {
						body = `{"expectedVersion":1,"fields":{}}`
					}
					var err error
					store.record, err = agentprofile.NewPrivateRecord(store.record.Profile, agentprofile.PrivateFields{}, false)
					if err != nil {
						t.Fatal("invalid clear fixture")
					}
				}
				rw := httptest.NewRecorder()
				privateProfileHTTPServe(s, method, rw, privateProfileHTTPRequest(method, privateProfileHTTPPath, body, token, "application/json; charset=utf-8"))
				if rw.Code != 200 || rw.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("private self response failed or cached")
				}
				if store.access.SessionDigest != access.digest || store.access.WorkspacePrincipal.Type != actorref.Person || store.access.WorkspacePrincipal.ID != privateProfileHTTPOwner {
					t.Fatal("Store invocation was not server-resolved exact session owner")
				}
				if access.calls != 1 || store.readCalls+store.replaceCalls != 1 {
					t.Fatal("duplicate/unexpected private capability call")
				}
				if method == http.MethodPut {
					if store.replaceCalls != 1 || store.input.ExpectedVersion != 1 {
						t.Fatal("PUT changed expected version or used read fallback")
					}
					if configured && (!reflect.DeepEqual(store.input.Fields.LanguagePreferences, []string{"中文", "English"}) || store.input.Fields.AgentNotes != "自己的说明") {
						t.Fatal("PUT passed noncanonical private fields")
					}
					if !configured && !agentprofile.PrivateFieldsEmpty(store.input.Fields) {
						t.Fatal("explicit clear fabricated default fields")
					}
				} else if store.readCalls != 1 {
					t.Fatal("GET mutated private profile")
				}
				var payload struct {
					Data agentprofile.PrivateRecord `json:"data"`
				}
				if err := json.Unmarshal(rw.Body.Bytes(), &payload); err != nil || !reflect.DeepEqual(payload.Data, store.record) {
					t.Fatal("response metadata/fields/version contract changed")
				}
				if payload.Data.Configured != configured || payload.Data.Profile.OwnerType != actorref.Person {
					t.Fatal("private response shape invalid")
				}
			})
		}
	}
}

func TestPrivateAgentProfileHTTPStoreErrorsNeverDisclosePrivateDetail(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range []struct {
			name string
			err  error
			want int
			code string
		}{
			{"stale version", fmt.Errorf("private detail: %w", agentprofile.ErrConflict), 409, "private_profile_version_conflict"},
			{"current session revoked in Store", agentprofile.ErrForbidden, 403, "private_profile_forbidden"},
			{"foreign owner denied in Store", agentprofile.ErrForbidden, 403, "private_profile_forbidden"},
			{"inactive exact Agent", agentprofile.ErrUnavailable, 503, "private_profile_unavailable"},
			{"missing profile", agentprofile.ErrNotFound, 404, "private_profile_not_found"},
			{"invalid input", agentprofile.ErrInvalid, 400, "invalid_private_profile"},
			{"raw PostgreSQL detail", errors.New("PG DETAIL fields contains " + privateProfileHTTPMarker), 503, "private_profile_unavailable"},
		} {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				s, _, store, token := privateProfileHTTPFixture(t)
				store.err = tc.err
				body := ""
				if method == http.MethodPut {
					body = privateProfileHTTPValidInput
				}
				rw := httptest.NewRecorder()
				privateProfileHTTPServe(s, method, rw, privateProfileHTTPRequest(method, privateProfileHTTPPath, body, token, "application/json"))
				privateProfileHTTPAssertError(t, rw, tc.want, tc.code)
				if store.readCalls+store.replaceCalls != 1 {
					t.Fatal("expected current-session Store revalidation not called exactly once")
				}
			})
		}
	}
}

func TestPrivateAgentProfileHTTPRejectsForeignOrMalformedOutput(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range []struct {
			name string
			edit func(*agentprofile.PrivateRecord)
		}{
			{"foreign owner", func(p *agentprofile.PrivateRecord) { p.Profile.OwnerID = privateProfileHTTPForeign }},
			{"Organization owner", func(p *agentprofile.PrivateRecord) { p.Profile.OwnerType = actorref.Organization }},
			{"Business owner", func(p *agentprofile.PrivateRecord) { p.Profile.OwnerType = actorref.Business }},
			{"unknown schema", func(p *agentprofile.PrivateRecord) { p.SchemaVersion = "future-schema" }},
			{"missing exact Agent", func(p *agentprofile.PrivateRecord) { p.Profile.AgentID = "" }},
			{"zero version", func(p *agentprofile.PrivateRecord) { p.Profile.ProfileVersion = 0 }},
			{"invalid chronology", func(p *agentprofile.PrivateRecord) { p.Profile.UpdatedAt = p.Profile.CreatedAt.Add(-time.Minute) }},
			{"unconfigured hidden fields", func(p *agentprofile.PrivateRecord) { p.Configured = false }},
			{"too large private note", func(p *agentprofile.PrivateRecord) {
				p.Fields.AgentNotes = privateProfileHTTPMarker + strings.Repeat("a", 2001)
			}},
			{"illegal field controls", func(p *agentprofile.PrivateRecord) { p.Fields.Availability = "\x00" }},
		} {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				s, _, store, token := privateProfileHTTPFixture(t)
				tc.edit(&store.record)
				body := ""
				if method == http.MethodPut {
					body = privateProfileHTTPValidInput
				}
				rw := httptest.NewRecorder()
				privateProfileHTTPServe(s, method, rw, privateProfileHTTPRequest(method, privateProfileHTTPPath, body, token, "application/json"))
				privateProfileHTTPAssertError(t, rw, 503, "private_profile_unavailable")
				if strings.Contains(rw.Body.String(), privateProfileHTTPForeign) || strings.Contains(rw.Body.String(), privateProfileHTTPAgent) {
					t.Fatal("malformed private response leaked metadata")
				}
			})
		}
	}
}

type privateProfileHTTPFailingBody struct{}

func (privateProfileHTTPFailingBody) Read([]byte) (int, error) {
	return 0, errors.New(privateProfileHTTPMarker)
}
func (privateProfileHTTPFailingBody) Close() error { return nil }

func TestPrivateAgentProfileHTTPBodyReadErrorsAreGeneric(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			s, _, store, token := privateProfileHTTPFixture(t)
			r := privateProfileHTTPRequest(method, privateProfileHTTPPath, "", token, "application/json")
			r.Body = privateProfileHTTPFailingBody{}
			rw := httptest.NewRecorder()
			privateProfileHTTPServe(s, method, rw, r)
			want, code := 400, "private_profile_get_body_not_supported"
			if method == http.MethodPut {
				want, code = 413, "private_profile_body_too_large"
			}
			privateProfileHTTPAssertError(t, rw, want, code)
			if store.readCalls+store.replaceCalls != 0 {
				t.Fatal("failed body read reached private Store")
			}
		})
	}
}

func TestPrivateAgentProfileHTTPNilBodyCannotPanicOrMutate(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			s, _, store, token := privateProfileHTTPFixture(t)
			r := privateProfileHTTPRequest(method, privateProfileHTTPPath, "", token, "application/json")
			r.Body = nil
			rw := httptest.NewRecorder()
			privateProfileHTTPServe(s, method, rw, r)
			if method == http.MethodGet {
				if rw.Code != 200 || store.readCalls != 1 || store.replaceCalls != 0 || rw.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("nil GET body did not remain an authenticated read")
				}
				return
			}
			privateProfileHTTPAssertError(t, rw, 400, "invalid_private_profile")
			if store.readCalls+store.replaceCalls != 0 {
				t.Fatal("nil PUT body reached private Store")
			}
		})
	}
}

func privateProfileHTTPNew(catalog foundation.PublicCatalog, access identity.AccessStore) http.Handler {
	return New(catalog, access, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
}

func TestPrivateAgentProfileHTTPConstructorWiresOnlySelfRoutes(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			_, access, store, token := privateProfileHTTPFixture(t)
			h := privateProfileHTTPNew(privateProfileHTTPCatalog{privateProfileHTTPStore: store}, access)
			body := ""
			if method == http.MethodPut {
				body = privateProfileHTTPValidInput
			}
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest(method, privateProfileHTTPPath, body, token, "application/json"))
			if rw.Code != 200 || store.readCalls+store.replaceCalls != 1 || rw.Header().Get("Cache-Control") != "no-store" || !safeRequestID.MatchString(rw.Header().Get("X-Request-ID")) {
				t.Fatal("New did not register/trace the private self route")
			}
			if store.access.WorkspacePrincipal.ID != privateProfileHTTPOwner {
				t.Fatal("constructor route passed untrusted owner")
			}
		})
	}
	t.Run("catalog lacks private capability", func(t *testing.T) {
		_, access, _, token := privateProfileHTTPFixture(t)
		h := privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, access)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, privateProfileHTTPRequest(http.MethodGet, privateProfileHTTPPath, "", token, ""))
		privateProfileHTTPAssertError(t, rw, 503, "private_profile_unavailable")
	})
	t.Run("constructor lacks identity capability", func(t *testing.T) {
		_, _, store, token := privateProfileHTTPFixture(t)
		h := privateProfileHTTPNew(privateProfileHTTPCatalog{privateProfileHTTPStore: store}, nil)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, privateProfileHTTPRequest(http.MethodGet, privateProfileHTTPPath, "", token, ""))
		privateProfileHTTPAssertError(t, rw, 503, "private_profile_unavailable")
		if store.readCalls != 0 {
			t.Fatal("missing identity reached private Store")
		}
	})
	for _, path := range []string{"/v1/accounts/" + privateProfileHTTPOwner + "/agent-private-profile", "/v1/agents/" + privateProfileHTTPAgent + "/private-profile"} {
		t.Run("no alternate "+strings.Split(path, "/")[2], func(t *testing.T) {
			_, access, store, token := privateProfileHTTPFixture(t)
			h := privateProfileHTTPNew(privateProfileHTTPCatalog{privateProfileHTTPStore: store}, access)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest(http.MethodGet, path, "", token, ""))
			if rw.Code != 404 || store.readCalls+store.replaceCalls != 0 {
				t.Fatal("alternate private owner/Agent route became available")
			}
		})
	}
}

func TestPrivateAgentProfileHTTPLogsDoNotContainErrorsFieldsOrCredentials(t *testing.T) {
	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })
	s, access, store, token := privateProfileHTTPFixture(t)
	store.err = errors.New("PostgreSQL DETAIL private body=" + privateProfileHTTPMarker)
	r := privateProfileHTTPRequest(http.MethodPut, privateProfileHTTPPath, `{"expectedVersion":1,"fields":{"agentNotes":"`+privateProfileHTTPMarker+`"}}`, token, "application/json")
	r.Header.Set("X-Request-ID", "private_http_log_001")
	h := privateProfileHTTPNew(privateProfileHTTPCatalog{privateProfileHTTPStore: store}, access)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, r)
	privateProfileHTTPAssertError(t, rw, 503, "private_profile_unavailable")
	if !strings.Contains(captured.String(), "private_profile_error category=unavailable") || !strings.Contains(captured.String(), "request_id=private_http_log_001") {
		t.Fatal("failure omitted safe category/request correlation")
	}
	for _, secret := range []string{privateProfileHTTPMarker, token, fmt.Sprintf("%x", access.digest), "PostgreSQL DETAIL", privateProfileHTTPOwner, privateProfileHTTPAgent, "agentNotes", r.Header.Get("Authorization")} {
		if strings.Contains(captured.String(), secret) {
			t.Fatal("private detail, request input or credential was written to logs")
		}
	}
	// Current fixed-code errors also do not write the wrapped database DETAIL.
	for _, err := range []error{fmt.Errorf("%s: %w", privateProfileHTTPMarker, agentprofile.ErrConflict), fmt.Errorf("%s: %w", privateProfileHTTPMarker, agentprofile.ErrForbidden)} {
		store.err = err
		rw = httptest.NewRecorder()
		s.getOwnAgentPrivateProfile(rw, privateProfileHTTPRequest(http.MethodGet, privateProfileHTTPPath, "", token, ""))
		if strings.Contains(captured.String(), privateProfileHTTPMarker) || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
			t.Fatal("wrapped private Store error disclosed")
		}
	}
	if _, err := io.Copy(io.Discard, &captured); err != nil {
		t.Fatal("could not discard synthetic captured logs")
	}
}
