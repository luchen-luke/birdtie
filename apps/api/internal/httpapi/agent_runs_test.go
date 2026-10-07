package httpapi

import (
	"errors"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentRunHTTPErrorRedactsNativeDetails(t *testing.T) {
	for _, c := range []struct {
		e    error
		code int
	}{{ar.ErrInvalid, 400}, {ar.ErrDenied, 403}, {ar.ErrExpired, 409}, {ar.ErrConflict, 409}, {ar.ErrNotFound, 404}, {ar.ErrUnavailable, 503}, {errors.New("TOKEN_PRIVATE_DATABASE_CANARY"), 503}} {
		w := httptest.NewRecorder()
		agentRunHTTPError(w, c.e)
		if w.Code != c.code || strings.Contains(w.Body.String(), "CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestAgentRunHTTPClosedHumanInputCannotChooseWorkerOrLease(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"momentId":null,"retentionGrantId":""}`, `{"momentId":"id","retentionGrantId":"","workerId":"x"}`, `{"momentId":"id","retentionGrantId":"","lease":99999}`, `{"momentId":"id","retentionGrantId":"","confirmed":true}`, `{"momentId":"id","retentionGrantId":"","body":"private"}`, `{"momentId":"id","momentId":"id","retentionGrantId":""}`, `{"momentId":"id","retentionGrantId":""} {}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if _, ok := candidateRetentionBody(w, r, "momentId", "retentionGrantId"); ok || w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
}
