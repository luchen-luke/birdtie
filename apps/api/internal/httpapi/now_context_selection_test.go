package httpapi

import (
	"errors"
	ncs "github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNowSelectionHTTPClosedErrors(t *testing.T) {
	for _, v := range []struct {
		err  error
		code int
	}{{ncs.ErrInvalid, 400}, {ncs.ErrDenied, 403}, {ncs.ErrConflict, 409}, {errors.New("private DB/token details"), 503}} {
		w := httptest.NewRecorder()
		nowSelectionFailure(w, v.err)
		if w.Code != v.code || strings.Contains(w.Body.String(), "private DB") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
