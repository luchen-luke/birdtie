package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"net/http/httptest"
	"testing"
)

func TestPlaceHoursUnavailableStopsAtExplicitNativeFailure(t *testing.T) {
	for _, org := range []bool{false, true} {
		w := httptest.NewRecorder()
		agentTaskWriteFailure(w, agentworkspace.ErrLocalPlaceHoursUnavailable, org)
		if w.Code != 422 || !containsBody(w.Body.String(), `"code":"place_hours_unavailable"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
