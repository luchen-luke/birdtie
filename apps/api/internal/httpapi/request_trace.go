package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/birdtie/birdtie/apps/api/internal/audittrace"
	"log"
	"net/http"
	"regexp"
	"time"
)

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func requestTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if !safeRequestID.MatchString(requestID) {
			bytes := make([]byte, 16)
			if _, err := rand.Read(bytes); err != nil {
				log.Printf("request id generation failed: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			requestID = hex.EncodeToString(bytes)
		}
		w.Header().Set("X-Request-ID", requestID)
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(audittrace.WithRequestID(r.Context(), requestID)))
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		log.Printf("request_id=%s method=%s status=%d latency_ms=%d",
			requestID, r.Method, recorder.status, time.Since(start).Milliseconds())
	})
}
