package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/audittrace"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestTraceEchoesSafeClientIDAndRecordsStatus(t *testing.T) {
	handler := requestTrace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if audittrace.RequestID(r.Context()) != "client_12345678" {
			t.Fatal("trace not propagated to native context")
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-ID", "client_12345678")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") != "client_12345678" {
		t.Fatalf("unexpected response: status=%d request_id=%q", response.Code, response.Header().Get("X-Request-ID"))
	}
}

func TestRequestTraceReplacesUnsafeID(t *testing.T) {
	var downstream string
	handler := requestTrace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstream = audittrace.RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "bad\nforged-log-line")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	id := response.Header().Get("X-Request-ID")
	if downstream != id {
		t.Fatal("replacement context/header differ")
	}
	if id == "bad\nforged-log-line" || len(id) != 32 || !safeRequestID.MatchString(id) {
		t.Fatalf("unsafe request id was accepted or replacement invalid: %q", id)
	}
}
