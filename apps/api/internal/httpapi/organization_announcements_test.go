package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrganizationAnnouncementHTTPErrorAndReadBodies(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
	}{{agentmemory.ErrInvalid, 400}, {agentmemory.ErrForbidden, 403}, {agentmemory.ErrNotFound, 404}, {agentmemory.ErrConflict, 409}, {identity.ErrUnauthorized, 401}, {errors.New("SQL private draft text"), 503}} {
		w := httptest.NewRecorder()
		announcementFailure(w, c.err)
		if w.Code != c.status || strings.Contains(w.Body.String(), "SQL private") {
			t.Fatal("private error leak")
		}
	}
	for _, c := range []struct {
		body string
		want bool
	}{{"", true}, {"{}", false}, {" ", false}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", strings.NewReader(c.body))
		if announcementEmptyBody(w, r) != c.want {
			t.Fatal("GET body accepted")
		}
	}
}
