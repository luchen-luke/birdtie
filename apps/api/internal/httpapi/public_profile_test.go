package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

// Transport spies prove exact wire/interface behavior only. Native authority
// and lock/expiry checks are exercised separately against disposable PG.
type publicProfileBoundaryStore struct {
	*privateProfileHTTPAccess
	input          identity.ProfileInput
	initial        identity.Actor
	receivedDigest [32]byte
	writeCalls     int
	writeErr       error
	resultOverride *identity.Profile
}

func (s *publicProfileBoundaryStore) UpdateOwnProfile(context.Context, string, identity.ProfileInput) (identity.Profile, error) {
	panic("Public HTTP must never fall back to the legacy owner-ID writer")
}
func (s *publicProfileBoundaryStore) UpdateHumanProfile(_ context.Context, digest [32]byte, initial identity.Actor, input identity.ProfileInput) (identity.Profile, error) {
	s.writeCalls++
	s.input = input
	s.initial = initial
	s.receivedDigest = digest
	if s.resultOverride != nil {
		return *s.resultOverride, s.writeErr
	}
	return identity.Profile{AccountID: initial.ID, DisplayName: input.DisplayName, Bio: input.Bio, Visibility: input.Visibility}, s.writeErr
}
func publicProfileBoundary(t *testing.T) (http.Handler, *publicProfileBoundaryStore, string) {
	t.Helper()
	_, access, _, token := privateProfileHTTPFixture(t)
	store := &publicProfileBoundaryStore{privateProfileHTTPAccess: access}
	return privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, store), store, token
}

const publicProfileValid = `{"displayName":"中文资料","bio":"本人简介","visibility":"public"}`

func TestPublicProfileStrictWire(t *testing.T) {
	cases := []struct{ name, body string }{
		{"empty", ""}, {"array", "[]"}, {"root null", "null"}, {"scalar", "42"}, {"empty object", "{}"},
		{"missing name", `{"bio":"","visibility":"public"}`}, {"missing bio", `{"displayName":"中文","visibility":"public"}`},
		{"missing visibility", `{"displayName":"中文","bio":""}`},
		{"duplicate", `{"displayName":"中文","bio":"","visibility":"private","visibility":"public"}`},
		{"escaped duplicate", `{"displayName":"中文","bio":"","visibility":"private","\u0076isibility":"public"}`},
		{"case alias", `{"DisplayName":"中文","bio":"","visibility":"public"}`},
		{"case conflict", `{"displayName":"中文","DisplayName":"替换","bio":"","visibility":"public"}`},
		{"name null", `{"displayName":null,"bio":"","visibility":"public"}`},
		{"bio null", `{"displayName":"中文","bio":null,"visibility":"public"}`},
		{"visibility null", `{"displayName":"中文","bio":"","visibility":null}`},
		{"name number", `{"displayName":12,"bio":"","visibility":"public"}`},
		{"nested bio", `{"displayName":"中文","bio":{"hidden":"text"},"visibility":"public"}`},
		{"name short", `{"displayName":"a","bio":"","visibility":"public"}`},
		{"name whitespace", `{"displayName":"  ","bio":"","visibility":"public"}`},
		{"visibility case", `{"displayName":"中文","bio":"","visibility":"PUBLIC"}`},
		{"name control", `{"displayName":"中文\n","bio":"","visibility":"public"}`},
		{"bio NUL", `{"displayName":"中文","bio":"\u0000","visibility":"public"}`},
		{"bio DEL", `{"displayName":"中文","bio":"\u007f","visibility":"public"}`},
		{"high surrogate", `{"displayName":"中文","bio":"\ud800","visibility":"public"}`},
		{"low surrogate", `{"displayName":"中文","bio":"\udc00","visibility":"public"}`},
		{"nonlow pair", `{"displayName":"中文","bio":"\ud800\u0001","visibility":"public"}`},
		{"invalid UTF8", `{"displayName":"中文","bio":"` + string([]byte{0xff}) + `","visibility":"public"}`},
		{"two documents", publicProfileValid + " {}"}, {"trailing garbage", publicProfileValid + "x"},
		{"oversized body", publicProfileValid + strings.Repeat(" ", publicProfileMaxBodyBytes)},
		{"name too large", `{"displayName":"` + strings.Repeat("名", 27) + `","bio":"","visibility":"public"}`},
		{"bio too large", `{"displayName":"中文","bio":"` + strings.Repeat("b", 501) + `","visibility":"public"}`},
	}
	for _, key := range []string{"ownerId", "accountId", "agentId", "expectedVersion", "confirmed", "purpose", "fields", "rules", "avatar", "city", "interests"} {
		cases = append(cases, struct{ name, body string }{"unknown " + key, strings.TrimSuffix(publicProfileValid, "}") + `,"` + key + `":"not-authority"}`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, store, token := publicProfileBoundary(t)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", tc.body, token, "application/json"))
			if rw.Code != 400 || store.writeCalls != 0 || rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("wire rejected=%t writes=%d", rw.Code == 400, store.writeCalls)
			}
			if strings.Contains(rw.Body.String(), "not-authority") || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
				t.Fatal("error disclosed source body")
			}
		})
	}
}

