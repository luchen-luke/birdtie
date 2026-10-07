package httpapi

import (
	"errors"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCandidatePipelineHTTPErrorDoesNotLeakPrivateDetails(t *testing.T) {
	for _, item := range []struct {
		e    error
		code int
	}{{acp.ErrInvalid, 400}, {acp.ErrDenied, 403}, {acp.ErrConflict, 409}, {acp.ErrBusy, 409}, {acp.ErrExpired, 409}, {acp.ErrUnavailable, 503}, {errors.New("PRIVATE_TOKEN_DATABASE_CANARY"), 503}} {
		w := httptest.NewRecorder()
		candidatePipelineHTTPError(w, item.e)
		if w.Code != item.code || strings.Contains(w.Body.String(), "CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestCandidatePipelineHTTPNoPayloadOrSwitchAccepted(t *testing.T) {
	for _, body := range []string{`{}`, `{"retentionGrantId":null}`, `{"retentionGrantId":"id","confirmed":true}`, `{"retentionGrantId":"id","retentionGrantId":"id"}`, `{"retentionGrantId":"id","sources":[]}`, `{"retentionGrantId":"id","featureEnabled":true}`, `{"retentionGrantId":"id","handlerVersion":"mom-candidate-local-v2"}`, `{"retentionGrantId":"id","payloadDigest":"a"}`, `{"retentionGrantId":"id"} {}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if _, ok := candidateRetentionBody(w, r, "retentionGrantId"); ok || w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
}
