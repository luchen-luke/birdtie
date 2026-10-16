package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/sessionrefresh"
)

type deviceSessionHTTPStore struct {
	identity.AccessStore
	calls, revokes int
	result         sessionrefresh.Response
	err            error
}

func (s *deviceSessionHTTPStore) RefreshSession(context.Context, string, string) (sessionrefresh.Response, error) {
	s.calls++
	return s.result, s.err
}
func (s *deviceSessionHTTPStore) RevokeRefreshFamily(context.Context, string) error {
	s.revokes++
	return s.err
}

func TestDeviceSessionHTTPStrictRefreshWithoutAccessAndNoCredentialCache(t *testing.T) {
	token, _, err := sessionrefresh.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	store := &deviceSessionHTTPStore{result: sessionrefresh.Response{AccessToken: access, RefreshToken: token, RefreshExpiresAt: time.Now().Add(time.Hour), AccountID: "17600000-0000-4000-8000-000000000001", AuthMethod: "dev_phone", TokenType: "Bearer", ExpiresInSeconds: 28800, IdleTimeoutSeconds: 1800}}
	s := &server{access: store, devPhoneEnabled: true}
	valid := `{"refreshToken":"` + token + `","operationId":"17600000-0000-4000-8000-000000000002"}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"no access required", valid, 200},
		{"unknown selector", strings.TrimSuffix(valid, "}") + `,"accountId":"other"}`, 400},
		{"trailing object", valid + `{}`, 400},
		{"oversized", `{"refreshToken":"` + strings.Repeat("x", 3000) + `","operationId":"17600000-0000-4000-8000-000000000002"}`, 400},
		{"invalid operation", strings.Replace(valid, "17600000-0000-4000-8000-000000000002", "not-an-operation", 1), 400},
		{"invalid credential", strings.Replace(valid, token, "invalid", 1), 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := store.calls
			r := httptest.NewRequest(http.MethodPost, "/v1/session/refresh", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			s.refreshPersistentSession(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d", w.Code, tc.want)
			}
			if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("credential response cacheable")
			}
			if tc.want == 200 {
				var out struct {
					Data sessionrefresh.Response `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.AccountID != store.result.AccountID || out.Data.AuthMethod != "dev_phone" {
					t.Fatal("wire contract")
				}
			} else if store.calls != before {
				t.Fatal("invalid request reached store")
			}
		})
	}
	store.err = identity.ErrUnauthorized
	w := httptest.NewRecorder()
	s.refreshPersistentSession(w, httptest.NewRequest(http.MethodPost, "/v1/session/refresh", strings.NewReader(valid)))
	if w.Code != 401 || strings.Contains(w.Body.String(), token) {
		t.Fatal("rejection leaked or wrong status")
	}
	s.devPhoneEnabled = false
	before := store.calls
	w = httptest.NewRecorder()
	s.refreshPersistentSession(w, httptest.NewRequest(http.MethodPost, "/v1/session/refresh", strings.NewReader(valid)))
	if w.Code != 401 || store.calls != before {
		t.Fatal("disabled login method reached store")
	}
}

func TestDeviceSessionHTTPLogoutWithExpiredAccessAndRefreshOnly(t *testing.T) {
	token, _, _ := sessionrefresh.NewToken()
	store := &deviceSessionHTTPStore{}
	s := &server{access: store, devPhoneEnabled: true}
	w := httptest.NewRecorder()
	s.logout(w, httptest.NewRequest(http.MethodPost, "/v1/session/logout", strings.NewReader(`{"refreshToken":"`+token+`"}`)))
	if w.Code != 204 || store.revokes != 1 {
		t.Fatal("refresh-only logout required access")
	}
	w = httptest.NewRecorder()
	s.logout(w, httptest.NewRequest(http.MethodPost, "/v1/session/logout", strings.NewReader(`{"refreshToken":"`+token+`","accountId":"other"}`)))
	if w.Code != 400 || store.revokes != 1 {
		t.Fatal("logout accepted unknown identity selector")
	}
	s.devPhoneEnabled = false
	w = httptest.NewRecorder()
	s.logout(w, httptest.NewRequest(http.MethodPost, "/v1/session/logout", strings.NewReader(`{"refreshToken":"`+token+`"}`)))
	if w.Code != 204 || store.revokes != 2 {
		t.Fatal("disabled sign-in prevented credential revocation")
	}
}