func TestPublicProfileClientCompatibilityAndNormalization(t *testing.T) {
	for _, tc := range []struct{ name, body, contentType string }{
		{"current client", publicProfileValid, "application/json"},
		{"trim original three fields", `{"displayName":"  中文资料  ","bio":"  本人简介  ","visibility":"public"}`, "application/json; charset=utf-8"},
		{"valid Unicode pair", `{"displayName":"中文资料","bio":"\ud83d\ude00","visibility":"private"}`, "application/json"},
		{"literal replacement character", `{"displayName":"中文资料","bio":"�","visibility":"private"}`, "application/json"},
		{"literal backslash", `{"displayName":"中文资料","bio":"\\ud800","visibility":"private"}`, "application/json"},
		{"boundary limits", `{"displayName":"` + strings.Repeat("n", 80) + `","bio":"` + strings.Repeat("b", 500) + `","visibility":"private"}`, "application/json"},
		{"bio multiline", `{"displayName":"中文资料","bio":"甲\n乙\t丙","visibility":"private"}`, "application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, store, token := publicProfileBoundary(t)
			r := privateProfileHTTPRequest("PUT", "/v1/me/profile", tc.body, token, tc.contentType)
			r.Header.Set("X-Request-ID", "profile_client_wire")
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, r)
			if rw.Code != 200 || store.writeCalls != 1 || store.receivedDigest != store.digest || store.initial != store.actor {
				t.Fatal("original client did not bind exact current server identity")
			}
			if tc.name == "trim original three fields" && (store.input.DisplayName != "中文资料" || store.input.Bio != "本人简介") {
				t.Fatal("normalization changed")
			}
			privateProfileHTTPDBAssertPublic(t, rw, identity.Profile{AccountID: store.actor.ID, DisplayName: store.input.DisplayName, Bio: store.input.Bio, Visibility: store.input.Visibility})
		})
	}
}

func TestPublicProfileTransportIdentityAndSelectors(t *testing.T) {
	for _, name := range []string{"anonymous", "invalid bearer", "duplicate bearer", "expired identity", "auth infrastructure", "missing type", "query owner", "bare query", "workspace", "empty workspace", "wrong content type", "missing content type", "duplicate content type", "other charset", "unknown MIME param", "nil body", "actual nil body", "canceled context"} {
		t.Run(name, func(t *testing.T) {
			h, store, token := publicProfileBoundary(t)
			r := privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json")
			want := 400
			switch name {
			case "anonymous":
				r.Header.Del("Authorization")
				want = 401
			case "invalid bearer":
				r.Header.Set("Authorization", "Bearer untrusted")
				want = 401
			case "duplicate bearer":
				r.Header.Add("Authorization", "Bearer "+token)
				want = 401
			case "expired identity":
				store.err = identity.ErrUnauthorized
				want = 401
			case "auth infrastructure":
				store.err = errors.New(privateProfileHTTPMarker)
				want = 503
			case "missing type":
				store.writeErr = identity.ErrUnauthorized
				want = 401
			case "query owner":
				r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
			case "bare query":
				r.URL.ForceQuery = true
			case "workspace":
				r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign)
				want = 403
			case "empty workspace":
				r.Header.Set("X-Birdtie-Organization-Workspace", "")
				want = 403
			case "wrong content type":
				r.Header.Set("Content-Type", "text/plain")
			case "missing content type":
				r.Header.Del("Content-Type")
			case "duplicate content type":
				r.Header.Add("Content-Type", "application/json")
			case "other charset":
				r.Header.Set("Content-Type", "application/json; charset=iso-8859-1")
			case "unknown MIME param":
				r.Header.Set("Content-Type", "application/json; boundary=x")
			case "nil body":
				r.Body = http.NoBody
			case "actual nil body":
				r.Body = nil
			case "canceled context":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
				store.writeErr = context.Canceled
				want = 503
			}
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, r)
			if rw.Code != want || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
				t.Fatalf("transport status=%d want=%d", rw.Code, want)
			}
			if name != "missing type" && name != "canceled context" && store.writeCalls != 0 {
				t.Fatal("denied transport reached writer")
			}
		})
	}
}

