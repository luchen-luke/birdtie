package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type emailLoginTestStore struct {
	calls  int
	digest [32]byte
	valid  bool
}

func (s *emailLoginTestStore) RequestDevPhoneChallenge(context.Context, [32]byte) error {
	return devauth.ErrInvalid
}
func (s *emailLoginTestStore) VerifyDevPhoneChallenge(context.Context, [32]byte, bool, [32]byte) error {
	return devauth.ErrInvalid
}
func (s *emailLoginTestStore) RequestDevEmailChallenge(_ context.Context, d [32]byte) error {
	s.calls++
	s.digest = d
	return nil
}
func (s *emailLoginTestStore) VerifyDevEmailChallenge(_ context.Context, d [32]byte, valid bool, _ [32]byte) error {
	s.calls++
	s.digest = d
	s.valid = valid
	if !valid {
		return devauth.ErrInvalid
	}
	return nil
}
func TestFixedEmailHTTPUsesDistinctRealmAndStrictBody(t *testing.T) {
	port := &emailLoginTestStore{}
	s := &server{devPhone: port, devPhoneEnabled: true}
	mux := http.NewServeMux()
	s.registerDevEmailRoutes(mux)
	call := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call("/v1/auth/dev-email/code", `{"email":" Owner@Example.TEST "}`); w.Code != 200 || port.digest != devauth.EmailDigest("owner@example.test") {
		t.Fatal("email became phone identity", w.Code)
	}
	w := call("/v1/auth/dev-email/verify", `{"email":"owner@example.test","code":"123456"}`)
	var out struct {
		Data struct {
			CredentialKind string `json:"credentialKind"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.CredentialKind != "email" || !port.valid {
		t.Fatal("explicit fixed email receipt missing")
	}
	before := port.calls
	if w = call("/v1/auth/dev-email/code", `{"email":"owner@example.test","phone":"fake"}`); w.Code != 400 || port.calls != before {
		t.Fatal("phone alias or extra field accepted")
	}
	s.devPhoneEnabled = false
	if w = call("/v1/auth/dev-email/code", `{"email":"owner@example.test"}`); w.Code != 404 || port.calls != before {
		t.Fatal("disabled test channel accepted")
	}
}
