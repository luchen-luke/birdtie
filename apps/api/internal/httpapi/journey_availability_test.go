package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestJourneyAvailabilityDisabledKeepsOriginalRecovery(t *testing.T) {
	if content.JourneysEnabled {
		t.Fatal("Journey release boundary must stay closed")
	}
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	store := &collectionPublicationHTTPSpy{}
	handler := collectionPublicationHTTPHandler(access, store)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/v1/me/journeys/discovery", ""); w.Code != http.StatusNotFound {
		t.Fatalf("deferred discovery route returned %d", w.Code)
	}
	if w := request(http.MethodPost, "/v1/me/personal-collections/journey/"+imageMomentID+"/publication", collectionPublicationBody); w.Code != http.StatusServiceUnavailable || store.calls != 0 {
		t.Fatalf("new Journey publication reached store: status=%d calls=%d", w.Code, store.calls)
	}
	if w := request(http.MethodGet, "/v1/me/personal-collections/publication-operations/22222222-2222-4222-8222-222222222222/receipt", ""); w.Code != http.StatusOK || store.action != "RECOVER" {
		t.Fatalf("original operation recovery unavailable: %d", w.Code)
	}
	if w := request(http.MethodPost, "/v1/me/personal-collections/theme_map/"+imageMomentID+"/publication", collectionPublicationBody); w.Code != http.StatusOK || store.action != "PUBLISH" {
		t.Fatalf("unrelated ThemeMap publication changed: %d", w.Code)
	}
}

func TestJourneyCopyAvailabilityDisabledKeepsOriginalRecovery(t *testing.T) {
	base := "/v1/public-collections/journey/" + imageMomentID + "/copy"
	for _, c := range []struct {
		name, method, path, body, org, action string
		status, calls                         int
		retire                                bool
	}{
		{"newJourneyPreview", "POST", base + "/preview", copyHTTPBody(false), "", "", 503, 0, false},
		// Commit must reach the store: only it can distinguish an original
		// immutable operation from a request for a new destination.
		{"originalJourneyReplay", "PUT", base, copyHTTPBody(true), "", "COPY", 200, 1, false},
		{"originalReceipt", "GET", "/v1/me/personal-collections/copy-operations/" + copyHTTPOperationID + "/receipt", "", "", "GET", 200, 1, false},
		{"originalStop", "POST", "/v1/me/personal-collections/copy-operations/" + copyHTTPOperationID + "/stop", "", "", "STOP", 200, 1, false},
		{"themeMapPreview", "POST", strings.Replace(base, "/journey/", "/theme_map/", 1) + "/preview", copyHTTPBody(false), "", "PREVIEW", 200, 1, false},
		{"themeMapCommit", "PUT", strings.Replace(base, "/journey/", "/theme_map/", 1), copyHTTPBody(true), "", "COPY", 200, 1, false},
		{"organizationStillDenied", "POST", base + "/preview", copyHTTPBody(false), imageMomentID, "", 403, 0, false},
		{"originalReplaySessionRetired", "PUT", base, copyHTTPBody(true), "", "COPY", 409, 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := &collectionCopyHTTPSpy{retire: c.retire}
			w := copyHTTPRequest(t, g, c.method, c.path, c.body, c.org)
			if w.Code != c.status || g.calls != c.calls || g.action != c.action {
				t.Fatalf("status=%d calls=%d action=%s", w.Code, g.calls, g.action)
			}
			if c.status != 200 && (strings.Contains(w.Body.String(), "approval") || strings.Contains(w.Body.String(), "本人新集合")) {
				t.Fatal("unavailable/retired request exposed approval or private input")
			}
		})
	}
}
