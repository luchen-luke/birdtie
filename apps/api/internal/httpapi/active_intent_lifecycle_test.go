package httpapi

import (
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"net/http/httptest"
	"testing"
)

func TestActiveIntentHTTPClosedErrors(t *testing.T) {
	for _, c := range []struct {
		e    error
		code int
	}{{ai.ErrInvalid, 400}, {ai.ErrDenied, 403}, {ai.ErrNotFound, 404}, {ai.ErrConflict, 409}, {errors.New("secret database detail"), 503}} {
		w := httptest.NewRecorder()
		activeIntentFailure(w, c.e)
		if w.Code != c.code {
			t.Fatal(w.Code)
		}
		if c.e.Error() == w.Body.String() {
			t.Fatal("raw error released")
		}
	}
}