func TestPublicProfileNoLegacyFallbackAndSafeErrors(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	h := privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, access)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json"))
	if rw.Code != 503 || access.calls != 0 {
		t.Fatal("missing secure port did not fail closed")
	}
	h = privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, nil)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json"))
	if rw.Code != 503 {
		t.Fatal("nil access did not fail closed")
	}
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"revoked", identity.ErrUnauthorized, 401}, {"invalid", identity.ErrInvalidProfile, 400}, {"absent", identity.ErrNotFound, 404}, {"unavailable", identity.ErrProfileUnavailable, 503}, {"raw private SQL", errors.New(privateProfileHTTPMarker + " token secret SQL detail"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s, token := publicProfileBoundary(t)
			s.writeErr = tc.err
			var logs bytes.Buffer
			oldOut := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(oldOut)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json"))
			if rw.Code != tc.want || strings.Contains(rw.Body.String()+logs.String(), privateProfileHTTPMarker) || strings.Contains(logs.String(), token) {
				t.Fatal("unsafe or incorrect public error")
			}
			var response struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(rw.Body.Bytes(), &response) != nil || response.Error.Message == "" {
				t.Fatal("missing Chinese safe message")
			}
		})
	}
}

func TestPublicProfileReaderErrorFailsClosed(t *testing.T) {
	h, s, token := publicProfileBoundary(t)
	r := privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json")
	r.Body = io.NopCloser(publicProfileFailReader{})
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, r)
	if rw.Code != 400 || s.writeCalls != 0 {
		t.Fatal("body read failure reached writer")
	}
}

type publicProfileFailReader struct{}

func (publicProfileFailReader) Read([]byte) (int, error) {
	return 0, errors.New(privateProfileHTTPMarker)
}

func TestPublicProfileSuccessfulResultMustMatchCurrentSelfAndInput(t *testing.T) {
	for _, name := range []string{"other owner", "missing owner", "missing name", "visibility invalid", "different valid value", "different privacy", "trim required", "private control", "invalid UTF8"} {
		t.Run(name, func(t *testing.T) {
			h, store, token := publicProfileBoundary(t)
			result := identity.Profile{AccountID: store.actor.ID, DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"}
			switch name {
			case "other owner":
				result.AccountID = privateProfileHTTPForeign
			case "missing owner":
				result.AccountID = ""
			case "missing name":
				result.DisplayName = ""
			case "visibility invalid":
				result.Visibility = "unknown"
			case "different valid value":
				result.Bio = privateProfileHTTPMarker
			case "different privacy":
				result.Visibility = "private"
			case "trim required":
				result.DisplayName = " 中文资料 "
			case "private control":
				result.Bio = "\x00"
			case "invalid UTF8":
				result.Bio = string([]byte{0xff})
			}
			store.resultOverride = &result
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, privateProfileHTTPRequest("PUT", "/v1/me/profile", publicProfileValid, token, "application/json"))
			if rw.Code != 503 || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) || strings.Contains(rw.Body.String(), privateProfileHTTPForeign) {
				t.Fatal("wrong-bound success result escaped the response gate")
			}
		})
	}
}

func TestPublicProfileNativeUnconfiguredStoreFailsClosed(t *testing.T) {
	_, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	input := identity.ProfileInput{DisplayName: "中文资料", Bio: "本人简介", Visibility: "public"}
	actor := identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}
	for _, name := range []string{"nil context", "nil Store", "nil pool"} {
		t.Run(name, func(t *testing.T) {
			store := postgres.New(nil, false)
			ctx := context.Background()
			if name == "nil context" {
				ctx = nil
			}
			if name == "nil Store" {
				store = nil
			}
			result, err := store.UpdateHumanProfile(ctx, digest, actor, input)
			if !errors.Is(err, identity.ErrProfileUnavailable) || result != (identity.Profile{}) {
				t.Fatal("unconfigured native gateway did not fail closed")
			}
		})
	}
}
