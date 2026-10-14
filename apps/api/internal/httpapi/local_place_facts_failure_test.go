package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

func TestLocalPlaceFactsUnavailableIsNotACompletedAnswer(t *testing.T) {
	for _, org := range []bool{false, true} {
		for _, err := range []error{agentworkspace.ErrLocalPlaceFactsUnavailable, errors.Join(errors.New("TEST_ONLY wrapper"), agentworkspace.ErrLocalPlaceFactsUnavailable)} {
			w := httptest.NewRecorder()
			agentTaskWriteFailure(w, err, org)
			if w.Code != http.StatusServiceUnavailable || !containsBody(w.Body.String(), `"code":"place_answer_unavailable"`) || !containsBody(w.Body.String(), "本轮尚未回答") {
				t.Fatalf("insufficient facts reported as %d: %s", w.Code, w.Body.String())
			}
		}
	}
}
