package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const nowLiveOverallTimeout = 30 * time.Second

// This startup-only owner reference selects a deadline; it grants no model,
// source, session or account access. The embedded native driver rechecks those.
type nowLiveOwnerBoundAnswers struct {
	agentworkspace.LiveAnswers
	ownerID string
}

func (n *nowLiveOwnerBoundAnswers) deadlineOwnerID() string {
	if n == nil || n.LiveAnswers == nil {
		return ""
	}
	return n.ownerID
}

type nowLiveDeadlineIdentity interface {
	Authenticate(context.Context, [32]byte) (identity.Actor, error)
}

type nowLiveDeadlineBoundary struct {
	ownerID string
	access  nowLiveDeadlineIdentity
	next    http.Handler
	timeout time.Duration
}

func withNowLiveDeadline(answers agentworkspace.LiveAnswers, access nowLiveDeadlineIdentity, next http.Handler) http.Handler {
	bound, ok := answers.(interface{ deadlineOwnerID() string })
	if !ok || bound.deadlineOwnerID() == "" || access == nil {
		return next
	}
	return nowLiveDeadlineBoundary{ownerID: bound.deadlineOwnerID(), access: access, next: next, timeout: nowLiveOverallTimeout}
}

func (b nowLiveDeadlineBoundary) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Inspect only routing and session headers. Body reads, task preparation,
	// native search, provider work and final projection share the same deadline.
	if r.Method != http.MethodPost || !nowLiveTaskCreatePath(r.URL.Path) || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		b.next.ServeHTTP(w, r)
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		b.next.ServeHTTP(w, r)
		return
	}
	digest, err := identity.ParseBearer(headers[0])
	if err != nil {
		b.next.ServeHTTP(w, r)
		return
	}

	// Capture before identity lookup so its latency cannot reset the window.
	// WithDeadline also preserves any earlier caller deadline and cancellation.
	deadlineCtx, cancel := context.WithDeadline(r.Context(), time.Now().Add(b.timeout))
	defer cancel()
	actor, err := b.access.Authenticate(deadlineCtx, digest)
	if err == nil && (actor.ID != b.ownerID || actor.AccountType != "person") {
		b.next.ServeHTTP(w, r)
		return
	}
	// An unavailable identity lookup cannot admit unbounded live work. The
	// existing handler still resolves identity and emits its original error.
	b.next.ServeHTTP(w, r.WithContext(deadlineCtx))
}

func nowLiveTaskCreatePath(path string) bool {
	parts := strings.Split(path, "/")
	return len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "cities" && parts[3] != "" && parts[4] == "agent" && parts[5] == "tasks"
}
